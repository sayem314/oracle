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

// seedProvider creates an OpenAI-compatible profile with one default model and
// returns the provider id.
func seedProvider(t *testing.T, s store.Store, userID int64, name string, isDefault int64) int64 {
	t.Helper()

	provider, err := s.CreateLLMProvider(t.Context(), db.CreateLLMProviderParams{
		UserID:    userID,
		Name:      name,
		Provider:  "openai",
		BaseUrl:   "https://api.example.com/v1",
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

func TestLLMResolverServerDefault(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	defaultProvider := llm.NewMock()
	r := &chat.LLMResolver{Store: s, Default: defaultProvider}

	got, err := r.Resolve(t.Context(), 1, 0, "")
	require.NoError(t, err)
	assert.Same(t, defaultProvider, got)
}

func TestLLMResolverDefaultProfile(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	defaultProvider := llm.NewMock()
	r := &chat.LLMResolver{Store: s, Default: defaultProvider}

	seedProvider(t, s, 1, "main", 1)
	seedProvider(t, s, 1, "other", 0)

	got, err := r.Resolve(t.Context(), 1, 0, "")
	require.NoError(t, err)
	assert.NotSame(t, defaultProvider, got)
}

func TestLLMResolverExplicitProfile(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	mainID := seedProvider(t, s, 1, "main", 1)
	otherID := seedProvider(t, s, 1, "other", 0)

	got, err := r.Resolve(t.Context(), 1, otherID, "")
	require.NoError(t, err)
	assert.NotNil(t, got)

	// A profile of another user is not resolvable.
	seedUser(t, dbConn, 2)
	_, err = r.Resolve(t.Context(), 2, mainID, "")
	require.ErrorContains(t, err, "provider not found")

	_, err = r.Resolve(t.Context(), 1, 999, "")
	require.ErrorContains(t, err, "provider not found")
}

func TestLLMResolverProfileWithoutModels(t *testing.T) {
	s, dbConn := newStore(t)
	seedUser(t, dbConn, 1)
	r := &chat.LLMResolver{Store: s, Default: llm.NewMock()}

	provider, err := s.CreateLLMProvider(t.Context(), db.CreateLLMProviderParams{
		UserID: 1, Name: "bare", Provider: "openai",
		BaseUrl: "https://api.example.com/v1", ApiKey: "sk-bare", IsDefault: 1,
	})
	require.NoError(t, err)

	// No models configured: an explicit model override still works.
	got, err := r.Resolve(t.Context(), 1, provider.ID, "ad-hoc-model")
	require.NoError(t, err)
	assert.NotNil(t, got)

	// Without one, resolution fails cleanly instead of calling the gateway.
	_, err = r.Resolve(t.Context(), 1, provider.ID, "")
	require.ErrorContains(t, err, "no model configured")
}
