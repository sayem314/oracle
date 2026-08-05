package chat_test

import (
	"database/sql"
	"fmt"
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

func TestLLMResolverDefault(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	defaultProvider := llm.NewMock()
	r := &chat.LLMResolver{Store: s, Default: defaultProvider}

	got, err := r.Resolve(t.Context(), 1)
	require.NoError(t, err)
	assert.Same(t, defaultProvider, got)
}

func TestLLMResolverUserSettings(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	seedUser(t, dbConn, 2)
	defaultProvider := llm.NewMock()
	r := &chat.LLMResolver{Store: s, Default: defaultProvider}

	_, err := s.UpsertUserSettings(t.Context(), db.UpsertUserSettingsParams{
		UserID:      1,
		LlmProvider: "openai",
		LlmBaseUrl:  "https://api.example.com/v1",
		LlmApiKey:   "sk-test",
		LlmModel:    "example-1",
	})
	require.NoError(t, err)

	got, err := r.Resolve(t.Context(), 1)
	require.NoError(t, err)
	assert.NotSame(t, defaultProvider, got)

	// A user without settings falls back to the default provider.
	other, err := r.Resolve(t.Context(), 2)
	require.NoError(t, err)
	assert.Same(t, defaultProvider, other)
}

func TestLLMResolverMissingAPIKey(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	_, err := dbConn.Exec(
		"INSERT INTO user_settings (user_id, llm_provider, llm_base_url, llm_api_key, llm_model) VALUES (1, 'openai', 'https://api.example.com/v1', '', 'example-1')",
	)
	require.NoError(t, err)

	_, err = r.Resolve(t.Context(), 1)
	require.ErrorContains(t, err, "missing an api key")
}
