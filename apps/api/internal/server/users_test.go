package server_test

import (
	"database/sql"
	"encoding/json"
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

type userResponse struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func doUsersRequest(t *testing.T, app *fiber.App, method, path, cookie string, body any) *http.Response {
	t.Helper()

	var reader *strings.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func decodeUserList(t *testing.T, res *http.Response) []userResponse {
	t.Helper()
	var users []userResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&users))
	return users
}

func decodeUser(t *testing.T, res *http.Response) userResponse {
	t.Helper()
	var u userResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&u))
	return u
}

func TestUsersAdminOnly(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	adminCookie, adminID := signUp(t, app, dbConn, "admin@example.com")

	// Admin creates a member through the API.
	res := doUsersRequest(t, app, http.MethodPost, "/api/v1/users", adminCookie, map[string]any{
		"email":    "member@example.com",
		"password": "Secure1pass",
	})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	member := decodeUser(t, res)
	assert.Equal(t, "user", member.Role)

	memberCookie := signIn(t, app, "member@example.com", "Secure1pass")

	t.Run("member cannot list users", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodGet, "/api/v1/users", memberCookie, nil)
		assert.Equal(t, http.StatusForbidden, res.StatusCode)
	})

	t.Run("member cannot create users", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodPost, "/api/v1/users", memberCookie, map[string]any{
			"email":    "other@example.com",
			"password": "Secure1pass",
		})
		assert.Equal(t, http.StatusForbidden, res.StatusCode)
	})

	t.Run("member cannot delete users", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodDelete, "/api/v1/users/"+itoa(adminID), memberCookie, nil)
		assert.Equal(t, http.StatusForbidden, res.StatusCode)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodGet, "/api/v1/users", "", nil)
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})
}

func TestListUsers(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	adminCookie, adminID := signUp(t, app, dbConn, "admin@example.com")

	res := doUsersRequest(t, app, http.MethodGet, "/api/v1/users", adminCookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	users := decodeUserList(t, res)
	require.Len(t, users, 1)
	assert.Equal(t, adminID, users[0].ID)
	assert.Equal(t, "admin", users[0].Role)
}

func TestCreateUserValidation(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	adminCookie, _ := signUp(t, app, dbConn, "admin@example.com")

	t.Run("missing email", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodPost, "/api/v1/users", adminCookie, map[string]any{"password": "Secure1pass"})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "email is required", decodeErrorMessage(t, res))
	})

	t.Run("missing password", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodPost, "/api/v1/users", adminCookie, map[string]any{"email": "x@example.com"})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "password is required", decodeErrorMessage(t, res))
	})

	t.Run("duplicate email", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodPost, "/api/v1/users", adminCookie, map[string]any{
			"email":    "admin@example.com",
			"password": "Secure1pass",
		})
		assert.Equal(t, http.StatusConflict, res.StatusCode)
	})

	t.Run("weak password surfaces limen policy", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodPost, "/api/v1/users", adminCookie, map[string]any{
			"email":    "weak@example.com",
			"password": "short",
		})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})
}

func TestDeleteUser(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	adminCookie, adminID := signUp(t, app, dbConn, "admin@example.com")

	res := doUsersRequest(t, app, http.MethodPost, "/api/v1/users", adminCookie, map[string]any{
		"email":    "member@example.com",
		"password": "Secure1pass",
	})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	member := decodeUser(t, res)

	// The member owns a session with a message, to prove the delete cascades.
	session, err := s.CreateSession(t.Context(), db.CreateSessionParams{UserID: member.ID})
	require.NoError(t, err)
	_, err = s.AppendMessage(t.Context(), db.AppendMessageParams{SessionID: session.ID, Role: "user", Content: "hi"})
	require.NoError(t, err)

	t.Run("cannot delete yourself", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodDelete, "/api/v1/users/"+itoa(adminID), adminCookie, nil)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "cannot delete yourself", decodeErrorMessage(t, res))
	})

	t.Run("unknown user", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodDelete, "/api/v1/users/99999", adminCookie, nil)
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
	})

	t.Run("delete member cascades", func(t *testing.T) {
		res := doUsersRequest(t, app, http.MethodDelete, "/api/v1/users/"+itoa(member.ID), adminCookie, nil)
		require.Equal(t, http.StatusNoContent, res.StatusCode)

		_, err := s.GetSession(t.Context(), session.ID)
		require.ErrorIs(t, err, sql.ErrNoRows)

		count, err := s.CountMessages(t.Context(), session.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(0), count)
	})
}
