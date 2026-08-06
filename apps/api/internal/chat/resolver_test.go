package chat_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

func newStore(t *testing.T) (store.Store, *sql.DB) {
	t.Helper()

	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") + "?_pragma=foreign_keys(ON)"
	dbConn, err := store.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	_, err = store.Migrate(dbConn)
	require.NoError(t, err)

	return store.New(dbConn), dbConn
}

func seedUser(t *testing.T, dbConn *sql.DB, id int64) {
	t.Helper()

	_, err := dbConn.Exec(
		"INSERT INTO auth_users (id, email) VALUES (?, ?)",
		id, fmt.Sprintf("user%d@example.com", id),
	)
	require.NoError(t, err)
}

// upstreamRecorder is a fake OpenAI-compatible gateway that records the auth
// header and request model so tests can assert which provider was picked.
type upstreamRecorder struct {
	server *httptest.Server
	auth   string
	model  string
}

func newUpstream(t *testing.T) *upstreamRecorder {
	t.Helper()

	u := &upstreamRecorder{}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.auth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		u.model = body.Model

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(u.server.Close)
	return u
}

// seedProvider creates an OpenAI-compatible profile pointing at the given
// gateway with one default model and returns the provider id.
func seedProvider(t *testing.T, s store.Store, name, baseURL string, isDefault int64) int64 {
	t.Helper()

	provider, err := s.CreateLLMProvider(t.Context(), db.CreateLLMProviderParams{
		Name:      name,
		Provider:  "openai",
		BaseUrl:   baseURL,
		ApiKey:    "sk-" + name,
		IsDefault: isDefault,
	})
	require.NoError(t, err)
	require.NoError(t, s.InsertLLMModel(t.Context(), db.InsertLLMModelParams{
		ProviderID: provider.ID,
		Name:       name + "-model",
		IsDefault:  1,
	}))
	return provider.ID
}

func seedPref(t *testing.T, s store.Store, userID, providerID int64, model string) {
	t.Helper()

	_, err := s.UpsertUserLLMPrefs(t.Context(), db.UpsertUserLLMPrefsParams{
		UserID:     userID,
		ProviderID: sql.NullInt64{Int64: providerID, Valid: true},
		Model:      model,
	})
	require.NoError(t, err)
}

// chatOnce streams one turn through the provider so the upstream records it.
func chatOnce(t *testing.T, p llm.Provider) {
	t.Helper()

	stream, err := p.Chat(t.Context(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
}

func TestLLMResolverServerDefault(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	defaultProvider := llm.NewMock()
	r := &chat.LLMResolver{Store: s, Default: defaultProvider}

	got, err := r.Resolve(t.Context(), 1, 0, "")
	require.NoError(t, err)
	assert.Same(t, defaultProvider, got)
}

func TestLLMResolverGlobalDefaultProfile(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	up := newUpstream(t)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	seedProvider(t, s, "other", up.server.URL, 0)
	seedProvider(t, s, "main", up.server.URL, 1)

	got, err := r.Resolve(t.Context(), 1, 0, "")
	require.NoError(t, err)
	chatOnce(t, got)

	assert.Equal(t, "Bearer sk-main", up.auth)
	assert.Equal(t, "main-model", up.model)
}

func TestLLMResolverPrefBeatsGlobalDefault(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	globalUp := newUpstream(t)
	prefUp := newUpstream(t)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	seedProvider(t, s, "global", globalUp.server.URL, 1)
	prefID := seedProvider(t, s, "preferred", prefUp.server.URL, 0)
	seedPref(t, s, 1, prefID, "")

	got, err := r.Resolve(t.Context(), 1, 0, "")
	require.NoError(t, err)
	chatOnce(t, got)

	assert.Equal(t, "Bearer sk-preferred", prefUp.auth)
	assert.Equal(t, "preferred-model", prefUp.model)
	assert.Empty(t, globalUp.auth)
}

func TestLLMResolverExplicitProfile(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	up := newUpstream(t)
	otherUp := newUpstream(t)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	mainID := seedProvider(t, s, "main", up.server.URL, 1)
	otherID := seedProvider(t, s, "other", otherUp.server.URL, 0)

	// The explicit provider wins even when the pref points elsewhere.
	seedPref(t, s, 1, mainID, "")

	got, err := r.Resolve(t.Context(), 1, otherID, "")
	require.NoError(t, err)
	chatOnce(t, got)
	assert.Equal(t, "Bearer sk-other", otherUp.auth)

	_, err = r.Resolve(t.Context(), 1, 999, "")
	require.ErrorContains(t, err, "provider not found")
}

func TestLLMResolverPrefModel(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	up := newUpstream(t)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	provider, err := s.CreateLLMProvider(t.Context(), db.CreateLLMProviderParams{
		Name: "pref", Provider: "openai",
		BaseUrl: up.server.URL, ApiKey: "sk-pref", IsDefault: 1,
	})
	require.NoError(t, err)
	require.NoError(t, s.InsertLLMModel(t.Context(), db.InsertLLMModelParams{
		ProviderID: provider.ID, Name: "default-model", IsDefault: 1,
	}))
	seedPref(t, s, 1, provider.ID, "pref-model")

	// The pref's model beats the provider's default model.
	got, err := r.Resolve(t.Context(), 1, 0, "")
	require.NoError(t, err)
	chatOnce(t, got)
	assert.Equal(t, "pref-model", up.model)

	// A per-request model beats the pref.
	got, err = r.Resolve(t.Context(), 1, 0, "request-model")
	require.NoError(t, err)
	chatOnce(t, got)
	assert.Equal(t, "request-model", up.model)
}

func TestLLMResolverPrefForDeletedProvider(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	up := newUpstream(t)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	prefID := seedProvider(t, s, "gone", up.server.URL, 0)
	seedPref(t, s, 1, prefID, "gone-model")
	require.NoError(t, s.DeleteLLMProvider(t.Context(), prefID))

	// The dangling pref falls through to the global default (here: none).
	got, err := r.Resolve(t.Context(), 1, 0, "")
	require.NoError(t, err)
	assert.Same(t, r.Default, got)
}

func TestLLMResolverProfileWithoutModels(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	up := newUpstream(t)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	provider, err := s.CreateLLMProvider(t.Context(), db.CreateLLMProviderParams{
		Name: "bare", Provider: "openai",
		BaseUrl: up.server.URL, ApiKey: "sk-bare", IsDefault: 1,
	})
	require.NoError(t, err)

	// No models configured: an explicit model override still works.
	got, err := r.Resolve(t.Context(), 1, provider.ID, "ad-hoc-model")
	require.NoError(t, err)
	chatOnce(t, got)
	assert.Equal(t, "ad-hoc-model", up.model)

	// Without one, resolution fails cleanly instead of calling the gateway.
	_, err = r.Resolve(t.Context(), 1, provider.ID, "")
	require.ErrorContains(t, err, "no model configured")
}
