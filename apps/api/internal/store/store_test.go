package store_test

import (
	"database/sql"
	"fmt"
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

// seedUser inserts a minimal auth_users row so chat rows can satisfy the
// sessions.user_id foreign key.
func seedUser(t *testing.T, dbConn *sql.DB, id int64) {
	t.Helper()

	_, err := dbConn.Exec(
		"INSERT INTO auth_users (id, email) VALUES (?, ?)",
		id, fmt.Sprintf("user%d@example.com", id),
	)
	require.NoError(t, err)
}

func TestSessionLifecycle(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)
	seedUser(t, dbConn, 2)

	created, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: 1, Title: "hello"})
	require.NoError(t, err)
	assert.Positive(t, created.ID)
	assert.Equal(t, int64(1), created.UserID)
	assert.Equal(t, "hello", created.Title)
	assert.False(t, created.CreatedAt.IsZero())

	got, err := s.GetSession(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, got)

	second, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: 1})
	require.NoError(t, err)
	_, err = s.CreateSession(ctx, db.CreateSessionParams{UserID: 2})
	require.NoError(t, err)

	sessions, err := s.ListSessions(ctx, db.ListSessionsParams{UserID: 1, Limit: 10})
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
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	session, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: 1, Title: "chat"})
	require.NoError(t, err)

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

	count, err := s.CountMessages(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	require.NoError(t, s.DeleteMessagesBySession(ctx, session.ID))
	count, err = s.CountMessages(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestListMessagesReturnsNewest(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	session, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: 1})
	require.NoError(t, err)

	var ids []int64
	for i := range 5 {
		m, err := s.AppendMessage(ctx, db.AppendMessageParams{
			SessionID: session.ID,
			Role:      "user",
			Content:   fmt.Sprintf("m%d", i),
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

func TestDeleteSessionCascadesMessages(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	session, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: 1})
	require.NoError(t, err)
	_, err = s.AppendMessage(ctx, db.AppendMessageParams{SessionID: session.ID, Role: "user", Content: "hi"})
	require.NoError(t, err)

	require.NoError(t, s.DeleteSession(ctx, session.ID))

	count, err := s.CountMessages(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestCreateSessionRequiresExistingUser(t *testing.T) {
	s, _ := openStore(t)

	_, err := s.CreateSession(t.Context(), db.CreateSessionParams{UserID: 42})
	require.Error(t, err)
}

func TestDeleteUserCascadesSessions(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	session, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: 1})
	require.NoError(t, err)
	_, err = s.AppendMessage(ctx, db.AppendMessageParams{SessionID: session.ID, Role: "user", Content: "hi"})
	require.NoError(t, err)

	_, err = dbConn.Exec("DELETE FROM auth_users WHERE id = ?", 1)
	require.NoError(t, err)

	_, err = s.GetSession(ctx, session.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)

	count, err := s.CountMessages(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestToolCallLifecycle(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	session, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: 1})
	require.NoError(t, err)
	asst := db.AppendMessageParams{SessionID: session.ID, Role: "assistant", Content: ""}
	msg, err := s.AppendMessage(ctx, asst)
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

func TestToolCallsCascadeWithMessage(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	session, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: 1})
	require.NoError(t, err)
	msg, err := s.AppendMessage(ctx, db.AppendMessageParams{SessionID: session.ID, Role: "assistant"})
	require.NoError(t, err)
	_, err = s.InsertToolCall(ctx, db.InsertToolCallParams{
		MessageID: msg.ID, CallID: "call_1", Name: "clock", Status: "pending",
	})
	require.NoError(t, err)

	require.NoError(t, s.DeleteMessagesBySession(ctx, session.ID))

	calls, err := s.ListToolCallsBySession(ctx, session.ID)
	require.NoError(t, err)
	assert.Empty(t, calls)
}

func TestResolveToolCallCountsRemaining(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	session, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: 1})
	require.NoError(t, err)
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

func TestGetToolCallIncludesSessionAndUser(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	session, err := s.CreateSession(ctx, db.CreateSessionParams{UserID: 1})
	require.NoError(t, err)
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
	assert.Equal(t, int64(1), got.UserID)

	_, err = s.GetToolCall(ctx, created.ID+999)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestMigrateIsIdempotent(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	dbConn, err := store.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	applied, err := store.Migrate(dbConn)
	require.NoError(t, err)
	assert.Equal(t, 8, applied)

	applied, err = store.Migrate(dbConn)
	require.NoError(t, err)
	assert.Equal(t, 0, applied)
}

func TestJobLifecycle(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	next := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	job, err := s.CreateJob(ctx, db.CreateJobParams{
		UserID:    1,
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

	jobs, err := s.ListJobsByUser(ctx, 1)
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
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	created, err := s.CreateJob(ctx, db.CreateJobParams{
		UserID:    1,
		Schedule:  "0 8 * * *",
		Prompt:    "brief me",
		Enabled:   1,
		NextRunAt: sql.NullTime{Time: time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC), Valid: true},
	})
	require.NoError(t, err)

	// Read the row back and claim using exactly the value we read. This proves
	// the optimistic claim survives the DATETIME write/read round trip.
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

	// The next_run_at moved, so a second claim with the stale expectation fails.
	claimed, err = s.ClaimJob(ctx, db.ClaimJobParams{
		NewNextRunAt:      sql.NullTime{Time: next.Add(time.Hour), Valid: true},
		LastRunAt:         sql.NullTime{Time: time.Date(2026, 8, 5, 8, 0, 2, 0, time.UTC), Valid: true},
		ID:                due[0].ID,
		ExpectedNextRunAt: due[0].NextRunAt,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), claimed)

	got, err := s.GetJob(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "running", got.LastStatus)
	assert.True(t, got.NextRunAt.Valid)
	assert.Equal(t, next.UTC(), got.NextRunAt.Time.UTC())
	assert.True(t, got.LastRunAt.Valid)
}

func TestListDueJobs(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	dueTime := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	_, err := s.CreateJob(ctx, db.CreateJobParams{
		UserID: 1, Schedule: "0 8 * * *", Prompt: "due", Enabled: 1,
		NextRunAt: sql.NullTime{Time: dueTime, Valid: true},
	})
	require.NoError(t, err)
	_, err = s.CreateJob(ctx, db.CreateJobParams{
		UserID: 1, Schedule: "0 9 * * *", Prompt: "future", Enabled: 1,
		NextRunAt: sql.NullTime{Time: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC), Valid: true},
	})
	require.NoError(t, err)
	_, err = s.CreateJob(ctx, db.CreateJobParams{
		UserID: 1, Schedule: "0 7 * * *", Prompt: "disabled", Enabled: 0,
		NextRunAt: sql.NullTime{Time: dueTime, Valid: true},
	})
	require.NoError(t, err)

	got, err := s.ListDueJobs(ctx, sql.NullTime{Time: time.Date(2026, 8, 5, 8, 30, 0, 0, time.UTC), Valid: true})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "due", got[0].Prompt)
}

func TestLLMProviders(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	_, err := s.GetDefaultLLMProvider(ctx)
	require.ErrorIs(t, err, sql.ErrNoRows)

	created, err := s.CreateLLMProvider(ctx, db.CreateLLMProviderParams{
		Name:      "OpenRouter",
		Provider:  "openai",
		BaseUrl:   "https://openrouter.ai/api/v1",
		ApiKey:    "sk-test",
		IsDefault: 1,
	})
	require.NoError(t, err)
	assert.Positive(t, created.ID)

	require.NoError(t, s.InsertLLMModel(ctx, db.InsertLLMModelParams{ProviderID: created.ID, Name: "model-a"}))
	require.NoError(t, s.InsertLLMModel(ctx, db.InsertLLMModelParams{ProviderID: created.ID, Name: "model-b", IsDefault: 1}))

	models, err := s.ListLLMModelsByProvider(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "model-a", models[0].Name)
	assert.Equal(t, int64(1), models[1].IsDefault)

	def, err := s.GetDefaultLLMProvider(ctx)
	require.NoError(t, err)
	assert.Equal(t, created.ID, def.ID)

	second, err := s.CreateLLMProvider(ctx, db.CreateLLMProviderParams{
		Name: "Ollama", Provider: "openai", BaseUrl: "http://localhost:11434/v1", ApiKey: "ollama",
	})
	require.NoError(t, err)

	require.NoError(t, s.ClearDefaultLLMProviders(ctx))
	updated, err := s.UpdateLLMProvider(ctx, db.UpdateLLMProviderParams{
		Name: "Ollama", Provider: "openai", BaseUrl: "http://localhost:11434/v1", ApiKey: "ollama",
		IsDefault: 1, ID: second.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated.IsDefault)

	def, err = s.GetDefaultLLMProvider(ctx)
	require.NoError(t, err)
	assert.Equal(t, second.ID, def.ID)

	list, err := s.ListLLMProviders(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)

	require.NoError(t, s.DeleteLLMModelsByProvider(ctx, created.ID))
	models, err = s.ListLLMModelsByProvider(ctx, created.ID)
	require.NoError(t, err)
	assert.Empty(t, models)

	require.NoError(t, s.DeleteLLMProvider(ctx, created.ID))
	_, err = s.GetLLMProvider(ctx, created.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestLLMProviderUniqueName(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	params := db.CreateLLMProviderParams{
		Name: "main", Provider: "openai", BaseUrl: "https://api.example.com/v1", ApiKey: "sk-test",
	}
	_, err := s.CreateLLMProvider(ctx, params)
	require.NoError(t, err)

	// Names are globally unique across the instance.
	_, err = s.CreateLLMProvider(ctx, params)
	require.Error(t, err)
}

func TestUserLLMPrefs(t *testing.T) {
	s, dbConn := openStore(t)
	ctx := t.Context()
	seedUser(t, dbConn, 1)

	_, err := s.GetUserLLMPrefs(ctx, 1)
	require.ErrorIs(t, err, sql.ErrNoRows)

	created, err := s.CreateLLMProvider(ctx, db.CreateLLMProviderParams{
		Name: "main", Provider: "openai", BaseUrl: "https://api.example.com/v1", ApiKey: "sk-test",
	})
	require.NoError(t, err)

	pref, err := s.UpsertUserLLMPrefs(ctx, db.UpsertUserLLMPrefsParams{
		UserID:     1,
		ProviderID: sql.NullInt64{Int64: created.ID, Valid: true},
		Model:      "model-a",
	})
	require.NoError(t, err)
	assert.Equal(t, created.ID, pref.ProviderID.Int64)
	assert.Equal(t, "model-a", pref.Model)

	pref, err = s.UpsertUserLLMPrefs(ctx, db.UpsertUserLLMPrefsParams{
		UserID:     1,
		ProviderID: sql.NullInt64{Int64: created.ID, Valid: true},
		Model:      "model-b",
	})
	require.NoError(t, err)
	assert.Equal(t, "model-b", pref.Model)

	got, err := s.GetUserLLMPrefs(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "model-b", got.Model)

	// Deleting the provider leaves the pref behind with a NULL provider_id
	// (ON DELETE SET NULL) so it falls back to the global default.
	require.NoError(t, s.DeleteLLMProvider(ctx, created.ID))
	got, err = s.GetUserLLMPrefs(ctx, 1)
	require.NoError(t, err)
	assert.False(t, got.ProviderID.Valid)

	require.NoError(t, s.DeleteUserLLMPrefs(ctx, 1))
	_, err = s.GetUserLLMPrefs(ctx, 1)
	require.ErrorIs(t, err, sql.ErrNoRows)
}
