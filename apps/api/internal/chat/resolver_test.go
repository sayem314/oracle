package chat_test

import (
	"database/sql"
	"encoding/json"
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

// seedGlobalProvider writes the singleton provider row pointing at the given
// gateway with the stored model.
func seedGlobalProvider(t *testing.T, s store.Store, baseURL, apiKey, model string) {
	t.Helper()

	_, err := s.UpsertLLMProvider(t.Context(), db.UpsertLLMProviderParams{
		Provider: "openai",
		BaseUrl:  baseURL,
		ApiKey:   apiKey,
		Model:    model,
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
	s, _ := newStore(t)
	defaultProvider := llm.NewMock()
	r := &chat.LLMResolver{Store: s, Default: defaultProvider}

	got, err := r.Resolve(t.Context(), "")
	require.NoError(t, err)
	assert.Same(t, defaultProvider, got)
}

func TestLLMResolverUsesStoredModel(t *testing.T) {
	s, _ := newStore(t)
	up := newUpstream(t)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	seedGlobalProvider(t, s, up.server.URL, "sk-main", "stored-model")

	got, err := r.Resolve(t.Context(), "")
	require.NoError(t, err)
	chatOnce(t, got)

	assert.Equal(t, "Bearer sk-main", up.auth)
	assert.Equal(t, "stored-model", up.model)
}

func TestLLMResolverRequestModelOverrides(t *testing.T) {
	s, _ := newStore(t)
	up := newUpstream(t)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	seedGlobalProvider(t, s, up.server.URL, "sk-main", "stored-model")

	got, err := r.Resolve(t.Context(), "request-model")
	require.NoError(t, err)
	chatOnce(t, got)

	assert.Equal(t, "request-model", up.model)
}

func TestLLMResolverEmptyModelFails(t *testing.T) {
	s, _ := newStore(t)
	up := newUpstream(t)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	_, err := s.UpsertLLMProvider(t.Context(), db.UpsertLLMProviderParams{
		Provider: "openai",
		BaseUrl:  up.server.URL,
		ApiKey:   "sk-main",
		Model:    "",
	})
	require.NoError(t, err)

	_, err = r.Resolve(t.Context(), "")
	require.ErrorContains(t, err, "no model configured")
}
