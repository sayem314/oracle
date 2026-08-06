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

type providerBody struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Provider     string   `json:"provider"`
	BaseURL      string   `json:"base_url"`
	HasAPIKey    bool     `json:"has_api_key"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"default_model"`
	Default      bool     `json:"default"`
}

func doProviderRequest(t *testing.T, app *fiber.App, method, path, cookie string, body any) *http.Response {
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

func decodeProvider(t *testing.T, res *http.Response) providerBody {
	t.Helper()
	var body providerBody
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	return body
}

func decodeProviderList(t *testing.T, res *http.Response) []providerBody {
	t.Helper()
	var list []providerBody
	require.NoError(t, json.NewDecoder(res.Body).Decode(&list))
	return list
}

func validProviderPayload(name string) map[string]any {
	return map[string]any{
		"name":          name,
		"provider":      "openai",
		"base_url":      "https://api.example.com/v1",
		"api_key":       "sk-" + name,
		"models":        []string{name + "-model-1", name + "-model-2"},
		"default_model": name + "-model-1",
	}
}

func TestProvidersEmptyByDefault(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := doProviderRequest(t, app, http.MethodGet, "/api/v1/llm/providers", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Empty(t, decodeProviderList(t, res))
}

func TestProvidersUnauthenticated(t *testing.T) {
	app, _, _ := newTestApp(t, llm.NewMock())

	res := doProviderRequest(t, app, http.MethodGet, "/api/v1/llm/providers", "", nil)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)

	res = doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", "", validProviderPayload("x"))
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
}

func TestProviderCreateAndList(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, validProviderPayload("router"))
	require.Equal(t, http.StatusCreated, res.StatusCode)

	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "sk-router", "api key must never be returned")

	res = doProviderRequest(t, app, http.MethodGet, "/api/v1/llm/providers", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	list := decodeProviderList(t, res)
	require.Len(t, list, 1)

	got := list[0]
	assert.Equal(t, "router", got.Name)
	assert.Equal(t, "openai", got.Provider)
	assert.Equal(t, "https://api.example.com/v1", got.BaseURL)
	assert.True(t, got.HasAPIKey)
	assert.Equal(t, []string{"router-model-1", "router-model-2"}, got.Models)
	assert.Equal(t, "router-model-1", got.DefaultModel)
	assert.True(t, got.Default, "first profile becomes the default")
}

func TestProviderCreateValidation(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	t.Run("missing name", func(t *testing.T) {
		payload := validProviderPayload("x")
		delete(payload, "name")
		res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, payload)
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("unknown provider", func(t *testing.T) {
		payload := validProviderPayload("x")
		payload["provider"] = "gemini"
		res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, payload)
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("missing base_url", func(t *testing.T) {
		payload := validProviderPayload("x")
		delete(payload, "base_url")
		res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, payload)
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("missing api_key", func(t *testing.T) {
		payload := validProviderPayload("x")
		delete(payload, "api_key")
		res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, payload)
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("default_model not in models", func(t *testing.T) {
		payload := validProviderPayload("x")
		payload["default_model"] = "not-listed"
		res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, payload)
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})
}

func TestProviderDuplicateName(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, validProviderPayload("router"))
	require.Equal(t, http.StatusCreated, res.StatusCode)

	res = doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, validProviderPayload("router"))
	require.Equal(t, http.StatusConflict, res.StatusCode)
	assert.Equal(t, "a provider with this name already exists", decodeErrorMessage(t, res))
}

func TestProviderUpdate(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, validProviderPayload("router"))
	require.Equal(t, http.StatusCreated, res.StatusCode)
	created := decodeProvider(t, res)

	t.Run("keeps stored key, replaces models", func(t *testing.T) {
		payload := validProviderPayload("router")
		delete(payload, "api_key")
		payload["models"] = []string{"new-model"}
		payload["default_model"] = "new-model"

		res := doProviderRequest(t, app, http.MethodPatch, "/api/v1/llm/providers/"+itoa(created.ID), cookie, payload)
		require.Equal(t, http.StatusOK, res.StatusCode)
		got := decodeProvider(t, res)
		assert.True(t, got.HasAPIKey)
		assert.Equal(t, []string{"new-model"}, got.Models)
		assert.Equal(t, "new-model", got.DefaultModel)

		stored, err := s.GetLLMProvider(t.Context(), created.ID)
		require.NoError(t, err)
		assert.Equal(t, "sk-router", stored.ApiKey)
	})

	t.Run("default moves between profiles", func(t *testing.T) {
		res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, validProviderPayload("local"))
		require.Equal(t, http.StatusCreated, res.StatusCode)
		local := decodeProvider(t, res)
		assert.False(t, local.Default)

		payload := validProviderPayload("local")
		delete(payload, "api_key")
		payload["default"] = true
		res = doProviderRequest(t, app, http.MethodPatch, "/api/v1/llm/providers/"+itoa(local.ID), cookie, payload)
		require.Equal(t, http.StatusOK, res.StatusCode)
		assert.True(t, decodeProvider(t, res).Default)

		res = doProviderRequest(t, app, http.MethodGet, "/api/v1/llm/providers", cookie, nil)
		list := decodeProviderList(t, res)
		require.Len(t, list, 2)
		defaults := 0
		for _, p := range list {
			if p.Default {
				defaults++
			}
		}
		assert.Equal(t, 1, defaults)
	})
}

func TestProviderDelete(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, validProviderPayload("router"))
	require.Equal(t, http.StatusCreated, res.StatusCode)
	created := decodeProvider(t, res)

	res = doProviderRequest(t, app, http.MethodDelete, "/api/v1/llm/providers/"+itoa(created.ID), cookie, nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)

	_, err := s.GetLLMProvider(t.Context(), created.ID)
	require.Error(t, err)

	res = doProviderRequest(t, app, http.MethodDelete, "/api/v1/llm/providers/"+itoa(created.ID), cookie, nil)
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestProvidersIsolatedPerUser(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	ownerCookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := doUsersRequest(t, app, http.MethodPost, "/api/v1/users", ownerCookie, map[string]any{
		"email":    "member@example.com",
		"password": "Secure1pass",
	})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	memberCookie := signIn(t, app, "member@example.com", "Secure1pass")

	res = doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", ownerCookie, validProviderPayload("router"))
	require.Equal(t, http.StatusCreated, res.StatusCode)
	created := decodeProvider(t, res)

	res = doProviderRequest(t, app, http.MethodGet, "/api/v1/llm/providers", memberCookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Empty(t, decodeProviderList(t, res))

	path := "/api/v1/llm/providers/" + itoa(created.ID)
	res = doProviderRequest(t, app, http.MethodPatch, path, memberCookie, validProviderPayload("stolen"))
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	res = doProviderRequest(t, app, http.MethodDelete, path, memberCookie, nil)
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

// TestChatUsesSelectedProvider verifies the chat request's provider_id picks
// a profile, with the model override applied on top.
func TestChatUsesSelectedProvider(t *testing.T) {
	var gotAuth, gotModel string
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

	payload := validProviderPayload("router")
	payload["base_url"] = upstream.URL
	res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, payload)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	provider := decodeProvider(t, res)

	t.Run("profile default model", func(t *testing.T) {
		res := postChat(t, app, cookie, map[string]any{"message": "hi", "provider_id": provider.ID})
		require.Equal(t, http.StatusOK, res.StatusCode)
		parseSSE(t, res.Body)

		assert.Equal(t, "Bearer sk-router", gotAuth)
		assert.Equal(t, "router-model-1", gotModel)
	})

	t.Run("model override wins", func(t *testing.T) {
		res := postChat(t, app, cookie, map[string]any{
			"message":     "hi",
			"provider_id": provider.ID,
			"model":       "router-model-2",
		})
		require.Equal(t, http.StatusOK, res.StatusCode)
		parseSSE(t, res.Body)

		assert.Equal(t, "Bearer sk-router", gotAuth)
		assert.Equal(t, "router-model-2", gotModel)
	})

	t.Run("unknown provider errors in stream", func(t *testing.T) {
		res := postChat(t, app, cookie, map[string]any{"message": "hi", "provider_id": 999})
		require.Equal(t, http.StatusOK, res.StatusCode)

		frames := parseSSE(t, res.Body)
		errs := framesByName(frames, "error")
		require.Len(t, errs, 1)
		var chatErr errorEvent
		decodeFrame(t, errs[0], &chatErr)
		assert.Contains(t, chatErr.Message, "provider not found")
	})
}
