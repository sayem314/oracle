package store_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

func openStore(t *testing.T) (store.Store, *sql.DB) {
	t.Helper()

	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") + "?_pragma=foreign_keys(ON)"
	dbConn, err := store.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	applied, err := store.Migrate(dbConn)
	require.NoError(t, err)
	require.Positive(t, applied)

	return store.New(dbConn), dbConn
}

func newSession(t *testing.T, s store.Store, title string) db.Session {
	t.Helper()
	session, err := s.CreateSession(t.Context(), title)
	require.NoError(t, err)
	return session
}

func TestSessionLifecycle(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	created, err := s.CreateSession(ctx, "hello")
	require.NoError(t, err)
	assert.Positive(t, created.ID)
	assert.Equal(t, "hello", created.Title)
	assert.False(t, created.CreatedAt.IsZero())

	got, err := s.GetSession(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, got)

	second, err := s.CreateSession(ctx, "")
	require.NoError(t, err)

	sessions, err := s.ListSessions(ctx, db.ListSessionsParams{Limit: 10})
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	assert.Equal(t, second.ID, sessions[0].ID)

	require.NoError(t, s.UpdateSessionTitle(ctx, db.UpdateSessionTitleParams{ID: created.ID, Title: "renamed"}))
	got, err = s.GetSession(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.Title)

	require.NoError(t, s.TouchSession(ctx, created.ID))

	require.NoError(t, s.DeleteSession(ctx, created.ID))
	_, err = s.GetSession(ctx, created.ID)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestMessageLifecycle(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	session := newSession(t, s, "chat")

	first, err := s.AppendMessage(ctx, db.AppendMessageParams{SessionID: session.ID, Role: "user", Content: "hi"})
	require.NoError(t, err)
	second, err := s.AppendMessage(ctx, db.AppendMessageParams{SessionID: session.ID, Role: "assistant", Content: "hello"})
	require.NoError(t, err)

	messages, err := s.ListMessages(ctx, db.ListMessagesParams{SessionID: session.ID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, messages, 2)
	assert.Equal(t, first.ID, messages[0].ID)
	assert.Equal(t, second.ID, messages[1].ID)
	assert.Equal(t, "assistant", messages[1].Role)
	assert.Equal(t, "hello", messages[1].Content)

	require.NoError(t, s.DeleteSession(ctx, session.ID))
	remain, err := s.ListMessages(ctx, db.ListMessagesParams{SessionID: session.ID, Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, remain)
}

func TestListMessagesReturnsNewest(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	session := newSession(t, s, "")

	var ids []int64
	for range 5 {
		m, err := s.AppendMessage(ctx, db.AppendMessageParams{
			SessionID: session.ID,
			Role:      "user",
			Content:   "m",
		})
		require.NoError(t, err)
		ids = append(ids, m.ID)
	}

	page, err := s.ListMessages(ctx, db.ListMessagesParams{SessionID: session.ID, Limit: 2})
	require.NoError(t, err)
	require.Len(t, page, 2)
	assert.Equal(t, ids[3], page[0].ID)
	assert.Equal(t, ids[4], page[1].ID)

	older, err := s.ListMessages(ctx, db.ListMessagesParams{SessionID: session.ID, Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Len(t, older, 2)
	assert.Equal(t, ids[1], older[0].ID)
	assert.Equal(t, ids[2], older[1].ID)
}

func TestToolCallLifecycle(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	session := newSession(t, s, "")
	msg, err := s.AppendMessage(ctx, db.AppendMessageParams{SessionID: session.ID, Role: "assistant"})
	require.NoError(t, err)

	created, err := s.InsertToolCall(ctx, db.InsertToolCallParams{
		MessageID: msg.ID,
		CallID:    "call_1",
		Name:      "clock",
		Arguments: `{}`,
		Status:    "pending",
	})
	require.NoError(t, err)
	assert.Positive(t, created.ID)
	assert.Equal(t, "pending", created.Status)
	assert.Empty(t, created.Result)

	require.NoError(t, s.UpdateToolCallResult(ctx, db.UpdateToolCallResultParams{
		ID:     created.ID,
		Result: "12:00",
		Status: "done",
	}))

	calls, err := s.ListToolCallsBySession(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, "12:00", calls[0].Result)
	assert.Equal(t, "done", calls[0].Status)
}

func TestGetToolCallIncludesSession(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	session := newSession(t, s, "")
	msg, err := s.AppendMessage(ctx, db.AppendMessageParams{SessionID: session.ID, Role: "assistant"})
	require.NoError(t, err)
	created, err := s.InsertToolCall(ctx, db.InsertToolCallParams{
		MessageID: msg.ID, CallID: "call_1", Name: "clock", Status: "awaiting_approval",
	})
	require.NoError(t, err)

	got, err := s.GetToolCall(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ToolCall.ID)
	assert.Equal(t, session.ID, got.SessionID)

	_, err = s.GetToolCall(ctx, created.ID+999)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestResolveToolCallCountsRemaining(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	session := newSession(t, s, "")
	msg, err := s.AppendMessage(ctx, db.AppendMessageParams{SessionID: session.ID, Role: "assistant"})
	require.NoError(t, err)

	var ids []int64
	for _, callID := range []string{"call_1", "call_2"} {
		row, err := s.InsertToolCall(ctx, db.InsertToolCallParams{
			MessageID: msg.ID, CallID: callID, Name: "clock", Status: "awaiting_approval",
		})
		require.NoError(t, err)
		ids = append(ids, row.ID)
	}

	remaining, err := s.ResolveToolCall(ctx, db.UpdateToolCallResultParams{
		ID: ids[0], Result: "12:00", Status: "done",
	}, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), remaining)

	remaining, err = s.ResolveToolCall(ctx, db.UpdateToolCallResultParams{
		ID: ids[1], Result: "denied by user", Status: "denied",
	}, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), remaining)

	count, err := s.CountPendingApprovalsBySession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestMigrateIsIdempotent(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	dbConn, err := store.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	applied, err := store.Migrate(dbConn)
	require.NoError(t, err)
	assert.Equal(t, 7, applied)

	applied, err = store.Migrate(dbConn)
	require.NoError(t, err)
	assert.Equal(t, 0, applied)
}

func TestJobLifecycle(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	next := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	job, err := s.CreateJob(ctx, db.CreateJobParams{
		Schedule:  "0 8 * * *",
		Prompt:    "morning briefing",
		Enabled:   1,
		NextRunAt: sql.NullTime{Time: next, Valid: true},
	})
	require.NoError(t, err)
	assert.Positive(t, job.ID)
	assert.Equal(t, "0 8 * * *", job.Schedule)
	assert.True(t, job.NextRunAt.Valid)

	got, err := s.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, got.ID)

	jobs, err := s.ListJobs(ctx)
	require.NoError(t, err)
	assert.Len(t, jobs, 1)

	updated, err := s.UpdateJob(ctx, db.UpdateJobParams{
		Schedule: "30 7 * * *",
		Prompt:   "earlier briefing",
		Enabled:  0,
		ID:       job.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "30 7 * * *", updated.Schedule)
	assert.Equal(t, int64(0), updated.Enabled)
	assert.False(t, updated.NextRunAt.Valid)

	require.NoError(t, s.DeleteJob(ctx, job.ID))
	_, err = s.GetJob(ctx, job.ID)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestJobClaimRoundTrip(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	_, err := s.CreateJob(ctx, db.CreateJobParams{
		Schedule:  "0 8 * * *",
		Prompt:    "brief me",
		Enabled:   1,
		NextRunAt: sql.NullTime{Time: time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC), Valid: true},
	})
	require.NoError(t, err)

	due, err := s.ListDueJobs(ctx, sql.NullTime{Time: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC), Valid: true})
	require.NoError(t, err)
	require.Len(t, due, 1)

	next := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	claimed, err := s.ClaimJob(ctx, db.ClaimJobParams{
		NewNextRunAt:      sql.NullTime{Time: next, Valid: true},
		LastRunAt:         sql.NullTime{Time: time.Date(2026, 8, 5, 8, 0, 1, 0, time.UTC), Valid: true},
		ID:                due[0].ID,
		ExpectedNextRunAt: due[0].NextRunAt,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), claimed)

	claimed, err = s.ClaimJob(ctx, db.ClaimJobParams{
		NewNextRunAt:      sql.NullTime{Time: next.Add(time.Hour), Valid: true},
		LastRunAt:         sql.NullTime{Time: time.Date(2026, 8, 5, 8, 0, 2, 0, time.UTC), Valid: true},
		ID:                due[0].ID,
		ExpectedNextRunAt: due[0].NextRunAt,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), claimed)
}

func TestListDueJobs(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	dueTime := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	_, err := s.CreateJob(ctx, db.CreateJobParams{
		Schedule: "0 8 * * *", Prompt: "due", Enabled: 1,
		NextRunAt: sql.NullTime{Time: dueTime, Valid: true},
	})
	require.NoError(t, err)
	_, err = s.CreateJob(ctx, db.CreateJobParams{
		Schedule: "0 9 * * *", Prompt: "future", Enabled: 1,
		NextRunAt: sql.NullTime{Time: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC), Valid: true},
	})
	require.NoError(t, err)
	_, err = s.CreateJob(ctx, db.CreateJobParams{
		Schedule: "0 7 * * *", Prompt: "disabled", Enabled: 0,
		NextRunAt: sql.NullTime{Time: dueTime, Valid: true},
	})
	require.NoError(t, err)

	got, err := s.ListDueJobs(ctx, sql.NullTime{Time: time.Date(2026, 8, 5, 8, 30, 0, 0, time.UTC), Valid: true})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "due", got[0].Prompt)
}

func TestLLMProviderSingleton(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	// Fresh instance has no provider row until seeded.
	_, err := s.GetLLMProvider(ctx)
	require.ErrorIs(t, err, sql.ErrNoRows)

	updated, err := s.UpsertLLMProvider(ctx, db.UpsertLLMProviderParams{
		Provider: "openai",
		BaseUrl:  "https://openrouter.ai/api/v1",
		ApiKey:   "sk-test",
		Model:    "model-a",
	})
	require.NoError(t, err)
	assert.Equal(t, "openai", updated.Provider)
	assert.Equal(t, "model-a", updated.Model)

	got, err := s.GetLLMProvider(ctx)
	require.NoError(t, err)
	assert.Equal(t, "sk-test", got.ApiKey)
	assert.Equal(t, "model-a", got.Model)

	// Upsert updates in place; the singleton id stays 1.
	updated, err = s.UpsertLLMProvider(ctx, db.UpsertLLMProviderParams{
		Provider: "openai",
		BaseUrl:  "https://openrouter.ai/api/v1",
		ApiKey:   "sk-test",
		Model:    "model-b",
	})
	require.NoError(t, err)
	assert.Equal(t, "model-b", updated.Model)
	assert.Equal(t, int64(1), updated.ID)
}

func TestLLMModelSetter(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	_, err := s.UpsertLLMProvider(ctx, db.UpsertLLMProviderParams{
		Provider: "openai",
		BaseUrl:  "https://api.example.com/v1",
		ApiKey:   "sk-test",
		Model:    "model-a",
	})
	require.NoError(t, err)

	updated, err := s.SetLLMModel(ctx, "model-c")
	require.NoError(t, err)
	assert.Equal(t, "model-c", updated.Model)
}

func TestSettingsRoundTrip(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	_, err := s.GetSettings(ctx)
	require.ErrorIs(t, err, sql.ErrNoRows)

	created, err := s.UpsertSettings(ctx, db.UpsertSettingsParams{
		PermissionDefault: "allow",
		PermissionRules:   "get_time:ask, web_*:deny",
	})
	require.NoError(t, err)
	assert.Equal(t, "allow", created.PermissionDefault)

	got, err := s.GetSettings(ctx)
	require.NoError(t, err)
	assert.Equal(t, "allow", got.PermissionDefault)
	assert.Equal(t, "get_time:ask, web_*:deny", got.PermissionRules)
}
