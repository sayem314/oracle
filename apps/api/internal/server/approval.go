package server

import (
	"database/sql"
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/sse"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

const (
	approvalDecisionApprove = "approve"
	approvalDecisionDeny    = "deny"

	deniedByUserResult = "denied by user"
)

type approvalLocalsKey struct{}

type approvalContext struct {
	call     db.GetToolCallRow
	decision string
}

type approvalRequest struct {
	ID       int64  `json:"id"`
	Decision string `json:"decision"`
}

type approvalDecisionEvent struct {
	ID     int64  `json:"id"`
	Result string `json:"result"`
	Status string `json:"status"`
}

func newApprovalHandler(deps Deps) fiber.Handler {
	stream := sse.New(sse.Config{
		Handler: func(c fiber.Ctx, s *sse.Stream) error {
			return streamApproval(deps, c, s)
		},
	})

	return func(c fiber.Ctx) error {
		var req approvalRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
		if req.ID <= 0 {
			return fiber.NewError(fiber.StatusBadRequest, "id is required")
		}
		if req.Decision != approvalDecisionApprove && req.Decision != approvalDecisionDeny {
			return fiber.NewError(fiber.StatusBadRequest, "decision must be approve or deny")
		}

		ctx := c.Context()
		userID := c.Locals(userIDKey{}).(int64)

		call, err := deps.Store.GetToolCall(ctx, req.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fiber.NewError(fiber.StatusNotFound, "tool call not found")
			}
			return err
		}
		if call.UserID != userID {
			return fiber.NewError(fiber.StatusNotFound, "tool call not found")
		}
		if call.ToolCall.Status != toolCallStatusAwaitingApproval {
			return fiber.NewError(fiber.StatusConflict, "tool call is not awaiting approval")
		}

		c.Locals(approvalLocalsKey{}, approvalContext{call: call, decision: req.Decision})
		return stream(c)
	}
}

// streamApproval records the decision and, once every call of the turn is
// decided, resumes the paused run from persisted state.
func streamApproval(deps Deps, c fiber.Ctx, s *sse.Stream) error {
	approval := c.Locals(approvalLocalsKey{}).(approvalContext)
	call := approval.call
	sink := sseSink{s}
	ctx := s.Context()

	var result, status string
	if approval.decision == approvalDecisionApprove {
		result, status = executeToolCall(deps, ctx, llm.ToolCall{
			ID:        call.ToolCall.CallID,
			Name:      call.ToolCall.Name,
			Arguments: call.ToolCall.Arguments,
		})
	} else {
		result, status = deniedByUserResult, toolCallStatusDenied
	}

	remaining, err := deps.Store.ResolveToolCall(ctx, db.UpdateToolCallResultParams{
		ID:     call.ToolCall.ID,
		Result: result,
		Status: status,
	}, call.SessionID)
	if err != nil {
		return sendChatError(s, err)
	}

	if err := sink.Send("decision", approvalDecisionEvent{
		ID:     call.ToolCall.ID,
		Result: result,
		Status: status,
	}); err != nil {
		return err
	}

	if remaining > 0 {
		return sink.Send("done", chatDoneEvent{FinishReason: finishAwaitingApproval})
	}

	history, err := buildHistory(ctx, deps.Store, call.SessionID)
	if err != nil {
		return sendChatError(s, err)
	}
	if err := runToolLoop(deps, sink, ctx, call.SessionID, llm.Request{Messages: history}); err != nil {
		return sendChatError(s, err)
	}
	return nil
}
