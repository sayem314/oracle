package server_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/auth"
	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/permission"
	"github.com/sayem314/oracle/apps/api/internal/server"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/tool"
	"github.com/sayem314/oracle/apps/api/internal/tool/all"
)

const testAuthSecret = "0123456789abcdef0123456789abcdef"

var allowAll = permission.NewRuleset(permission.Allow, nil)

func newTestApp(t *testing.T, provider llm.Provider) (*fiber.App, store.Store, *sql.DB) {
	t.Helper()
	return newTestAppWithTools(t, provider, builtinTools(t))
}

func builtinTools(t *testing.T) *tool.Registry {
	t.Helper()
	r := tool.NewRegistry()
	require.NoError(t, r.RegisterGroups(all.Groups()...))
	return r
}

func newTestAppWithTools(t *testing.T, provider llm.Provider, tools tool.Executor) (*fiber.App, store.Store, *sql.DB) {
	t.Helper()
	return newTestAppFull(t, provider, tools, allowAll)
}

func newTestAppFull(t *testing.T, provider llm.Provider, tools tool.Executor, perms *permission.Ruleset) (*fiber.App, store.Store, *sql.DB) {
	t.Helper()

	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") + "?_pragma=foreign_keys(ON)"
	dbConn, err := store.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	_, err = store.Migrate(dbConn)
	require.NoError(t, err)

	a, err := auth.New(dbConn, auth.Options{Secret: []byte(testAuthSecret)})
	require.NoError(t, err)

	s := store.New(dbConn)
	resolver := &chat.LLMResolver{Store: s, Default: provider}
	engine := &chat.Engine{
		Store:       s,
		LLM:         resolver,
		Tools:       tools,
		Permissions: perms,
	}
	app := server.New(server.Deps{Store: s, Auth: a, Chat: engine})
	return app, s, dbConn
}

// signUp registers a user through the public /auth endpoint and returns the
// session cookie plus the new user's ID.
func signUp(t *testing.T, app *fiber.App, dbConn *sql.DB, email string) (string, int64) {
	t.Helper()

	body, err := json.Marshal(map[string]any{"email": email, "password": "Secure1pass"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/auth/signup/credential", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)

	var cookie string
	for _, c := range res.Cookies() {
		if c.Name == "limen_session" {
			cookie = c.Name + "=" + c.Value
		}
	}
	require.NotEmpty(t, cookie, "sign-up did not set a session cookie")

	var userID int64
	require.NoError(t, dbConn.QueryRow("SELECT id FROM auth_users WHERE email = ?", email).Scan(&userID))
	return cookie, userID
}
