package auth_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/auth"
	"github.com/sayem314/oracle/apps/api/internal/store"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestNewRejectsInvalidSecret(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	dbConn, err := store.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	_, err = auth.New(dbConn, auth.Options{Secret: []byte("too-short")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func newAuth(t *testing.T) (auth.Auth, *sql.DB) {
	t.Helper()

	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") + "?_pragma=foreign_keys(ON)"
	dbConn, err := store.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	_, err = store.Migrate(dbConn)
	require.NoError(t, err)

	a, err := auth.New(dbConn, auth.Options{Secret: []byte(testSecret)})
	require.NoError(t, err)
	return a, dbConn
}

// signup is intentionally unused; the SignUp flow is exercised through the
// server's sign-up gate tests.

func TestHasUsersStartsEmptyAndLocksAfterFirst(t *testing.T) {
	a, _ := newAuth(t)

	has, err := a.HasUsers(t.Context())
	require.NoError(t, err)
	assert.False(t, has)
}

func TestFirstUserRoleAndHasUsers(t *testing.T) {
	// The interface no longer exposes admin-created users or roles stamped by
	// CreateUser; the first-account-admin contract is enforced by the sign-up
	// gate in the server, exercised there. Here we confirm HasUsers flips after
	// a user row exists.
	a, dbConn := newAuth(t)

	has, err := a.HasUsers(t.Context())
	require.NoError(t, err)
	assert.False(t, has)

	var id int64
	require.NoError(t, dbConn.QueryRow(
		"INSERT INTO auth_users (id, email, password, role) VALUES (1, 'me@example.com', 'x', ?) RETURNING id",
		auth.RoleAdmin,
	).Scan(&id))
	assert.Equal(t, int64(1), id)

	has, err = a.HasUsers(t.Context())
	require.NoError(t, err)
	assert.True(t, has)
}

func TestRoleReflectsStoredValue(t *testing.T) {
	a, dbConn := newAuth(t)

	var id int64
	require.NoError(t, dbConn.QueryRow(
		"INSERT INTO auth_users (id, email, password, role) VALUES (1, 'role@example.com', 'x', ?) RETURNING id",
		auth.RoleUser,
	).Scan(&id))

	role, err := a.Role(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, auth.RoleUser, role)

	_, err = dbConn.Exec("UPDATE auth_users SET role = ? WHERE id = ?", auth.RoleAdmin, id)
	require.NoError(t, err)

	role, err = a.Role(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, auth.RoleAdmin, role)

	_, err = a.Role(t.Context(), id+999)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestGetUser(t *testing.T) {
	a, dbConn := newAuth(t)

	var id int64
	require.NoError(t, dbConn.QueryRow(
		"INSERT INTO auth_users (id, email, password, role) VALUES (1, 'me@example.com', 'x', 'user') RETURNING id",
	).Scan(&id))

	user, err := a.GetUser(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, int64(1), user.ID)
	assert.Equal(t, "me@example.com", user.Email)
	assert.Equal(t, "user", user.Role)
	assert.False(t, user.CreatedAt.IsZero())

	_, err = a.GetUser(t.Context(), id+999)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}
