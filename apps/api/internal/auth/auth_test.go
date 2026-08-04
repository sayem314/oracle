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

func TestCreateUserAssignsUserRole(t *testing.T) {
	a, _ := newAuth(t)

	user, err := a.CreateUser(t.Context(), "member@example.com", "Secure1pass")
	require.NoError(t, err)
	assert.Positive(t, user.ID)
	assert.Equal(t, "member@example.com", user.Email)
	// Admin-created users are always the plain user role.
	assert.Equal(t, auth.RoleUser, user.Role)
	assert.False(t, user.CreatedAt.IsZero())
}

func TestCreateUserDuplicateEmail(t *testing.T) {
	a, _ := newAuth(t)

	_, err := a.CreateUser(t.Context(), "dup@example.com", "Secure1pass")
	require.NoError(t, err)

	_, err = a.CreateUser(t.Context(), "dup@example.com", "Secure1pass")
	require.Error(t, err)
	var aerr *auth.Error
	require.ErrorAs(t, err, &aerr)
	assert.Equal(t, 409, aerr.Status)
}

func TestCreateUserWeakPassword(t *testing.T) {
	a, _ := newAuth(t)

	_, err := a.CreateUser(t.Context(), "weak@example.com", "short")
	require.Error(t, err)
	var aerr *auth.Error
	require.ErrorAs(t, err, &aerr)
	assert.Equal(t, 400, aerr.Status)
}

func TestListAndDeleteUsers(t *testing.T) {
	a, _ := newAuth(t)

	u1, err := a.CreateUser(t.Context(), "one@example.com", "Secure1pass")
	require.NoError(t, err)
	u2, err := a.CreateUser(t.Context(), "two@example.com", "Secure1pass")
	require.NoError(t, err)

	users, err := a.ListUsers(t.Context())
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, u1.ID, users[0].ID)
	assert.Equal(t, u2.ID, users[1].ID)

	require.NoError(t, a.DeleteUser(t.Context(), u1.ID))

	users, err = a.ListUsers(t.Context())
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, u2.ID, users[0].ID)

	// Deleting a missing user reports no rows.
	err = a.DeleteUser(t.Context(), u1.ID)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestRole(t *testing.T) {
	a, dbConn := newAuth(t)

	user, err := a.CreateUser(t.Context(), "role@example.com", "Secure1pass")
	require.NoError(t, err)

	role, err := a.Role(t.Context(), user.ID)
	require.NoError(t, err)
	assert.Equal(t, auth.RoleUser, role)

	// Promote directly and confirm Role reflects the stored value.
	_, err = dbConn.Exec("UPDATE auth_users SET role = ? WHERE id = ?", auth.RoleAdmin, user.ID)
	require.NoError(t, err)

	role, err = a.Role(t.Context(), user.ID)
	require.NoError(t, err)
	assert.Equal(t, auth.RoleAdmin, role)

	_, err = a.Role(t.Context(), user.ID+999)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}
