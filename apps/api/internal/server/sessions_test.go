package server_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type sessionResponse struct {
	ID             int64      `json:"id"`
	Title          string     `json:"title"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LoopEnabled    bool       `json:"loop_enabled"`
	LoopInterval   string     `json:"loop_interval"`
	LoopNextRunAt  *time.Time `json:"loop_next_run_at"`
	LoopLastRunAt  *time.Time `json:"loop_last_run_at"`
	LoopLastStatus string     `json:"loop_last_status"`
	LoopError      string     `json:"loop_error"`
	LoopRunCount   int64      `json:"loop_run_count"`
}

type sessionMessage struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		ID     int64  `json:"id"`
		CallID string `json:"call_id"`
		Name   string `json:"name"`
		Result string `json:"result"`
		Status string `json:"status"`
	} `json:"tool_calls"`
}

func decodeSessionList(t *testing.T, res *http.Response) []sessionResponse {
	t.Helper()
	var sessions []sessionResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&sessions))
	return sessions
}

func decodeSessionMessages(t *testing.T, res *http.Response) []sessionMessage {
	t.Helper()
	var msgs []sessionMessage
	require.NoError(t, json.NewDecoder(res.Body).Decode(&msgs))
	return msgs
}

func TestListSessions(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	_, err := s.CreateSession(t.Context(), "mine")
	require.NoError(t, err)
	_, err = s.CreateSession(t.Context(), "second")
	require.NoError(t, err)

	res := doRequest(t, app, http.MethodGet, "/api/v1/sessions", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	sessions := decodeSessionList(t, res)
	require.Len(t, sessions, 2)
}

func TestListSessionMessagesWithToolCalls(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	session, err := s.CreateSession(t.Context(), "")
	require.NoError(t, err)

	_, err = s.AppendMessage(t.Context(), db.AppendMessageParams{SessionID: session.ID, Role: "user", Content: "what time is it?"})
	require.NoError(t, err)
	assistant, err := s.AppendMessage(t.Context(), db.AppendMessageParams{SessionID: session.ID, Role: "assistant", Content: "checking"})
	require.NoError(t, err)
	call, err := s.InsertToolCall(t.Context(), db.InsertToolCallParams{
		MessageID: assistant.ID,
		CallID:    "call_1",
		Name:      "clock",
		Arguments: "{}",
		Status:    "pending",
	})
	require.NoError(t, err)
	err = s.UpdateToolCallResult(t.Context(), db.UpdateToolCallResultParams{
		ID:     call.ID,
		Result: "12:00",
		Status: "done",
	})
	require.NoError(t, err)
	_, err = s.AppendMessage(t.Context(), db.AppendMessageParams{SessionID: session.ID, Role: "assistant", Content: "It is 12:00"})
	require.NoError(t, err)

	res := doRequest(t, app, http.MethodGet, "/api/v1/sessions/"+itoa(session.ID)+"/messages", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	msgs := decodeSessionMessages(t, res)
	require.Len(t, msgs, 3)

	assert.Equal(t, "user", msgs[0].Role)
	assert.Empty(t, msgs[0].ToolCalls)

	assert.Equal(t, "assistant", msgs[1].Role)
	require.Len(t, msgs[1].ToolCalls, 1)
	assert.Equal(t, "clock", msgs[1].ToolCalls[0].Name)
	assert.Equal(t, "12:00", msgs[1].ToolCalls[0].Result)
	assert.Equal(t, "done", msgs[1].ToolCalls[0].Status)

	assert.Equal(t, "It is 12:00", msgs[2].Content)
}

func TestRenameSession(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	session, err := s.CreateSession(t.Context(), "")
	require.NoError(t, err)

	res := doRequest(t, app, http.MethodPatch, "/api/v1/sessions/"+itoa(session.ID), cookie, map[string]any{"title": "renamed"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	var updated sessionResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&updated))
	assert.Equal(t, "renamed", updated.Title)

	got, err := s.GetSession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.Title)

	res = doRequest(t, app, http.MethodPatch, "/api/v1/sessions/"+itoa(session.ID), cookie, map[string]any{})
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	assert.Equal(t, "no fields to update", decodeErrorMessage(t, res))
}

func TestUpdateSessionLoopFields(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	session, err := s.CreateSession(t.Context(), "")
	require.NoError(t, err)

	res := doRequest(t, app, http.MethodPatch, "/api/v1/sessions/"+itoa(session.ID), cookie, map[string]any{
		"loop_enabled":  true,
		"loop_interval": "30s",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)
	var updated sessionResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&updated))
	assert.True(t, updated.LoopEnabled)
	assert.Equal(t, "30s", updated.LoopInterval)
	require.NotNil(t, updated.LoopNextRunAt, "enabling should queue the first run")

	got, err := s.GetSession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.LoopEnabled)
	assert.True(t, got.LoopNextRunAt.Valid)

	res = doRequest(t, app, http.MethodPatch, "/api/v1/sessions/"+itoa(session.ID), cookie, map[string]any{
		"loop_enabled": false,
	})
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NoError(t, json.NewDecoder(res.Body).Decode(&updated))
	assert.False(t, updated.LoopEnabled)
	assert.Nil(t, updated.LoopNextRunAt, "disabling should cancel queued runs")

	res = doRequest(t, app, http.MethodPatch, "/api/v1/sessions/"+itoa(session.ID), cookie, map[string]any{
		"loop_interval": "not-a-duration",
	})
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	assert.Equal(t, "loop_interval must be a duration like \"30s\" or \"5m\"", decodeErrorMessage(t, res))
}

func TestRunLoopNow(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	session, err := s.CreateSession(t.Context(), "")
	require.NoError(t, err)

	res := doRequest(t, app, http.MethodPost, "/api/v1/sessions/"+itoa(session.ID)+"/loop/run", cookie, map[string]any{})
	require.Equal(t, http.StatusAccepted, res.StatusCode)
	var updated sessionResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&updated))
	require.NotNil(t, updated.LoopNextRunAt, "run now should queue an iteration")

	got, err := s.GetSession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.True(t, got.LoopNextRunAt.Valid)

	require.NoError(t, s.UpdateSessionLoopResult(t.Context(), db.UpdateSessionLoopResultParams{
		ID:             session.ID,
		LoopLastStatus: "running",
		LoopError:      "",
		LoopNextRunAt:  sql.NullInt64{},
	}))

	res = doRequest(t, app, http.MethodPost, "/api/v1/sessions/"+itoa(session.ID)+"/loop/run", cookie, map[string]any{})
	require.Equal(t, http.StatusConflict, res.StatusCode)
	assert.Equal(t, "loop run in progress", decodeErrorMessage(t, res))
}

func TestChatRejectedWhileLoopRunning(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	session, err := s.CreateSession(t.Context(), "")
	require.NoError(t, err)
	require.NoError(t, s.UpdateSessionLoopResult(t.Context(), db.UpdateSessionLoopResultParams{
		ID:             session.ID,
		LoopLastStatus: "running",
		LoopError:      "",
		LoopNextRunAt:  sql.NullInt64{},
	}))

	res := doRequest(t, app, http.MethodPost, "/api/v1/chat", cookie, map[string]any{
		"session_id": session.ID,
		"message":    "hello",
	})
	require.Equal(t, http.StatusConflict, res.StatusCode)
	assert.Equal(t, "session loop is running", decodeErrorMessage(t, res))
}

func TestDeleteSession(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	session, err := s.CreateSession(t.Context(), "")
	require.NoError(t, err)
	msg, err := s.AppendMessage(t.Context(), db.AppendMessageParams{SessionID: session.ID, Role: "user", Content: "hi"})
	require.NoError(t, err)
	_, err = s.InsertToolCall(t.Context(), db.InsertToolCallParams{
		MessageID: msg.ID,
		CallID:    "call_1",
		Name:      "clock",
		Status:    "done",
	})
	require.NoError(t, err)

	res := doRequest(t, app, http.MethodDelete, "/api/v1/sessions/"+itoa(session.ID), cookie, nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)

	messages, err := s.ListMessages(t.Context(), db.ListMessagesParams{SessionID: session.ID, Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, messages)

	calls, err := s.ListToolCallsBySession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Empty(t, calls)

	res = doRequest(t, app, http.MethodDelete, "/api/v1/sessions/"+itoa(session.ID), cookie, nil)
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}
