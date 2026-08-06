package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/token"
)

// CompactionConfig controls when and how long conversations are condensed.
// Zero values disable compaction; all values are token estimates.
type CompactionConfig struct {
	// ContextWindow is the estimated usable context budget in tokens.
	ContextWindow int
	// ReserveTokens is set aside after ContextWindow for the summary call and
	// output; compaction triggers past ContextWindow - ReserveTokens.
	ReserveTokens int
	// TailTurns is how many recent user turns are kept verbatim.
	TailTurns int
	// KeepRecentTokens is the budget for the verbatim recent tail.
	KeepRecentTokens int
	// ToolOutputChars truncates tool results before they reach the model.
	ToolOutputChars int
}

// Enabled reports whether compaction or tool-output truncation is active.
func (c CompactionConfig) Enabled() bool {
	return c.ContextWindow > 0 && c.ReserveTokens >= 0 && c.TailTurns > 0
}

// summarizePrompt instructs the model to condense the head of a conversation.
const summarizePrompt = "Condense the conversation so far into a concise summary that preserves " +
	"the user's goals, key decisions, and any facts a follow-up turn needs. " +
	"Do not reply like an assistant to the user. Output only the summary text."

const summaryRole = "user"

// compactHistory applies compaction: it splits msgs into a head (to summarize)
// and a verbatim recent tail, asks summarize to compress the head, and returns
// the rebuilt history along with the produced summary (empty when no compaction
// happened). When the summarizer fails it falls back to dropping the head
// entirely, returning no summary, so an interactive chat is never broken.
func (e *Engine) compactHistory(ctx context.Context, cfg CompactionConfig, msgs []llm.Message, summarize summarizeFunc) ([]llm.Message, string, bool) {
	if !cfg.Enabled() || len(msgs) == 0 {
		return msgs, "", false
	}
	usable := cfg.ContextWindow - cfg.ReserveTokens
	if usable <= 0 || token.EstimateMessages(msgs) < usable {
		return msgs, "", false
	}

	head, tail := splitRecent(msgs, cfg.TailTurns, cfg.KeepRecentTokens)
	if len(head) == 0 {
		return msgs, "", false
	}

	if summary, err := summarize(ctx, head); err == nil && strings.TrimSpace(summary) != "" {
		return append([]llm.Message{{Role: llm.Role(summaryRole), Content: summary}}, tail...), summary, true
	}
	// Fail-soft: summarize failed, so drop the head rather than erroring.
	return tail, "", true
}

// summarizeFunc summarizes a message slice into a short recap. It is a field
// seam so tests can inject a fake without an LLM.
type summarizeFunc func(ctx context.Context, head []llm.Message) (string, error)

// summarizer runs a single non-tool LLM turn to condense head. provider must
// already be resolved. The model context may overflow on very long heads; the
// caller's fallback path in compactHistory tolerates the error.
func (e *Engine) summarizer(provider llm.Provider, model string) summarizeFunc {
	return func(ctx context.Context, head []llm.Message) (string, error) {
		stream, err := provider.Chat(ctx, llm.Request{
			System:   summarizePrompt,
			Messages: head,
		})
		if err != nil {
			return "", err
		}
		defer stream.Close() //nolint:errcheck

		var sb strings.Builder
		for stream.Next() {
			sb.WriteString(stream.Current().Delta)
		}
		if err := stream.Err(); err != nil {
			return "", err
		}
		return strings.TrimSpace(sb.String()), nil
	}
}

// splitRecent separates msgs into a head (everything to summarize) and a
// verbatim tail. It keeps the last tailTurns user turns, walking backward from
// the end so the newest context is always preserved. When keepRecent > 0, the
// tail is then shrunk from the front, dropping the oldest turns first, until it
// fits within the budget or only one turn remains.
func splitRecent(msgs []llm.Message, tailTurns, keepRecent int) (head, tail []llm.Message) {
	if len(msgs) == 0 {
		return msgs, nil
	}

	// Start with the full tail and trim the oldest turns until tailTurns remain.
	tail = dropOldestTurns(msgs, tailTurns)
	head = msgs[:len(msgs)-len(tail)]

	if keepRecent > 0 {
		for len(tail) > 1 && token.EstimateMessages(tail) > keepRecent {
			trimmed := dropOldestTurn(tail)
			if len(trimmed) == len(tail) {
				break
			}
			tail = trimmed
		}
		head = msgs[:len(msgs)-len(tail)]
	}
	return head, tail
}

// dropOldestTurns removes the oldest turns from msgs until n user turns remain,
// returning the newest n-turn suffix. A turn is a user message plus the
// assistant/tool messages after it up to the next user message.
func dropOldestTurns(msgs []llm.Message, n int) []llm.Message {
	userCount := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleUser && msgs[i].ToolCallID == "" {
			userCount++
			if userCount == n {
				return msgs[i:]
			}
		}
	}
	return msgs
}

// dropOldestTurn removes the oldest turn from msgs, returning the rest.
func dropOldestTurn(msgs []llm.Message) []llm.Message {
	for i := 0; i < len(msgs); i++ {
		if msgs[i].Role == llm.RoleUser && msgs[i].ToolCallID == "" {
			return msgs[i+1:]
		}
	}
	return msgs
}

// truncateToolOutputs caps tool-result content at n runes so a runaway tool
// cannot flood the assembled context.
func truncateToolOutputs(msgs []llm.Message, n int) []llm.Message {
	if n <= 0 {
		return msgs
	}
	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		out[i] = m
		if m.Role == llm.RoleTool && m.Content != "" {
			r := []rune(m.Content)
			if len(r) > n {
				out[i].Content = string(r[:n]) + fmt.Sprintf("\n[truncated: tool output exceeds %d runs]", n)
			}
		}
	}
	return out
}
