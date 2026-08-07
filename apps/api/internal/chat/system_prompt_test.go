package chat

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestBuildSystemPromptNoStore proves the base prompt stands alone when there
// is no store or settings row.
func TestBuildSystemPromptNoStore(t *testing.T) {
	prompt := buildSystemPrompt(t.Context(), nil)
	assert.Contains(t, prompt, "You are oracle")
	assert.NotContains(t, prompt, "Administrator instructions")
}

func TestBuildSystemPromptEmptyStore(t *testing.T) {
	s, _ := newStore(t)
	prompt := buildSystemPrompt(t.Context(), s)
	assert.Contains(t, prompt, "You are oracle")
	assert.NotContains(t, prompt, "Administrator instructions")
}

func TestBuildSystemPromptWithInstructions(t *testing.T) {
	s, _ := newStore(t)
	_, err := s.UpsertSettings(t.Context(), db.UpsertSettingsParams{
		PermissionDefault: "ask",
		Instructions:      "Always answer in haiku.",
	})
	require.NoError(t, err)

	prompt := buildSystemPrompt(t.Context(), s)
	assert.Contains(t, prompt, "You are oracle")
	assert.Contains(t, prompt, "Administrator instructions:")
	assert.Contains(t, prompt, "Always answer in haiku.")
}

func TestBuildSystemPromptIgnoresBlankInstructions(t *testing.T) {
	s, _ := newStore(t)
	_, err := s.UpsertSettings(t.Context(), db.UpsertSettingsParams{
		PermissionDefault: "ask",
		Instructions:      "   ",
	})
	require.NoError(t, err)

	prompt := buildSystemPrompt(t.Context(), s)
	assert.NotContains(t, prompt, "Administrator instructions")
}
