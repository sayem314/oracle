package server

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/sse"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

const chatHistoryLimit = 1000

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

	messages, err := deps.Store.ListMessages(ctx, db.ListMessagesParams{
		SessionID: sessionID,
		Limit:     chatHistoryLimit,
	})
	if err != nil {
		return chatContext{}, err
	}

	llmReq := llm.Request{Model: req.Model}
	for _, m := range messages {
		llmReq.Messages = append(llmReq.Messages, llm.Message{Role: llm.Role(m.Role), Content: m.Content})
	}

	return chatContext{sessionID: sessionID, userMessageID: userMsg.ID, request: llmReq}, nil
}

func streamChat(deps Deps, c fiber.Ctx, s *sse.Stream) error {
	chat := c.Locals(chatLocalsKey{}).(chatContext)

	if err := s.Event(sse.Event{Name: "start", Data: chatStartEvent{
		SessionID:     chat.sessionID,
		UserMessageID: chat.userMessageID,
	}}); err != nil {
		return err
	}

	stream, err := deps.LLM.Chat(s.Context(), chat.request)
	if err != nil {
		return sendChatError(s, err)
	}
	defer func() { _ = stream.Close() }()

	var text strings.Builder
	var finishReason string
	for stream.Next() {
		chunk := stream.Current()
		if chunk.Delta != "" {
			text.WriteString(chunk.Delta)
			if err := s.Event(sse.Event{Name: "delta", Data: chatDeltaEvent{Content: chunk.Delta}}); err != nil {
				return err
			}
		}
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
	}
	if err := stream.Err(); err != nil {
		return sendChatError(s, err)
	}

	ctx := s.Context()
	assistant, err := deps.Store.AppendMessage(ctx, db.AppendMessageParams{
		SessionID: chat.sessionID,
		Role:      string(llm.RoleAssistant),
		Content:   text.String(),
	})
	if err != nil {
		return sendChatError(s, err)
	}
	if err := deps.Store.TouchSession(ctx, chat.sessionID); err != nil {
		return sendChatError(s, err)
	}

	return s.Event(sse.Event{Name: "done", Data: chatDoneEvent{
		MessageID:    assistant.ID,
		FinishReason: finishReason,
	}})
}

func sendChatError(s *sse.Stream, err error) error {
	if werr := s.Event(sse.Event{Name: "error", Data: chatErrorEvent{Message: err.Error()}}); werr != nil {
		return werr
	}
	return nil
}
