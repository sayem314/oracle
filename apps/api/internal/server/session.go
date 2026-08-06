package server

import (
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

const sessionListLimit = 200

type sessionResponse struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func sessionToResponse(s db.Session) sessionResponse {
	return sessionResponse{
		ID:        s.ID,
		Title:     s.Title,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// resolveSession loads a session by path id or returns 404 so existence is
// never leaked.
func resolveSession(deps Deps, c fiber.Ctx) (db.Session, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return db.Session{}, fiber.NewError(fiber.StatusBadRequest, "invalid session id")
	}

	session, err := deps.Store.GetSession(c.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.Session{}, fiber.NewError(fiber.StatusNotFound, "session not found")
		}
		return db.Session{}, err
	}
	return session, nil
}

func newListSessionsHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		sessions, err := deps.Store.ListSessions(c.Context(), db.ListSessionsParams{
			Limit:  sessionListLimit,
			Offset: 0,
		})
		if err != nil {
			return err
		}
		out := make([]sessionResponse, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, sessionToResponse(s))
		}
		return c.JSON(out)
	}
}

type sessionToolCallResponse struct {
	ID        int64  `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
	Status    string `json:"status"`
}

type sessionMessageResponse struct {
	ID        int64                     `json:"id"`
	Role      string                    `json:"role"`
	Content   string                    `json:"content"`
	CreatedAt time.Time                 `json:"created_at"`
	ToolCalls []sessionToolCallResponse `json:"tool_calls,omitempty"`
}

func newListMessagesHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		session, err := resolveSession(deps, c)
		if err != nil {
			return err
		}

		ctx := c.Context()
		messages, err := deps.Store.ListMessages(ctx, db.ListMessagesParams{
			SessionID: session.ID,
			Limit:     chat.HistoryLimit,
			Offset:    0,
		})
		if err != nil {
			return err
		}

		toolCalls, err := deps.Store.ListToolCallsBySession(ctx, session.ID)
		if err != nil {
			return err
		}
		callsByMessage := make(map[int64][]sessionToolCallResponse)
		for _, tc := range toolCalls {
			callsByMessage[tc.MessageID] = append(callsByMessage[tc.MessageID], sessionToolCallResponse{
				ID:        tc.ID,
				CallID:    tc.CallID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
				Result:    tc.Result,
				Status:    tc.Status,
			})
		}

		out := make([]sessionMessageResponse, 0, len(messages))
		for _, m := range messages {
			out = append(out, sessionMessageResponse{
				ID:        m.ID,
				Role:      m.Role,
				Content:   m.Content,
				CreatedAt: m.CreatedAt,
				ToolCalls: callsByMessage[m.ID],
			})
		}
		return c.JSON(out)
	}
}

type updateSessionRequest struct {
	Title *string `json:"title"`
}

func newUpdateSessionHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		session, err := resolveSession(deps, c)
		if err != nil {
			return err
		}

		var req updateSessionRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
		if req.Title == nil {
			return fiber.NewError(fiber.StatusBadRequest, "title is required")
		}

		if err := deps.Store.UpdateSessionTitle(c.Context(), db.UpdateSessionTitleParams{
			Title: *req.Title,
			ID:    session.ID,
		}); err != nil {
			return err
		}

		session.Title = *req.Title
		return c.JSON(sessionToResponse(session))
	}
}

func newDeleteSessionHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		session, err := resolveSession(deps, c)
		if err != nil {
			return err
		}
		if err := deps.Store.DeleteSession(c.Context(), session.ID); err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}
