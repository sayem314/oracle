package scheduler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/permission"
	"github.com/sayem314/oracle/apps/api/internal/scheduler"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

func newStore(t *testing.T) (store.Store, *sql.DB) {
	t.Helper()

	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") + "?_pragma=foreign_keys(ON)"
	dbConn, err := store.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	_, err = store.Migrate(dbConn)
	require.NoError(t, err)

	return store.New(dbConn), dbConn
}

func seedUser(t *testing.T, dbConn *sql.DB, id int64) {
	t.Helper()

	_, err := dbConn.Exec(
		"INSERT INTO auth_users (id, email) VALUES (?, ?)",
		id, fmt.Sprintf("user%d@example.com", id),
	)
	require.NoError(t, err)
}

type fakeStream struct {
	chunks []llm.Chunk
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
func (s *fakeStream) Err() error         { return nil }
func (s *fakeStream) Close() error       { return nil }

type scriptedProvider struct {
	rounds      [][]llm.Chunk
	gotRequests []llm.Request
}

func (p *scriptedProvider) Chat(_ context.Context, req llm.Request) (llm.Stream, error) {
	p.gotRequests = append(p.gotRequests, req)
	var chunks []llm.Chunk
	if len(p.gotRequests)-1 < len(p.rounds) {
		chunks = p.rounds[len(p.gotRequests)-1]
	}
	return &fakeStream{chunks: chunks}, nil
}

func clockRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	r := tool.NewRegistry()
	require.NoError(t, r.Register(tool.Tool{
		Definition: llm.Tool{Name: "clock", Description: "Return the current time."},
		Execute:    func(_ context.Context, _ json.RawMessage) (string, error) { return "12:00", nil },
	}))
	return r
}

func createDueJob(t *testing.T, s store.Store, userID int64, schedule, prompt string, enabled int64) db.Job {
	t.Helper()

	job, err := s.CreateJob(t.Context(), db.CreateJobParams{
		UserID:    userID,
		Schedule:  schedule,
		Prompt:    prompt,
		Enabled:   enabled,
		NextRunAt: sql.NullTime{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	})
	require.NoError(t, err)
	return job
}

func TestNextAfter(t *testing.T) {
	from := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	next, err := scheduler.NextAfter("0 8 * * *", from)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC), next)

	_, err = scheduler.NextAfter("not a cron", from)
	require.Error(t, err)

	_, err = scheduler.NextAfter("0 8 * *", from)
	require.Error(t, err)
}

func TestRunOnceExecutesDueJob(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)

	provider := &scriptedProvider{rounds: [][]llm.Chunk{
		{{Delta: "good morning"}, {FinishReason: "stop"}},
	}}
	engine := &chat.Engine{
		Store:       s,
		LLM:         &chat.LLMResolver{Store: s, Default: provider},
		Tools:       tool.NewRegistry(),
		Permissions: permission.NewRuleset(permission.Allow, nil),
		Headless:    true,
	}
	sched := scheduler.New(s, engine, time.Minute)

	job := createDueJob(t, s, 1, "0 8 * * *", "brief me", 1)

	require.NoError(t, sched.RunOnce(t.Context()))

	got, err := s.GetJob(t.Context(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, "ok", got.LastStatus)
	assert.True(t, got.LastRunAt.Valid)
	// next_run_at advanced past the fixed 2020 due time.
	require.True(t, got.NextRunAt.Valid)
	assert.True(t, got.NextRunAt.Time.After(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))

	// A session was created and linked, with the prompt and the answer persisted.
	require.True(t, got.SessionID.Valid)
	msgs, err := s.ListMessages(t.Context(), db.ListMessagesParams{SessionID: got.SessionID.Int64, Limit: 10})
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "brief me", msgs[0].Content)
	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "good morning", msgs[1].Content)

	// A second tick does not re-run the job: next_run_at is now in the future.
	require.NoError(t, sched.RunOnce(t.Context()))
	msgs, err = s.ListMessages(t.Context(), db.ListMessagesParams{SessionID: got.SessionID.Int64, Limit: 20})
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
}

// TestRunOnceUsesOwnerSettings verifies headless runs resolve the job owner's
// stored LLM settings instead of the server default provider.
func TestRunOnceUsesOwnerSettings(t *testing.T) {
	var gotAuth, gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)

	_, err := s.UpsertUserSettings(t.Context(), db.UpsertUserSettingsParams{
		UserID:      1,
		LlmProvider: "openai",
		LlmBaseUrl:  upstream.URL,
		LlmApiKey:   "sk-owner",
		LlmModel:    "owner-model",
	})
	require.NoError(t, err)

	engine := &chat.Engine{
		Store:       s,
		LLM:         &chat.LLMResolver{Store: s, Default: llm.NewMock()},
		Tools:       tool.NewRegistry(),
		Permissions: permission.NewRuleset(permission.Allow, nil),
		Headless:    true,
	}
	sched := scheduler.New(s, engine, time.Minute)
	createDueJob(t, s, 1, "0 8 * * *", "brief me", 1)

	require.NoError(t, sched.RunOnce(t.Context()))

	assert.Equal(t, "Bearer sk-owner", gotAuth)
	assert.Equal(t, "owner-model", gotModel)
}

func TestRunOnceSkipsDisabledAndFutureJobs(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)

	provider := &scriptedProvider{rounds: [][]llm.Chunk{}}
	engine := &chat.Engine{
		Store:       s,
		LLM:         &chat.LLMResolver{Store: s, Default: provider},
		Tools:       tool.NewRegistry(),
		Permissions: permission.NewRuleset(permission.Allow, nil),
		Headless:    true,
	}
	sched := scheduler.New(s, engine, time.Minute)

	disabled := createDueJob(t, s, 1, "0 8 * * *", "disabled job", 0)

	future, err := s.CreateJob(t.Context(), db.CreateJobParams{
		UserID:    1,
		Schedule:  "0 8 * * *",
		Prompt:    "future job",
		Enabled:   1,
		NextRunAt: sql.NullTime{Time: time.Now().Add(24 * time.Hour), Valid: true},
	})
	require.NoError(t, err)

	require.NoError(t, sched.RunOnce(t.Context()))

	for _, id := range []int64{disabled.ID, future.ID} {
		got, err := s.GetJob(t.Context(), id)
		require.NoError(t, err)
		assert.Empty(t, got.LastStatus)
		assert.False(t, got.LastRunAt.Valid)
		assert.False(t, got.SessionID.Valid)
	}
	assert.Empty(t, provider.gotRequests)
}

func TestRunOnceHeadlessAskDeniesTool(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)

	provider := &scriptedProvider{rounds: [][]llm.Chunk{
		{{FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "clock", Arguments: "{}"}}}},
		{{Delta: "no time for you"}, {FinishReason: "stop"}},
	}}
	engine := &chat.Engine{
		Store:       s,
		LLM:         &chat.LLMResolver{Store: s, Default: provider},
		Tools:       clockRegistry(t),
		Permissions: permission.NewRuleset(permission.Ask, nil),
		Headless:    true,
	}
	sched := scheduler.New(s, engine, time.Minute)

	job := createDueJob(t, s, 1, "0 8 * * *", "what time is it", 1)

	require.NoError(t, sched.RunOnce(t.Context()))

	got, err := s.GetJob(t.Context(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, "ok", got.LastStatus)
	require.True(t, got.SessionID.Valid)

	// The ask verdict was downgraded to deny: no approval row is left pending and
	// the tool call was recorded as denied.
	pending, err := s.CountPendingApprovalsBySession(t.Context(), got.SessionID.Int64)
	require.NoError(t, err)
	assert.Equal(t, int64(0), pending)

	calls, err := s.ListToolCallsBySession(t.Context(), got.SessionID.Int64)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, "denied", calls[0].Status)
	assert.Contains(t, calls[0].Result, "denied by policy")
}

func TestRunOnceRecordsProviderError(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)

	engine := &chat.Engine{
		Store:       s,
		LLM:         &chat.LLMResolver{Store: s, Default: &errorProvider{}},
		Tools:       tool.NewRegistry(),
		Permissions: permission.NewRuleset(permission.Allow, nil),
		Headless:    true,
	}
	sched := scheduler.New(s, engine, time.Minute)

	job := createDueJob(t, s, 1, "0 8 * * *", "brief me", 1)

	require.NoError(t, sched.RunOnce(t.Context()))

	got, err := s.GetJob(t.Context(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, "error", got.LastStatus)
}

type errorProvider struct{}

func (p *errorProvider) Chat(_ context.Context, _ llm.Request) (llm.Stream, error) {
	return nil, fmt.Errorf("provider down")
}

func TestStartStop(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)

	engine := &chat.Engine{
		Store:       s,
		LLM:         &chat.LLMResolver{Store: s, Default: llm.NewMock()},
		Tools:       tool.NewRegistry(),
		Permissions: permission.NewRuleset(permission.Allow, nil),
		Headless:    true,
	}
	sched := scheduler.New(s, engine, 10*time.Millisecond)

	sched.Start()
	time.Sleep(50 * time.Millisecond)
	sched.Stop()
}
