package server

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/sse"

	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type chatLocalsKey struct{}

type chatContext struct {
	sessionID     int64
	userID        int64
	providerID    int64
	userMessageID int64
	request       llm.Request
}

type chatRequest struct {
	SessionID  *int64 `json:"session_id"`
	Message    string `json:"message"`
	Model      string `json:"model"`
	ProviderID int64  `json:"provider_id"`
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
		cc, err := prepareChat(deps, c)
		if err != nil {
			return err
		}
		c.Locals(chatLocalsKey{}, cc)
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
	if req.ProviderID < 0 {
		return chatContext{}, fiber.NewError(fiber.StatusBadRequest, "invalid provider_id")
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

	history, err := deps.Chat.BuildHistory(ctx, sessionID)
	if err != nil {
		return chatContext{}, err
	}

	return chatContext{
		sessionID:     sessionID,
		userID:        userID,
		providerID:    req.ProviderID,
		userMessageID: userMsg.ID,
		request:       llm.Request{Model: req.Model, Messages: history},
	}, nil
}

func streamChat(deps Deps, c fiber.Ctx, s *sse.Stream) error {
	cc := c.Locals(chatLocalsKey{}).(chatContext)
	sink := sseSink{s}

	if err := sink.Send("start", chat.StartEvent{
		SessionID:     cc.sessionID,
		UserMessageID: cc.userMessageID,
	}); err != nil {
		return err
	}

	if err := deps.Chat.Run(s.Context(), sink, cc.sessionID, cc.userID, cc.providerID, cc.request); err != nil {
		return sendChatError(s, err)
	}
	return nil
}

func sendChatError(s *sse.Stream, err error) error {
	if werr := s.Event(sse.Event{Name: "error", Data: chat.ErrorEvent{Message: err.Error()}}); werr != nil {
		return werr
	}
	return nil
}
