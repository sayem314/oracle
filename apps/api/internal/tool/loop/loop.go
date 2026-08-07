package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

// New returns the set_loop tool, which configures the current session's goal
// loop. The engine only makes the control available inside a run.
func New() []tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"enabled":{"type":"boolean","description":"Whether the session loop should run."},"interval":{"type":"string","description":"Delay between iterations, e.g. \"30s\" or \"5m\"; empty or omitted means continuous."}},"additionalProperties":false}`)
	return []tool.Tool{
		{
			Definition: llm.Tool{
				Name: "set_loop",
				Description: "Enable, disable, or re-interval the current session's goal loop. A loop re-runs the " +
					"conversation headlessly on the interval so the task keeps making progress without the user present, " +
					"e.g. \"check the weather every few minutes and report\". Enable with an interval to start; disable " +
					"to stop; pass only the interval to change the pace of a running loop. This persistently changes " +
					"the session and keeps running until disabled or the run budget is exhausted.",
				Parameters: schema,
			},
			Execute: execute,
		},
	}
}

func execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Enabled  *bool   `json:"enabled"`
		Interval *string `json:"interval"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("set_loop: %w", err)
	}
	if in.Enabled == nil && in.Interval == nil {
		return "", errors.New("set_loop: provide enabled and/or interval")
	}
	control, ok := tool.LoopControlFrom(ctx)
	if !ok {
		return "", errors.New("set_loop: no session loop control in this context")
	}
	return control.SetLoop(ctx, in.Enabled, in.Interval)
}
