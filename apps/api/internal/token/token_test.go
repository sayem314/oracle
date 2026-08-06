package token_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/token"
)

func TestEstimateScalesWithLength(t *testing.T) {
	short := token.Estimate("hello")
	long := token.Estimate(strings.Repeat("a", 400))
	assert.Less(t, short, long)
	assert.Equal(t, 100, long)
}

func TestEstimateNonEmpty(t *testing.T) {
	assert.GreaterOrEqual(t, token.Estimate(""), 1)
}

func TestEstimateMessagesGrows(t *testing.T) {
	one := token.EstimateMessages([]llm.Message{{Role: llm.RoleUser, Content: "hi"}})
	many := token.EstimateMessages([]llm.Message{
		{Role: llm.RoleUser, Content: "hi"},
		{Role: llm.RoleAssistant, Content: "hello there"},
		{Role: llm.RoleUser, Content: strings.Repeat("x", 200)},
	})
	assert.Less(t, one, many)
}

func TestEstimateJSON(t *testing.T) {
	n := token.EstimateJSON(map[string]string{"a": "b"})
	assert.GreaterOrEqual(t, n, 1)
}
