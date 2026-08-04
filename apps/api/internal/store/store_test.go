package store_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

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
	assert.Equal(t, "assistant", messages[1].Role)
	assert.Equal(t, "hello", messages[1].Content)

	count, err := s.CountMessages(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	page, err := s.ListMessages(ctx, db.ListMessagesParams{SessionID: session.ID, Limit: 1, Offset: 1})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, second.ID, page[0].ID)

	require.NoError(t, s.DeleteMessagesBySession(ctx, session.ID))
	count, err = s.CountMessages(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
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

func TestMigrateIsIdempotent(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	dbConn, err := store.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	applied, err := store.Migrate(dbConn)
	require.NoError(t, err)
	assert.Equal(t, 3, applied)

	applied, err = store.Migrate(dbConn)
	require.NoError(t, err)
	assert.Equal(t, 0, applied)
}
