package tool

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

// NewBuiltin returns the tools oracle ships with.
func NewBuiltin() []Tool {
	return []Tool{
		getTimeTool(),
		webFetchTool(httpClient()),
		webSearchTool(httpClient()),
		mathEvalTool(),
	}
}

func getTimeTool() Tool {
	schema := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	return Tool{
		Definition: llm.Tool{
			Name:        "get_time",
			Description: "Return the current date and time in UTC as RFC 3339.",
			Parameters:  schema,
		},
		Execute: func(_ context.Context, _ json.RawMessage) (string, error) {
			return time.Now().UTC().Format(time.RFC3339), nil
		},
	}
}
