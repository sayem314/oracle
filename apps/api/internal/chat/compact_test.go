package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

func turns(n int) []llm.Message {
	// Interleave user + assistant like a real transcript.
	var msgs []llm.Message
	for i := 0; i < n; i++ {
		msgs = append(msgs,
			llm.Message{Role: llm.RoleUser, Content: "user turn " + itoa(i)},
			llm.Message{Role: llm.RoleAssistant, Content: "reply to turn " + itoa(i)},
		)
	}
	return msgs
}

func itoa(i int) string {
	return strings.Repeat("x", i+1)
}

func TestSplitRecentKeepsOnlyTailTurns(t *testing.T) {
	msgs := turns(6)
	head, tail := splitRecent(msgs, 2, 0)
	// Two user turns = 4 messages, and head ends on the prior assistant reply.
	assert.Len(t, tail, 4)
	assert.Equal(t, "user turn ", strings.TrimRight(tail[0].Content, "x"))
	assert.Equal(t, "reply to turn ", strings.TrimRight(head[len(head)-1].Content, "x"))
	assert.Len(t, head, len(msgs)-4)
}

func TestSplitRecentRespectsBudget(t *testing.T) {
	msgs := turns(20)
	// A tiny budget must force the tail below the full two-turn size.
	_, tail := splitRecent(msgs, 2, 0)
	fullSize := len(tail)
	_, tail = splitRecent(msgs, 2, 8)
	assert.Less(t, len(tail), fullSize)
}

func TestSplitRecentEmpty(t *testing.T) {
	head, tail := splitRecent(nil, 2, 0)
	assert.Empty(t, head)
	assert.Empty(t, tail)
}

func TestTruncateToolOutputs(t *testing.T) {
	const n = 50
	msgs := []llm.Message{
		{Role: llm.RoleTool, Content: strings.Repeat("z", 500), ToolCallID: "t1"},
		{Role: llm.RoleUser, Content: "small"},
	}
	out := truncateToolOutputs(msgs, n)
	got := out[0].Content
	assert.Len(t, []rune(strings.Split(got, "\n")[0]), n)
	assert.Contains(t, got, "[truncated: tool output exceeds 50 runs]")
	assert.Equal(t, "small", out[1].Content)
}

func TestTruncateToolOutputsDisabledOnZero(t *testing.T) {
	msgs := []llm.Message{{Role: llm.RoleTool, Content: strings.Repeat("z", 500)}}
	out := truncateToolOutputs(msgs, 0)
	assert.Equal(t, msgs, out)
}

func TestCompactHistoryBelowThresholdUnchanged(t *testing.T) {
	e := &Engine{}
	msgs := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	cfg := CompactionConfig{ContextWindow: 32000, ReserveTokens: 20000, TailTurns: 2, KeepRecentTokens: 2000}
	got, summary, ok := e.compactHistory(context.Background(), cfg, msgs, func(ctx context.Context, _ []llm.Message) (string, error) {
		t.Fatal("summarize should not be called under budget")
		return "", nil
	})
	assert.False(t, ok)
	assert.Empty(t, summary)
	assert.Equal(t, msgs, got)
}

func TestCompactHistorySummarizesHead(t *testing.T) {
	e := &Engine{}
	msgs := turns(40) // big enough to exceed a tiny window
	cfg := CompactionConfig{ContextWindow: 200, ReserveTokens: 50, TailTurns: 2, KeepRecentTokens: 0}
	called := false
	got, summary, ok := e.compactHistory(context.Background(), cfg, msgs,
		func(_ context.Context, head []llm.Message) (string, error) {
			called = true
			assert.NotEmpty(t, head)
			return "condensed history", nil
		})
	require.True(t, ok)
	require.True(t, called)
	assert.Equal(t, "condensed history", summary)
	require.Len(t, got, 1+4) // summary + 2 recent turns
	assert.Equal(t, "condensed history", got[0].Content)
}

func TestCompactHistoryFailsSoftOnSummaryError(t *testing.T) {
	e := &Engine{}
	msgs := turns(40)
	cfg := CompactionConfig{ContextWindow: 200, ReserveTokens: 50, TailTurns: 2, KeepRecentTokens: 0}
	got, summary, ok := e.compactHistory(context.Background(), cfg, msgs,
		func(_ context.Context, _ []llm.Message) (string, error) { return "", errCompaction })
	require.True(t, ok)
	assert.Empty(t, summary)
	// Head dropped, recent tail kept verbatim.
	assert.NotContains(t, got[0].Content, "condensed")
	assert.Len(t, got, 4)
}

var errCompaction = &testErr{}

type testErr struct{}

func (*testErr) Error() string { return "compaction failed" }
