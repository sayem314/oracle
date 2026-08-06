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

func TestProvidersAdminOnly(t *testing.T) {
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

	// Members can read the list (the chat picker needs it)...
	res = doProviderRequest(t, app, http.MethodGet, "/api/v1/llm/providers", memberCookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Len(t, decodeProviderList(t, res), 1)

	// ...but cannot create, edit, or delete providers.
	res = doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", memberCookie, validProviderPayload("mine"))
	require.Equal(t, http.StatusForbidden, res.StatusCode)
	path := "/api/v1/llm/providers/" + itoa(created.ID)
	res = doProviderRequest(t, app, http.MethodPatch, path, memberCookie, validProviderPayload("stolen"))
	require.Equal(t, http.StatusForbidden, res.StatusCode)
	res = doProviderRequest(t, app, http.MethodDelete, path, memberCookie, nil)
	require.Equal(t, http.StatusForbidden, res.StatusCode)
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

type prefsBody struct {
	ProviderID int64  `json:"provider_id"`
	Model      string `json:"model"`
}

func decodePrefs(t *testing.T, res *http.Response) prefsBody {
	t.Helper()
	var body prefsBody
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	return body
}

func TestLLMPrefs(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := doProviderRequest(t, app, http.MethodGet, "/api/v1/llm/prefs", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Zero(t, decodePrefs(t, res).ProviderID)

	// A pref must point at an existing provider.
	res = doProviderRequest(t, app, http.MethodPut, "/api/v1/llm/prefs", cookie, map[string]any{"provider_id": 999})
	require.Equal(t, http.StatusNotFound, res.StatusCode)

	res = doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, validProviderPayload("router"))
	require.Equal(t, http.StatusCreated, res.StatusCode)
	provider := decodeProvider(t, res)

	// The model must belong to the provider.
	res = doProviderRequest(t, app, http.MethodPut, "/api/v1/llm/prefs", cookie, map[string]any{"provider_id": provider.ID, "model": "nope"})
	require.Equal(t, http.StatusBadRequest, res.StatusCode)

	res = doProviderRequest(t, app, http.MethodPut, "/api/v1/llm/prefs", cookie, map[string]any{"provider_id": provider.ID, "model": "router-model-2"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	got := decodePrefs(t, res)
	assert.Equal(t, provider.ID, got.ProviderID)
	assert.Equal(t, "router-model-2", got.Model)

	res = doProviderRequest(t, app, http.MethodGet, "/api/v1/llm/prefs", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	got = decodePrefs(t, res)
	assert.Equal(t, provider.ID, got.ProviderID)
	assert.Equal(t, "router-model-2", got.Model)

	// provider_id 0 clears the pref.
	res = doProviderRequest(t, app, http.MethodPut, "/api/v1/llm/prefs", cookie, map[string]any{"provider_id": 0})
	require.Equal(t, http.StatusOK, res.StatusCode)
	res = doProviderRequest(t, app, http.MethodGet, "/api/v1/llm/prefs", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Zero(t, decodePrefs(t, res).ProviderID)

	// Deleting the provider leaves a dangling pref that reads back empty.
	res = doProviderRequest(t, app, http.MethodPut, "/api/v1/llm/prefs", cookie, map[string]any{"provider_id": provider.ID})
	require.Equal(t, http.StatusOK, res.StatusCode)
	res = doProviderRequest(t, app, http.MethodDelete, "/api/v1/llm/providers/"+itoa(provider.ID), cookie, nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	res = doProviderRequest(t, app, http.MethodGet, "/api/v1/llm/prefs", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Zero(t, decodePrefs(t, res).ProviderID)
}

// TestChatUsesUserPrefProvider verifies a user's stored default preference wins
// over the global default profile, and an explicit provider_id wins over both.
func TestChatUsesUserPrefProvider(t *testing.T) {
	var globalAuth, globalModel, prefAuth, prefModel string
	newUpstream := func(auth, model *string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*auth = r.Header.Get("Authorization")
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			*model = body.Model

			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		}))
	}
	globalUpstream := newUpstream(&globalAuth, &globalModel)
	defer globalUpstream.Close()
	prefUpstream := newUpstream(&prefAuth, &prefModel)
	defer prefUpstream.Close()

	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	globalPayload := validProviderPayload("global")
	globalPayload["base_url"] = globalUpstream.URL
	res := doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, globalPayload)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	global := decodeProvider(t, res)
	assert.True(t, global.Default)

	prefPayload := validProviderPayload("preferred")
	prefPayload["base_url"] = prefUpstream.URL
	res = doProviderRequest(t, app, http.MethodPost, "/api/v1/llm/providers", cookie, prefPayload)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	preferred := decodeProvider(t, res)

	t.Run("global default without pref", func(t *testing.T) {
		res := postChat(t, app, cookie, map[string]any{"message": "hi"})
		require.Equal(t, http.StatusOK, res.StatusCode)
		parseSSE(t, res.Body)

		assert.Equal(t, "Bearer sk-global", globalAuth)
		assert.Equal(t, "global-model-1", globalModel)
		assert.Empty(t, prefAuth)
	})

	// The user's pref provider and model beat the global default.
	res = doProviderRequest(t, app, http.MethodPut, "/api/v1/llm/prefs", cookie, map[string]any{
		"provider_id": preferred.ID,
		"model":       "preferred-model-2",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	res = postChat(t, app, cookie, map[string]any{"message": "hi"})
	require.Equal(t, http.StatusOK, res.StatusCode)
	parseSSE(t, res.Body)

	assert.Equal(t, "Bearer sk-preferred", prefAuth)
	assert.Equal(t, "preferred-model-2", prefModel)

	// An explicit provider_id on the request still wins.
	res = postChat(t, app, cookie, map[string]any{"message": "hi", "provider_id": global.ID})
	require.Equal(t, http.StatusOK, res.StatusCode)
	parseSSE(t, res.Body)

	assert.Equal(t, "Bearer sk-global", globalAuth)
	assert.Equal(t, "global-model-1", globalModel)
}
