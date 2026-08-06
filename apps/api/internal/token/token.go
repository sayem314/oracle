package token

import (
	"encoding/json"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

// charsPerToken is the estimate ratio opencode uses (4 chars per token). It is
// deliberately crude: it only has to be good enough to trigger compaction
// consistently, not to price billed tokens.
const charsPerToken = 4

// Estimate returns an approximate token count for a string.
func Estimate(s string) int {
	n := max(len(s)/charsPerToken, 1)
	return n
}

// EstimateMessages returns an approximate token count for a provider-facing
// message list, including a small per-message overhead and tool-call content.
func EstimateMessages(msgs []llm.Message) int {
	const overhead = 3
	total := 0
	for _, m := range msgs {
		total += Estimate(m.Content) + overhead
		for _, tc := range m.ToolCalls {
			total += Estimate(tc.Name) + Estimate(tc.Arguments)
		}
	}
	return total
}

// EstimateJSON returns an approximate token count for a serialized value.
func EstimateJSON(v any) int {
	b, _ := json.Marshal(v)
	return Estimate(string(b))
}
