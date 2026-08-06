package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/permission"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

const HistoryLimit = 1000

// ToolRoundLimit caps model->tool->model iterations in a single run so a
// misbehaving model cannot loop forever.
const ToolRoundLimit = 5

const FinishAwaitingApproval = "awaiting_approval"

const (
	StatusPending          = "pending"
	StatusDone             = "done"
	StatusError            = "error"
	StatusAwaitingApproval = "awaiting_approval"
	StatusDenied           = "denied"
)

type StartEvent struct {
	SessionID     int64 `json:"session_id"`
	UserMessageID int64 `json:"user_message_id"`
}

type DeltaEvent struct {
	Content string `json:"content"`
}

type DoneEvent struct {
	MessageID    int64  `json:"message_id"`
	FinishReason string `json:"finish_reason"`
}

type ErrorEvent struct {
	Message string `json:"message"`
}

type ToolCallsEvent struct {
	MessageID int64          `json:"message_id"`
	Calls     []llm.ToolCall `json:"calls"`
}

type ToolResultEvent struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Result     string `json:"result"`
}

type ApprovalRequiredEvent struct {
	ID         int64  `json:"id"`
	ToolCallID string `json:"tool_call_id"`
	MessageID  int64  `json:"message_id"`
	Name       string `json:"name"`
	Arguments  string `json:"arguments"`
}

type DecisionEvent struct {
	ID     int64  `json:"id"`
	Result string `json:"result"`
	Status string `json:"status"`
}

// Sink delivers run events to a consumer. Interactive runs stream them over
// SSE; headless runs discard them.
type Sink interface {
	Send(name string, data any) error
}

type DiscardSink struct{}

func (DiscardSink) Send(string, any) error { return nil }

// Resolver picks the LLM provider for a run.
type Resolver interface {
	Resolve(ctx context.Context, userID, providerID int64, model string) (llm.Provider, error)
}

// Engine drives the model->tool->model loop shared by the chat and approval
// handlers and the scheduler.
type Engine struct {
	Store       store.Store
	LLM         Resolver
	Tools       tool.Executor
	Permissions *permission.Ruleset
	Headless    bool
}

// AsHeadless returns a copy for unattended runs, where ask verdicts become
// denies because nobody is there to answer.
func (e *Engine) AsHeadless() *Engine {
	c := *e
	c.Headless = true
	return &c
}

// Run drives rounds until the model produces a final answer, tool approval
// pauses the run, or the round limit trips. req.Messages holds the
// conversation so far and is extended each round. The provider is resolved
// once for userID and providerID (0 = the user's default) and reused across
// rounds.
func (e *Engine) Run(ctx context.Context, sink Sink, sessionID, userID, providerID int64, req llm.Request) error {
	provider, err := e.LLM.Resolve(ctx, userID, providerID, req.Model)
	if err != nil {
		return err
	}
	tools := e.Tools.Definitions()

	for range ToolRoundLimit {
		roundReq := req
		roundReq.Tools = tools

		text, calls, finishReason, err := e.runRound(ctx, sink, provider, roundReq)
		if err != nil {
			return err
		}

		if len(calls) == 0 {
			assistant, err := e.Store.AppendMessage(ctx, db.AppendMessageParams{
				SessionID: sessionID,
				Role:      string(llm.RoleAssistant),
				Content:   text,
			})
			if err != nil {
				return err
			}
			if err := e.Store.TouchSession(ctx, sessionID); err != nil {
				return err
			}
			return sink.Send("done", DoneEvent{
				MessageID:    assistant.ID,
				FinishReason: finishReason,
			})
		}

		assistant, err := e.Store.AppendMessage(ctx, db.AppendMessageParams{
			SessionID: sessionID,
			Role:      string(llm.RoleAssistant),
			Content:   text,
		})
		if err != nil {
			return err
		}

		callRows := make([]db.ToolCall, 0, len(calls))
		for _, call := range calls {
			row, err := e.Store.InsertToolCall(ctx, db.InsertToolCallParams{
				MessageID: assistant.ID,
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
				Status:    StatusPending,
			})
			if err != nil {
				return err
			}
			callRows = append(callRows, row)
		}

		if err := sink.Send("tool_calls", ToolCallsEvent{
			MessageID: assistant.ID,
			Calls:     calls,
		}); err != nil {
			return err
		}

		req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, Content: text, ToolCalls: calls})
		awaiting := false
		for i, call := range calls {
			switch e.evaluate(call.Name) {
			case permission.Allow:
				result, status := e.ExecuteToolCall(ctx, call)
				if err := e.Store.UpdateToolCallResult(ctx, db.UpdateToolCallResultParams{
					ID:     callRows[i].ID,
					Result: result,
					Status: status,
				}); err != nil {
					return err
				}
				if err := sink.Send("tool_result", ToolResultEvent{
					ToolCallID: call.ID,
					Name:       call.Name,
					Result:     result,
				}); err != nil {
					return err
				}
				req.Messages = append(req.Messages, llm.Message{Role: llm.RoleTool, Content: result, ToolCallID: call.ID})
			case permission.Deny:
				result := fmt.Sprintf("denied by policy: tool %q is not allowed", call.Name)
				if err := e.Store.UpdateToolCallResult(ctx, db.UpdateToolCallResultParams{
					ID:     callRows[i].ID,
					Result: result,
					Status: StatusDenied,
				}); err != nil {
					return err
				}
				if err := sink.Send("tool_result", ToolResultEvent{
					ToolCallID: call.ID,
					Name:       call.Name,
					Result:     result,
				}); err != nil {
					return err
				}
				req.Messages = append(req.Messages, llm.Message{Role: llm.RoleTool, Content: result, ToolCallID: call.ID})
			default:
				if err := e.Store.SetToolCallStatus(ctx, db.SetToolCallStatusParams{
					ID:     callRows[i].ID,
					Status: StatusAwaitingApproval,
				}); err != nil {
					return err
				}
				if err := sink.Send("approval_required", ApprovalRequiredEvent{
					ID:         callRows[i].ID,
					ToolCallID: call.ID,
					MessageID:  assistant.ID,
					Name:       call.Name,
					Arguments:  call.Arguments,
				}); err != nil {
					return err
				}
				awaiting = true
			}
		}

		if awaiting {
			if err := e.Store.TouchSession(ctx, sessionID); err != nil {
				return err
			}
			return sink.Send("done", DoneEvent{FinishReason: FinishAwaitingApproval})
		}
	}

	return fmt.Errorf("tool call round limit (%d) reached", ToolRoundLimit)
}

func (e *Engine) evaluate(name string) permission.Verdict {
	if e.Headless {
		return e.Permissions.EvaluateHeadless(name)
	}
	return e.Permissions.Evaluate(name)
}

func (e *Engine) ExecuteToolCall(ctx context.Context, call llm.ToolCall) (result, status string) {
	result, err := e.Tools.Execute(ctx, call.Name, call.Arguments)
	if err != nil {
		return "tool error: " + err.Error(), StatusError
	}
	return result, StatusDone
}

// BuildHistory reconstructs the provider-facing conversation from the
// transcript, interleaving tool results after their assistant message.
func (e *Engine) BuildHistory(ctx context.Context, sessionID int64) ([]llm.Message, error) {
	messages, err := e.Store.ListMessages(ctx, db.ListMessagesParams{
		SessionID: sessionID,
		Limit:     HistoryLimit,
	})
	if err != nil {
		return nil, err
	}

	toolCalls, err := e.Store.ListToolCallsBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	callsByMessage := make(map[int64][]db.ToolCall)
	for _, tc := range toolCalls {
		callsByMessage[tc.MessageID] = append(callsByMessage[tc.MessageID], tc)
	}

	var history []llm.Message
	for _, m := range messages {
		msg := llm.Message{Role: llm.Role(m.Role), Content: m.Content}
		if m.Role == string(llm.RoleAssistant) {
			for _, tc := range callsByMessage[m.ID] {
				msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{ID: tc.CallID, Name: tc.Name, Arguments: tc.Arguments})
			}
			history = append(history, msg)
			for _, tc := range callsByMessage[m.ID] {
				history = append(history, llm.Message{Role: llm.RoleTool, Content: tc.Result, ToolCallID: tc.CallID})
			}
			continue
		}
		history = append(history, msg)
	}
	return history, nil
}

// runRound streams one model turn, relaying deltas to the sink, and returns
// the accumulated text, any requested tool calls, and the finish reason.
func (e *Engine) runRound(ctx context.Context, sink Sink, provider llm.Provider, req llm.Request) (string, []llm.ToolCall, string, error) {
	stream, err := provider.Chat(ctx, req)
	if err != nil {
		return "", nil, "", err
	}
	defer stream.Close() //nolint:errcheck

	var text strings.Builder
	var calls []llm.ToolCall
	var finishReason string
	for stream.Next() {
		chunk := stream.Current()
		if chunk.Delta != "" {
			text.WriteString(chunk.Delta)
			if err := sink.Send("delta", DeltaEvent{Content: chunk.Delta}); err != nil {
				return "", nil, "", err
			}
		}
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
		calls = append(calls, chunk.ToolCalls...)
	}
	if err := stream.Err(); err != nil {
		return "", nil, "", err
	}
	return text.String(), calls, finishReason, nil
}
