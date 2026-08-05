package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

type settingsBody struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	HasAPIKey bool   `json:"has_api_key"`
}

func putSettings(t *testing.T, app *fiber.App, cookie string, body map[string]any) *http.Response {
	t.Helper()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func getSettings(t *testing.T, app *fiber.App, cookie string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func decodeSettings(t *testing.T, res *http.Response) settingsBody {
	t.Helper()
	var body settingsBody
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	return body
}

func TestSettingsEmptyByDefault(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := getSettings(t, app, cookie)
	require.Equal(t, http.StatusOK, res.StatusCode)

	got := decodeSettings(t, res)
	assert.Equal(t, settingsBody{}, got)
}

func TestSettingsUnauthenticated(t *testing.T) {
	app, _, _ := newTestApp(t, llm.NewMock())

	res := getSettings(t, app, "")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)

	res = putSettings(t, app, "", map[string]any{})
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
}

func TestSettingsUpdateAndFetch(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := putSettings(t, app, cookie, map[string]any{
		"provider": "openai",
		"base_url": "https://api.example.com/v1",
		"api_key":  "sk-secret",
		"model":    "example-1",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "sk-secret", "api key must never be returned")

	res = getSettings(t, app, cookie)
	require.Equal(t, http.StatusOK, res.StatusCode)

	got := decodeSettings(t, res)
	assert.Equal(t, "openai", got.Provider)
	assert.Equal(t, "https://api.example.com/v1", got.BaseURL)
	assert.Equal(t, "example-1", got.Model)
	assert.True(t, got.HasAPIKey)
}

func TestSettingsIsolatedPerUser(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, userID := signUp(t, app, dbConn, "owner@example.com")

	res := doUsersRequest(t, app, http.MethodPost, "/api/v1/users", cookie, map[string]any{
		"email":    "member@example.com",
		"password": "Secure1pass",
	})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	otherCookie := signIn(t, app, "member@example.com", "Secure1pass")

	res = putSettings(t, app, cookie, map[string]any{
		"provider": "openai",
		"base_url": "https://api.example.com/v1",
		"api_key":  "sk-secret",
		"model":    "example-1",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	res = getSettings(t, app, otherCookie)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, settingsBody{}, decodeSettings(t, res))

	setting, err := s.GetUserSettings(t.Context(), userID)
	require.NoError(t, err)
	assert.Equal(t, "sk-secret", setting.LlmApiKey)
}

func TestSettingsKeepsStoredAPIKey(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, userID := signUp(t, app, dbConn, "owner@example.com")

	res := putSettings(t, app, cookie, map[string]any{
		"provider": "openai",
		"base_url": "https://api.example.com/v1",
		"api_key":  "sk-secret",
		"model":    "example-1",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	res = putSettings(t, app, cookie, map[string]any{
		"provider": "openai",
		"base_url": "https://api.example.com/v1",
		"model":    "example-2",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	got := decodeSettings(t, res)
	assert.Equal(t, "example-2", got.Model)
	assert.True(t, got.HasAPIKey)

	setting, err := s.GetUserSettings(t.Context(), userID)
	require.NoError(t, err)
	assert.Equal(t, "sk-secret", setting.LlmApiKey)
}

func TestSettingsRequiresAPIKey(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := putSettings(t, app, cookie, map[string]any{
		"provider": "openai",
		"base_url": "https://api.example.com/v1",
		"model":    "example-1",
	})
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	assert.Equal(t, "api_key is required", decodeErrorMessage(t, res))
}

func TestSettingsValidation(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	t.Run("unknown provider", func(t *testing.T) {
		res := putSettings(t, app, cookie, map[string]any{"provider": "gemini"})
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "provider must be openai", decodeErrorMessage(t, res))
	})

	t.Run("missing base_url", func(t *testing.T) {
		res := putSettings(t, app, cookie, map[string]any{
			"provider": "openai",
			"api_key":  "sk-secret",
			"model":    "example-1",
		})
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "base_url is required", decodeErrorMessage(t, res))
	})

	t.Run("missing model", func(t *testing.T) {
		res := putSettings(t, app, cookie, map[string]any{
			"provider": "openai",
			"base_url": "https://api.example.com/v1",
			"api_key":  "sk-secret",
		})
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "model is required", decodeErrorMessage(t, res))
	})

	t.Run("fields without provider", func(t *testing.T) {
		res := putSettings(t, app, cookie, map[string]any{"model": "example-1"})
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "provider is required when other fields are set", decodeErrorMessage(t, res))
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader("{"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", cookie)
		res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
		require.NoError(t, err)
		t.Cleanup(func() { _ = res.Body.Close() })
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})
}

func TestSettingsClear(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, userID := signUp(t, app, dbConn, "owner@example.com")

	res := putSettings(t, app, cookie, map[string]any{
		"provider": "openai",
		"base_url": "https://api.example.com/v1",
		"api_key":  "sk-secret",
		"model":    "example-1",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	res = putSettings(t, app, cookie, map[string]any{})
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, settingsBody{}, decodeSettings(t, res))

	_, err := s.GetUserSettings(t.Context(), userID)
	require.Error(t, err)
}

// TestChatUsesUserSettings verifies that a chat run resolves the caller's
// stored settings instead of the server default provider.
func TestChatUsesUserSettings(t *testing.T) {
	var gotAuth string
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := putSettings(t, app, cookie, map[string]any{
		"provider": "openai",
		"base_url": upstream.URL,
		"api_key":  "sk-user-key",
		"model":    "user-model",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	t.Run("settings model", func(t *testing.T) {
		res := postChat(t, app, cookie, map[string]any{"message": "hi"})
		require.Equal(t, http.StatusOK, res.StatusCode)
		parseSSE(t, res.Body)

		assert.Equal(t, "Bearer sk-user-key", gotAuth)
		assert.Equal(t, "user-model", gotModel)
	})

	t.Run("request model override wins", func(t *testing.T) {
		res := postChat(t, app, cookie, map[string]any{"message": "hi", "model": "override-model"})
		require.Equal(t, http.StatusOK, res.StatusCode)
		parseSSE(t, res.Body)

		assert.Equal(t, "Bearer sk-user-key", gotAuth)
		assert.Equal(t, "override-model", gotModel)
	})
}
