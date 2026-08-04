package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

func postChatJSON(t *testing.T, app *fiber.App, cookie, body string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func postChat(t *testing.T, app *fiber.App, cookie string, body map[string]any) *http.Response {
	t.Helper()

	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return postChatJSON(t, app, cookie, string(raw))
}

type sseFrame struct {
	Name string
	Data string
}

func parseSSE(t *testing.T, r io.Reader) []sseFrame {
	t.Helper()

	var frames []sseFrame
	var name string
	var data []string

	flush := func() {
		if name != "" || len(data) > 0 {
			frames = append(frames, sseFrame{Name: name, Data: strings.Join(data, "\n")})
		}
		name = ""
		data = nil
	}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		switch field {
		case "event":
			name = strings.TrimPrefix(value, " ")
		case "data":
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
	require.NoError(t, scanner.Err())
	flush()

	return frames
}

func framesByName(frames []sseFrame, name string) []sseFrame {
	var out []sseFrame
	for _, f := range frames {
		if f.Name == name {
			out = append(out, f)
		}
	}
	return out
}

func decodeFrame(t *testing.T, f sseFrame, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal([]byte(f.Data), v))
}

type fakeProvider struct {
	chatErr   error
	chunks    []llm.Chunk
	streamErr error

	gotRequest llm.Request
}

func (p *fakeProvider) Chat(_ context.Context, req llm.Request) (llm.Stream, error) {
	p.gotRequest = req
	if p.chatErr != nil {
		return nil, p.chatErr
	}
	return &fakeStream{chunks: p.chunks, err: p.streamErr}, nil
}

type fakeStream struct {
	chunks []llm.Chunk
	err    error
	pos    int
}

func (s *fakeStream) Next() bool {
	if s.pos >= len(s.chunks) {
		return false
	}
	s.pos++
	return true
}

func (s *fakeStream) Current() llm.Chunk { return s.chunks[s.pos-1] }

func (s *fakeStream) Err() error {
	if s.pos < len(s.chunks) {
		return nil
	}
	return s.err
}

func (s *fakeStream) Close() error { return nil }

type startEvent struct {
	SessionID     int64 `json:"session_id"`
	UserMessageID int64 `json:"user_message_id"`
}

type deltaEvent struct {
	Content string `json:"content"`
}

type doneEvent struct {
	MessageID    int64  `json:"message_id"`
	FinishReason string `json:"finish_reason"`
}

type errorEvent struct {
	Message string `json:"message"`
}

func decodeErrorMessage(t *testing.T, res *http.Response) string {
	t.Helper()

	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	return body.Error
}

func TestChatNewSession(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, userID := signUp(t, app, dbConn, "owner@example.com")

	res := postChat(t, app, cookie, map[string]any{"message": "hi"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, res.Header.Get("Content-Type"), "text/event-stream")

	frames := parseSSE(t, res.Body)

	starts := framesByName(frames, "start")
	require.Len(t, starts, 1)
	var start startEvent
	decodeFrame(t, starts[0], &start)
	assert.Positive(t, start.SessionID)
	assert.Positive(t, start.UserMessageID)

	var text strings.Builder
	for _, f := range framesByName(frames, "delta") {
		var delta deltaEvent
		decodeFrame(t, f, &delta)
		text.WriteString(delta.Content)
	}
	assert.Equal(t, "This is a mock response from oracle.", text.String())

	dones := framesByName(frames, "done")
	require.Len(t, dones, 1)
	var done doneEvent
	decodeFrame(t, dones[0], &done)
	assert.Positive(t, done.MessageID)
	assert.Equal(t, "stop", done.FinishReason)

	assert.Empty(t, framesByName(frames, "error"))

	session, err := s.GetSession(t.Context(), start.SessionID)
	require.NoError(t, err)
	assert.Equal(t, userID, session.UserID)

	count, err := s.CountMessages(t.Context(), start.SessionID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestChatStreamsHistoryToProvider(t *testing.T) {
	provider := &fakeProvider{chunks: []llm.Chunk{
		{Delta: "Hello"},
		{Delta: " world"},
		{FinishReason: "stop"},
	}}
	app, s, dbConn := newTestApp(t, provider)
	cookie, userID := signUp(t, app, dbConn, "owner@example.com")

	ctx := t.Context()
	session, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: userID})
	require.NoError(t, err)
	_, err = s.AppendMessage(ctx, db.AppendMessageParams{SessionID: session.ID, Role: "user", Content: "hi"})
	require.NoError(t, err)
	_, err = s.AppendMessage(ctx, db.AppendMessageParams{SessionID: session.ID, Role: "assistant", Content: "hello"})
	require.NoError(t, err)

	res := postChat(t, app, cookie, map[string]any{
		"session_id": session.ID,
		"message":    "how are you?",
		"model":      "gpt-test",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	frames := parseSSE(t, res.Body)

	starts := framesByName(frames, "start")
	require.Len(t, starts, 1)
	var start startEvent
	decodeFrame(t, starts[0], &start)
	assert.Equal(t, session.ID, start.SessionID)

	var text strings.Builder
	for _, f := range framesByName(frames, "delta") {
		var delta deltaEvent
		decodeFrame(t, f, &delta)
		text.WriteString(delta.Content)
	}
	assert.Equal(t, "Hello world", text.String())

	require.Len(t, framesByName(frames, "done"), 1)
	assert.Empty(t, framesByName(frames, "error"))

	assert.Equal(t, "gpt-test", provider.gotRequest.Model)
	require.Equal(t, []llm.Message{
		{Role: llm.RoleUser, Content: "hi"},
		{Role: llm.RoleAssistant, Content: "hello"},
		{Role: llm.RoleUser, Content: "how are you?"},
	}, provider.gotRequest.Messages)

	count, err := s.CountMessages(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(4), count)
}

func TestChatEmptyMessage(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := postChat(t, app, cookie, map[string]any{"message": "   "})
	require.Equal(t, http.StatusBadRequest, res.StatusCode)

	assert.Equal(t, "message is required", decodeErrorMessage(t, res))
}

func TestChatInvalidJSON(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := postChatJSON(t, app, cookie, "{")
	require.Equal(t, http.StatusBadRequest, res.StatusCode)

	assert.Equal(t, "invalid request body", decodeErrorMessage(t, res))
}

func TestChatUnknownSession(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := postChat(t, app, cookie, map[string]any{"session_id": 999, "message": "hi"})
	require.Equal(t, http.StatusNotFound, res.StatusCode)

	assert.Equal(t, "session not found", decodeErrorMessage(t, res))
}

func TestChatProviderError(t *testing.T) {
	provider := &fakeProvider{chatErr: errors.New("provider down")}
	app, s, dbConn := newTestApp(t, provider)
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := postChat(t, app, cookie, map[string]any{"message": "hi"})
	require.Equal(t, http.StatusOK, res.StatusCode)

	frames := parseSSE(t, res.Body)

	errs := framesByName(frames, "error")
	require.Len(t, errs, 1)
	var chatErr errorEvent
	decodeFrame(t, errs[0], &chatErr)
	assert.Contains(t, chatErr.Message, "provider down")

	assert.Empty(t, framesByName(frames, "delta"))
	assert.Empty(t, framesByName(frames, "done"))

	starts := framesByName(frames, "start")
	require.Len(t, starts, 1)
	var start startEvent
	decodeFrame(t, starts[0], &start)

	count, err := s.CountMessages(t.Context(), start.SessionID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestChatMidStreamError(t *testing.T) {
	provider := &fakeProvider{
		chunks:    []llm.Chunk{{Delta: "partial"}},
		streamErr: errors.New("connection lost"),
	}
	app, s, dbConn := newTestApp(t, provider)
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := postChat(t, app, cookie, map[string]any{"message": "hi"})
	require.Equal(t, http.StatusOK, res.StatusCode)

	frames := parseSSE(t, res.Body)

	require.Len(t, framesByName(frames, "delta"), 1)

	errs := framesByName(frames, "error")
	require.Len(t, errs, 1)
	var chatErr errorEvent
	decodeFrame(t, errs[0], &chatErr)
	assert.Contains(t, chatErr.Message, "connection lost")

	assert.Empty(t, framesByName(frames, "done"))

	starts := framesByName(frames, "start")
	require.Len(t, starts, 1)
	var start startEvent
	decodeFrame(t, starts[0], &start)

	count, err := s.CountMessages(t.Context(), start.SessionID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}
