package server_test

import (
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

func TestChatRequiresAuth(t *testing.T) {
	app, _, _ := newTestApp(t, llm.NewMock())

	res := postChat(t, app, "", map[string]any{"message": "hi"})
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	assert.Equal(t, "unauthorized", decodeErrorMessage(t, res))
}

func TestChatRejectsInvalidSession(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	signUp(t, app, dbConn, "owner@example.com")

	res := postChat(t, app, "limen_session=bogus-token", map[string]any{"message": "hi"})
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	assert.Equal(t, "unauthorized", decodeErrorMessage(t, res))
}

func TestChatRejectsOtherUsersSession(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")
	seedUser(t, dbConn, 999)

	foreign, err := s.CreateSession(t.Context(), db.CreateSessionParams{UserID: 999})
	require.NoError(t, err)

	res := postChat(t, app, cookie, map[string]any{"session_id": foreign.ID, "message": "hi"})
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.Equal(t, "session not found", decodeErrorMessage(t, res))
}

func TestSignupLocksAfterFirstUser(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	signUp(t, app, dbConn, "first@example.com")

	req := httptest.NewRequest(http.MethodPost, "/auth/signup/credential",
		strings.NewReader(`{"email":"second@example.com","password":"Secure1pass"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()

	assert.Equal(t, http.StatusForbidden, res.StatusCode)
	assert.Equal(t, "sign-up is disabled", decodeErrorMessage(t, res))
}

func TestFirstUserIsAdmin(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	signUp(t, app, dbConn, "owner@example.com")

	var role string
	require.NoError(t, dbConn.QueryRow("SELECT role FROM auth_users WHERE email = ?", "owner@example.com").Scan(&role))
	assert.Equal(t, "admin", role)
}

func TestSigninAfterSignup(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	signUp(t, app, dbConn, "owner@example.com")

	req := httptest.NewRequest(http.MethodPost, "/auth/signin/credential",
		strings.NewReader(`{"credential":"owner@example.com","password":"Secure1pass"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var cookie string
	for _, c := range res.Cookies() {
		if c.Name == "limen_session" {
			cookie = c.Name + "=" + c.Value
		}
	}
	require.NotEmpty(t, cookie)

	chat := postChat(t, app, cookie, map[string]any{"message": "hi"})
	assert.Equal(t, http.StatusOK, chat.StatusCode)
}
