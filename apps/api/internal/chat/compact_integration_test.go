package chat_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/permission"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

// fixedResolver hands every run the same provider.
type fixedResolver struct{ p llm.Provider }

func (f fixedResolver) Resolve(ctx context.Context, model string) (llm.Provider, error) {
	return f.p, nil
}

// TestRunCompactsAndPersistsSummary drives Engine.Run with a compaction config
// small enough that the assembled history overflows, and verifies the summary
// is written to the session row.
func TestRunCompactsAndPersistsSummary(t *testing.T) {
	s, _ := newStore(t)

	session, err := s.CreateSession(t.Context(), "")
	require.NoError(t, err)

	mock := llm.NewMock()
	engine := &chat.Engine{
		Store:       s,
		LLM:         fixedResolver{p: mock},
		Tools:       tool.NewRegistry(),
		Permissions: permission.NewRuleset(permission.Allow, nil),
		Compaction: chat.CompactionConfig{
			ContextWindow:    200,
			ReserveTokens:    50,
			TailTurns:        2,
			KeepRecentTokens: 0,
			ToolOutputChars:  2000,
		},
	}

	// Seed a long conversation so a tiny window overflows.
	for range 20 {
		_, err := s.AppendMessage(t.Context(), db.AppendMessageParams{
			SessionID: session.ID,
			Role:      string(llm.RoleUser),
			Content:   "question " + manyX(100),
		})
		require.NoError(t, err)
	}

	// Callers hand Run the assembled history (the server uses BuildHistory).
	history, err := engine.BuildHistory(t.Context(), session.ID)
	require.NoError(t, err)

	err = engine.Run(t.Context(), chat.DiscardSink{}, session.ID,
		llm.Request{Messages: history})
	require.NoError(t, err)

	got, err := s.GetSession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, got.Summary)
}

func manyX(n int) string {
	s := make([]byte, n+30)
	for i := range s {
		s[i] = 'x'
	}
	return string(s)
}
