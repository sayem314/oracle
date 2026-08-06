package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/permission"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

func postApprovalJSON(t *testing.T, app *fiber.App, cookie, body string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func postApproval(t *testing.T, app *fiber.App, cookie string, body map[string]any) *http.Response {
	t.Helper()

	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return postApprovalJSON(t, app, cookie, string(raw))
}

type approvalRequiredEvent struct {
	ID         int64  `json:"id"`
	ToolCallID string `json:"tool_call_id"`
	MessageID  int64  `json:"message_id"`
	Name       string `json:"name"`
	Arguments  string `json:"arguments"`
}

type decisionEvent struct {
	ID     int64  `json:"id"`
	Result string `json:"result"`
	Status string `json:"status"`
}

var askAll = permission.NewRuleset(permission.Ask, nil)

// chatUntilApprovalRequired runs one chat turn against a provider that asks
// for the clock tool and returns the session plus the pending tool call row.
func chatUntilApprovalRequired(t *testing.T, app *fiber.App, cookie string) (int64, int64) {
	t.Helper()

	res := postChat(t, app, cookie, map[string]any{"message": "what time is it?"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	frames := parseSSE(t, res.Body)

	starts := framesByName(frames, "start")
	require.Len(t, starts, 1)
	var start startEvent
	decodeFrame(t, starts[0], &start)

	pending := framesByName(frames, "approval_required")
	require.Len(t, pending, 1)
	var ap approvalRequiredEvent
	decodeFrame(t, pending[0], &ap)
	assert.Equal(t, "clock", ap.Name)
	assert.Equal(t, "call_1", ap.ToolCallID)

	dones := framesByName(frames, "done")
	require.Len(t, dones, 1)
	var done doneEvent
	decodeFrame(t, dones[0], &done)
	assert.Equal(t, "awaiting_approval", done.FinishReason)

	return start.SessionID, ap.ID
}

func TestApprovalApproveResumesRun(t *testing.T) {
	provider := &scriptedProvider{rounds: [][]llm.Chunk{
		{{FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "clock", Arguments: "{}"}}}},
		{{Delta: "It is 12:00"}, {FinishReason: "stop"}},
	}}
	app, s, dbConn := newTestAppFull(t, provider, clockRegistry(t), askAll)
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	sessionID, rowID := chatUntilApprovalRequired(t, app, cookie)

	// New chat is blocked while a decision is pending.
	res := postChat(t, app, cookie, map[string]any{"session_id": sessionID, "message": "hello?"})
	require.Equal(t, http.StatusConflict, res.StatusCode)
	assert.Equal(t, "tool approval pending", decodeErrorMessage(t, res))

	res = postApproval(t, app, cookie, map[string]any{"id": rowID, "decision": "approve"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	frames := parseSSE(t, res.Body)

	decisions := framesByName(frames, "decision")
	require.Len(t, decisions, 1)
	var decision decisionEvent
	decodeFrame(t, decisions[0], &decision)
	assert.Equal(t, rowID, decision.ID)
	assert.Equal(t, "12:00", decision.Result)
	assert.Equal(t, "done", decision.Status)

	var text strings.Builder
	for _, f := range framesByName(frames, "delta") {
		var delta deltaEvent
		decodeFrame(t, f, &delta)
		text.WriteString(delta.Content)
	}
	assert.Equal(t, "It is 12:00", text.String())

	dones := framesByName(frames, "done")
	require.Len(t, dones, 1)
	var done doneEvent
	decodeFrame(t, dones[0], &done)
	assert.Equal(t, "stop", done.FinishReason)
	assert.Positive(t, done.MessageID)
	assert.Empty(t, framesByName(frames, "error"))

	// The resumed round replayed the persisted history: user message, assistant
	// tool-call turn, and the approved tool result.
	require.Len(t, provider.gotRequests, 2)
	require.Len(t, provider.gotRequests[1].Messages, 3)
	assert.Equal(t, llm.RoleUser, provider.gotRequests[1].Messages[0].Role)
	assert.Len(t, provider.gotRequests[1].Messages[1].ToolCalls, 1)
	assert.Equal(t, llm.RoleTool, provider.gotRequests[1].Messages[2].Role)
	assert.Equal(t, "12:00", provider.gotRequests[1].Messages[2].Content)

	calls, err := s.ListToolCallsBySession(t.Context(), sessionID)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, "done", calls[0].Status)
	assert.Equal(t, "12:00", calls[0].Result)

	msgs, err := s.ListMessages(t.Context(), db.ListMessagesParams{SessionID: sessionID, Limit: 10})
	require.NoError(t, err)
	assert.Len(t, msgs, 3)
}

func TestApprovalDenyFeedsBackToModel(t *testing.T) {
	provider := &scriptedProvider{rounds: [][]llm.Chunk{
		{{FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "clock", Arguments: "{}"}}}},
		{{Delta: "ok, no time for you"}, {FinishReason: "stop"}},
	}}
	app, s, dbConn := newTestAppFull(t, provider, clockRegistry(t), askAll)
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	sessionID, rowID := chatUntilApprovalRequired(t, app, cookie)

	res := postApproval(t, app, cookie, map[string]any{"id": rowID, "decision": "deny"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	frames := parseSSE(t, res.Body)

	decisions := framesByName(frames, "decision")
	require.Len(t, decisions, 1)
	var decision decisionEvent
	decodeFrame(t, decisions[0], &decision)
	assert.Equal(t, "denied by user", decision.Result)
	assert.Equal(t, "denied", decision.Status)

	require.Len(t, framesByName(frames, "done"), 1)
	assert.Empty(t, framesByName(frames, "error"))

	require.Len(t, provider.gotRequests, 2)
	last := provider.gotRequests[1].Messages[len(provider.gotRequests[1].Messages)-1]
	assert.Equal(t, llm.RoleTool, last.Role)
	assert.Equal(t, "denied by user", last.Content)

	calls, err := s.ListToolCallsBySession(t.Context(), sessionID)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, "denied", calls[0].Status)
	assert.Equal(t, "denied by user", calls[0].Result)
}

func TestApprovalWaitsForEveryCall(t *testing.T) {
	provider := &scriptedProvider{rounds: [][]llm.Chunk{
		{{FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "clock", Arguments: "{}"},
			{ID: "call_2", Name: "clock", Arguments: "{}"},
		}}},
		{{Delta: "done"}, {FinishReason: "stop"}},
	}}
	app, _, dbConn := newTestAppFull(t, provider, clockRegistry(t), askAll)
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := postChat(t, app, cookie, map[string]any{"message": "what time is it?"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	frames := parseSSE(t, res.Body)
	pending := framesByName(frames, "approval_required")
	require.Len(t, pending, 2)
	var first, second approvalRequiredEvent
	decodeFrame(t, pending[0], &first)
	decodeFrame(t, pending[1], &second)

	// First decision records but does not resume.
	res = postApproval(t, app, cookie, map[string]any{"id": first.ID, "decision": "approve"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	frames = parseSSE(t, res.Body)
	require.Len(t, framesByName(frames, "decision"), 1)
	dones := framesByName(frames, "done")
	require.Len(t, dones, 1)
	var done doneEvent
	decodeFrame(t, dones[0], &done)
	assert.Equal(t, "awaiting_approval", done.FinishReason)
	assert.Empty(t, framesByName(frames, "delta"))
	require.Len(t, provider.gotRequests, 1)

	// Second decision resumes the run.
	res = postApproval(t, app, cookie, map[string]any{"id": second.ID, "decision": "approve"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	frames = parseSSE(t, res.Body)
	require.Len(t, framesByName(frames, "decision"), 1)
	require.Len(t, framesByName(frames, "done"), 1)
	assert.NotEmpty(t, framesByName(frames, "delta"))
	require.Len(t, provider.gotRequests, 2)
}

func TestChatDeniedByPolicy(t *testing.T) {
	provider := &scriptedProvider{rounds: [][]llm.Chunk{
		{{FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "clock", Arguments: "{}"}}}},
		{{Delta: "not allowed apparently"}, {FinishReason: "stop"}},
	}}
	perms := permission.NewRuleset(permission.Allow, []permission.Rule{
		{Tool: "clock", Verdict: permission.Deny},
	})
	app, s, dbConn := newTestAppFull(t, provider, clockRegistry(t), perms)
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := postChat(t, app, cookie, map[string]any{"message": "what time is it?"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	frames := parseSSE(t, res.Body)

	assert.Empty(t, framesByName(frames, "approval_required"))
	toolResults := framesByName(frames, "tool_result")
	require.Len(t, toolResults, 1)
	var tr toolResultEvent
	decodeFrame(t, toolResults[0], &tr)
	assert.Contains(t, tr.Result, "denied by policy")

	require.Len(t, framesByName(frames, "done"), 1)
	assert.Empty(t, framesByName(frames, "error"))

	require.Len(t, provider.gotRequests, 2)
	last := provider.gotRequests[1].Messages[len(provider.gotRequests[1].Messages)-1]
	assert.Contains(t, last.Content, "denied by policy")

	starts := framesByName(frames, "start")
	require.Len(t, starts, 1)
	var start startEvent
	decodeFrame(t, starts[0], &start)
	calls, err := s.ListToolCallsBySession(t.Context(), start.SessionID)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, "denied", calls[0].Status)
}

func TestApprovalValidationAndOwnership(t *testing.T) {
	provider := &scriptedProvider{rounds: [][]llm.Chunk{
		{{FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "clock", Arguments: "{}"}}}},
		{{Delta: "ok"}, {FinishReason: "stop"}},
	}}
	app, _, dbConn := newTestAppFull(t, provider, clockRegistry(t), askAll)
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	_, rowID := chatUntilApprovalRequired(t, app, cookie)

	t.Run("unauthenticated", func(t *testing.T) {
		res := postApproval(t, app, "", map[string]any{"id": rowID, "decision": "approve"})
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	t.Run("invalid decision", func(t *testing.T) {
		res := postApproval(t, app, cookie, map[string]any{"id": rowID, "decision": "maybe"})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "decision must be approve or deny", decodeErrorMessage(t, res))
	})

	t.Run("missing id", func(t *testing.T) {
		res := postApproval(t, app, cookie, map[string]any{"decision": "approve"})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "id is required", decodeErrorMessage(t, res))
	})

	t.Run("unknown tool call", func(t *testing.T) {
		res := postApproval(t, app, cookie, map[string]any{"id": 99999, "decision": "approve"})
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
	})

	t.Run("approve then re-decide", func(t *testing.T) {
		res := postApproval(t, app, cookie, map[string]any{"id": rowID, "decision": "approve"})
		require.Equal(t, http.StatusOK, res.StatusCode)
		parseSSE(t, res.Body)

		res = postApproval(t, app, cookie, map[string]any{"id": rowID, "decision": "deny"})
		assert.Equal(t, http.StatusConflict, res.StatusCode)
		assert.Equal(t, "tool call is not awaiting approval", decodeErrorMessage(t, res))
	})
}
