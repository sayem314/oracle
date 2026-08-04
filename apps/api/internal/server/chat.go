package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/sse"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/permission"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

const chatHistoryLimit = 1000

// chatToolRoundLimit caps model->tool->model iterations in a single request so
// a misbehaving model cannot loop forever.
const chatToolRoundLimit = 5

const finishAwaitingApproval = "awaiting_approval"

const (
	toolCallStatusPending          = "pending"
	toolCallStatusDone             = "done"
	toolCallStatusError            = "error"
	toolCallStatusAwaitingApproval = "awaiting_approval"
	toolCallStatusDenied           = "denied"
)

type chatLocalsKey struct{}

type chatContext struct {
	sessionID     int64
	userMessageID int64
	request       llm.Request
}

type chatRequest struct {
	SessionID *int64 `json:"session_id"`
	Message   string `json:"message"`
	Model     string `json:"model"`
}

type chatStartEvent struct {
	SessionID     int64 `json:"session_id"`
	UserMessageID int64 `json:"user_message_id"`
}

type chatDeltaEvent struct {
	Content string `json:"content"`
}

type chatDoneEvent struct {
	MessageID    int64  `json:"message_id"`
	FinishReason string `json:"finish_reason"`
}

type chatErrorEvent struct {
	Message string `json:"message"`
}

type chatToolCallsEvent struct {
	MessageID int64          `json:"message_id"`
	Calls     []llm.ToolCall `json:"calls"`
}

type chatToolResultEvent struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Result     string `json:"result"`
}

type chatApprovalRequiredEvent struct {
	ID         int64  `json:"id"`
	ToolCallID string `json:"tool_call_id"`
	MessageID  int64  `json:"message_id"`
	Name       string `json:"name"`
	Arguments  string `json:"arguments"`
}

// eventSink delivers chat events to a client. The chat and approval handlers
// both stream the same events, over SSE in either case.
type eventSink interface {
	Send(name string, data any) error
}

type sseSink struct {
	s *sse.Stream
}

func (e sseSink) Send(name string, data any) error {
	return e.s.Event(sse.Event{Name: name, Data: data})
}

func newChatHandler(deps Deps) fiber.Handler {
	stream := sse.New(sse.Config{
		Handler: func(c fiber.Ctx, s *sse.Stream) error {
			return streamChat(deps, c, s)
		},
	})

	return func(c fiber.Ctx) error {
		chat, err := prepareChat(deps, c)
		if err != nil {
			return err
		}
		c.Locals(chatLocalsKey{}, chat)
		return stream(c)
	}
}

func prepareChat(deps Deps, c fiber.Ctx) (chatContext, error) {
	var req chatRequest
	if err := c.Bind().JSON(&req); err != nil {
		return chatContext{}, fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Message) == "" {
		return chatContext{}, fiber.NewError(fiber.StatusBadRequest, "message is required")
	}

	ctx := c.Context()
	userID := c.Locals(userIDKey{}).(int64)

	var sessionID int64
	switch req.SessionID {
	case nil:
		session, err := deps.Store.CreateSession(ctx, db.CreateSessionParams{UserID: userID})
		if err != nil {
			return chatContext{}, err
		}
		sessionID = session.ID
	default:
		session, err := deps.Store.GetSession(ctx, *req.SessionID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return chatContext{}, fiber.NewError(fiber.StatusNotFound, "session not found")
			}
			return chatContext{}, err
		}
		if session.UserID != userID {
			return chatContext{}, fiber.NewError(fiber.StatusNotFound, "session not found")
		}
		pending, err := deps.Store.CountPendingApprovalsBySession(ctx, *req.SessionID)
		if err != nil {
			return chatContext{}, err
		}
		if pending > 0 {
			return chatContext{}, fiber.NewError(fiber.StatusConflict, "tool approval pending")
		}
		sessionID = *req.SessionID
	}

	userMsg, err := deps.Store.AppendMessage(ctx, db.AppendMessageParams{
		SessionID: sessionID,
		Role:      string(llm.RoleUser),
		Content:   req.Message,
	})
	if err != nil {
		return chatContext{}, err
	}

	history, err := buildHistory(ctx, deps.Store, sessionID)
	if err != nil {
		return chatContext{}, err
	}

	return chatContext{
		sessionID:     sessionID,
		userMessageID: userMsg.ID,
		request:       llm.Request{Model: req.Model, Messages: history},
	}, nil
}

// buildHistory reconstructs the provider-facing conversation from the
// transcript, interleaving tool results after their assistant message.
func buildHistory(ctx context.Context, s store.Store, sessionID int64) ([]llm.Message, error) {
	messages, err := s.ListMessages(ctx, db.ListMessagesParams{
		SessionID: sessionID,
		Limit:     chatHistoryLimit,
	})
	if err != nil {
		return nil, err
	}

	toolCalls, err := s.ListToolCallsBySession(ctx, sessionID)
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

func streamChat(deps Deps, c fiber.Ctx, s *sse.Stream) error {
	chat := c.Locals(chatLocalsKey{}).(chatContext)
	sink := sseSink{s}

	if err := sink.Send("start", chatStartEvent{
		SessionID:     chat.sessionID,
		UserMessageID: chat.userMessageID,
	}); err != nil {
		return err
	}

	if err := runToolLoop(deps, sink, s.Context(), chat.sessionID, chat.request); err != nil {
		return sendChatError(s, err)
	}
	return nil
}

// runToolLoop drives model->tool->model rounds until the model produces a
// final answer, tool approval pauses the run, or the round limit trips.
// req.Messages holds the conversation so far and is extended each round.
func runToolLoop(deps Deps, sink eventSink, ctx context.Context, sessionID int64, req llm.Request) error {
	tools := deps.Tools.Definitions()

	for round := 0; round < chatToolRoundLimit; round++ {
		roundReq := req
		roundReq.Tools = tools

		text, calls, finishReason, err := runChatRound(deps, sink, ctx, roundReq)
		if err != nil {
			return err
		}

		if len(calls) == 0 {
			assistant, err := deps.Store.AppendMessage(ctx, db.AppendMessageParams{
				SessionID: sessionID,
				Role:      string(llm.RoleAssistant),
				Content:   text,
			})
			if err != nil {
				return err
			}
			if err := deps.Store.TouchSession(ctx, sessionID); err != nil {
				return err
			}
			return sink.Send("done", chatDoneEvent{
				MessageID:    assistant.ID,
				FinishReason: finishReason,
			})
		}

		assistant, err := deps.Store.AppendMessage(ctx, db.AppendMessageParams{
			SessionID: sessionID,
			Role:      string(llm.RoleAssistant),
			Content:   text,
		})
		if err != nil {
			return err
		}

		callRows := make([]db.ToolCall, 0, len(calls))
		for _, call := range calls {
			row, err := deps.Store.InsertToolCall(ctx, db.InsertToolCallParams{
				MessageID: assistant.ID,
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
				Status:    toolCallStatusPending,
			})
			if err != nil {
				return err
			}
			callRows = append(callRows, row)
		}

		if err := sink.Send("tool_calls", chatToolCallsEvent{
			MessageID: assistant.ID,
			Calls:     calls,
		}); err != nil {
			return err
		}

		req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, Content: text, ToolCalls: calls})
		awaiting := false
		for i, call := range calls {
			switch deps.Permissions.Evaluate(call.Name) {
			case permission.Allow:
				result, status := executeToolCall(deps, ctx, call)
				if err := deps.Store.UpdateToolCallResult(ctx, db.UpdateToolCallResultParams{
					ID:     callRows[i].ID,
					Result: result,
					Status: status,
				}); err != nil {
					return err
				}
				if err := sink.Send("tool_result", chatToolResultEvent{
					ToolCallID: call.ID,
					Name:       call.Name,
					Result:     result,
				}); err != nil {
					return err
				}
				req.Messages = append(req.Messages, llm.Message{Role: llm.RoleTool, Content: result, ToolCallID: call.ID})
			case permission.Deny:
				result := fmt.Sprintf("denied by policy: tool %q is not allowed", call.Name)
				if err := deps.Store.UpdateToolCallResult(ctx, db.UpdateToolCallResultParams{
					ID:     callRows[i].ID,
					Result: result,
					Status: toolCallStatusDenied,
				}); err != nil {
					return err
				}
				if err := sink.Send("tool_result", chatToolResultEvent{
					ToolCallID: call.ID,
					Name:       call.Name,
					Result:     result,
				}); err != nil {
					return err
				}
				req.Messages = append(req.Messages, llm.Message{Role: llm.RoleTool, Content: result, ToolCallID: call.ID})
			default:
				if err := deps.Store.SetToolCallStatus(ctx, db.SetToolCallStatusParams{
					ID:     callRows[i].ID,
					Status: toolCallStatusAwaitingApproval,
				}); err != nil {
					return err
				}
				if err := sink.Send("approval_required", chatApprovalRequiredEvent{
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
			if err := deps.Store.TouchSession(ctx, sessionID); err != nil {
				return err
			}
			return sink.Send("done", chatDoneEvent{FinishReason: finishAwaitingApproval})
		}
	}

	return fmt.Errorf("tool call round limit (%d) reached", chatToolRoundLimit)
}

func executeToolCall(deps Deps, ctx context.Context, call llm.ToolCall) (result, status string) {
	result, err := deps.Tools.Execute(ctx, call.Name, call.Arguments)
	if err != nil {
		return "tool error: " + err.Error(), toolCallStatusError
	}
	return result, toolCallStatusDone
}

// runChatRound streams one model turn, relaying deltas to the client, and
// returns the accumulated text, any requested tool calls, and the finish reason.
func runChatRound(deps Deps, sink eventSink, ctx context.Context, req llm.Request) (string, []llm.ToolCall, string, error) {
	stream, err := deps.LLM.Chat(ctx, req)
	if err != nil {
		return "", nil, "", err
	}
	defer func() { _ = stream.Close() }()

	var text strings.Builder
	var calls []llm.ToolCall
	var finishReason string
	for stream.Next() {
		chunk := stream.Current()
		if chunk.Delta != "" {
			text.WriteString(chunk.Delta)
			if err := sink.Send("delta", chatDeltaEvent{Content: chunk.Delta}); err != nil {
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

func sendChatError(s *sse.Stream, err error) error {
	if werr := s.Event(sse.Event{Name: "error", Data: chatErrorEvent{Message: err.Error()}}); werr != nil {
		return werr
	}
	return nil
}
