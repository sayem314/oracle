package chat

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/permission"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
	"github.com/sayem314/oracle/apps/api/internal/tool"
	"github.com/sayem314/oracle/apps/api/internal/tool/loop"
)

// scriptedProvider serves a fixed list of turns, letting tests drive the
// model's tool calls deterministically.
type scriptedProvider struct {
	turns []llm.Chunk
	index int
}

func (p *scriptedProvider) Chat(_ context.Context, _ llm.Request) (llm.Stream, error) {
	if p.index >= len(p.turns) {
		return nil, fmt.Errorf("scriptedProvider: ran out of turns")
	}
	chunk := p.turns[p.index]
	p.index++
	return &oneChunkStream{chunk: chunk}, nil
}

type oneChunkStream struct {
	chunk llm.Chunk
	sent  bool
}

func (s *oneChunkStream) Next() bool {
	if s.sent {
		return false
	}
	s.sent = true
	return true
}

func (s *oneChunkStream) Current() llm.Chunk { return s.chunk }
func (s *oneChunkStream) Err() error         { return nil }
func (s *oneChunkStream) Close() error       { return nil }

type fixedResolver struct {
	provider llm.Provider
}

func (r *fixedResolver) Resolve(context.Context, string) (llm.Provider, error) {
	return r.provider, nil
}

// TestSetLoopToolEnablesSessionLoop proves the model can start the session's
// goal loop from inside a run, the mechanism behind "check every few minutes
// and report me".
func TestSetLoopToolEnablesSessionLoop(t *testing.T) {
	st, _ := newStore(t)
	session, err := st.CreateSession(t.Context(), "")
	require.NoError(t, err)

	registry := tool.NewRegistry()
	require.NoError(t, registry.RegisterGroups(loop.New))

	provider := &scriptedProvider{turns: []llm.Chunk{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:        "call-1",
				Name:      "set_loop",
				Arguments: `{"enabled":true,"interval":"5m"}`,
			}},
		},
		{Delta: "Loop enabled; I will check every 5 minutes.", FinishReason: "stop"},
	}}

	engine := &Engine{
		Store:       st,
		LLM:         &fixedResolver{provider: provider},
		Tools:       registry,
		Permissions: permission.NewRuleset(permission.Allow, nil),
	}

	err = engine.Run(t.Context(), DiscardSink{}, session.ID, llm.Request{
		Model: "test",
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: "check the weather every few minutes and report me",
		}},
	})
	require.NoError(t, err)

	got, err := st.GetSession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.LoopEnabled)
	assert.Equal(t, "5m", got.LoopInterval)
	assert.True(t, got.LoopNextRunAt.Valid, "enabling should queue the first run")
}

func TestSetLoopToolCanDisable(t *testing.T) {
	st, _ := newStore(t)
	session, err := st.CreateSession(t.Context(), "")
	require.NoError(t, err)
	require.NoError(t, st.UpdateSessionLoop(t.Context(), db.UpdateSessionLoopParams{
		ID:            session.ID,
		LoopEnabled:   1,
		LoopInterval:  "30s",
		LoopNextRunAt: sql.NullInt64{Int64: 1, Valid: true},
	}))

	registry := tool.NewRegistry()
	require.NoError(t, registry.RegisterGroups(loop.New))

	provider := &scriptedProvider{turns: []llm.Chunk{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:        "call-1",
				Name:      "set_loop",
				Arguments: `{"enabled":false}`,
			}},
		},
		{Delta: "Stopped the loop.", FinishReason: "stop"},
	}}

	engine := &Engine{
		Store:       st,
		LLM:         &fixedResolver{provider: provider},
		Tools:       registry,
		Permissions: permission.NewRuleset(permission.Allow, nil),
	}

	require.NoError(t, engine.Run(t.Context(), DiscardSink{}, session.ID, llm.Request{
		Model:    "test",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "stop monitoring"}},
	}))

	got, err := st.GetSession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), got.LoopEnabled)
	assert.False(t, got.LoopNextRunAt.Valid, "disabling should cancel queued runs")
}
