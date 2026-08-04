package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

// ExecuteFunc runs a tool with its raw JSON arguments and returns a result
// string that is fed back to the model.
type ExecuteFunc func(ctx context.Context, args json.RawMessage) (string, error)

// Tool pairs the model-facing definition with its implementation.
type Tool struct {
	Definition llm.Tool
	Execute    ExecuteFunc
}

// Executor looks up and runs registered tools. The chat loop depends on this
// interface; the Step 9 permission ruleset will wrap it.
type Executor interface {
	Definitions() []llm.Tool
	Execute(ctx context.Context, name, arguments string) (string, error)
}

// Registry is the default in-memory Executor.
type Registry struct {
	tools map[string]Tool
	order []string
}

var _ Executor = (*Registry)(nil)

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool, rejecting duplicate names so wiring mistakes fail fast.
func (r *Registry) Register(t Tool) error {
	name := t.Definition.Name
	if name == "" {
		return fmt.Errorf("tool: definition is missing a name")
	}
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("tool: duplicate tool %q", name)
	}
	r.tools[name] = t
	r.order = append(r.order, name)
	return nil
}

func (r *Registry) Definitions() []llm.Tool {
	defs := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		defs = append(defs, r.tools[name].Definition)
	}
	return defs
}

func (r *Registry) Execute(ctx context.Context, name, arguments string) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("tool: unknown tool %q", name)
	}
	var args json.RawMessage
	if arguments != "" {
		if !json.Valid([]byte(arguments)) {
			return "", fmt.Errorf("tool: %q received invalid JSON arguments", name)
		}
		args = json.RawMessage(arguments)
	}
	return t.Execute(ctx, args)
}
