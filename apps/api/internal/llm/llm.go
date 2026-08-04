package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	ProviderMock   = "mock"
	ProviderOpenAI = "openai"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Tool is a function the model may choose to invoke.
type Tool struct {
	Name        string
	Description string
	// Parameters is a JSON Schema describing the tool's arguments.
	Parameters json.RawMessage
}

// ToolCall is the model's request to invoke a tool. Arguments is the raw JSON
// the model produced, validated by the executor before dispatch.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Message struct {
	Role    Role
	Content string
	// ToolCalls is set on assistant messages that request tool invocations.
	ToolCalls []ToolCall
	// ToolCallID is set on tool messages and names the call they answer.
	ToolCallID string
}

type Request struct {
	Model    string
	System   string
	Messages []Message
	Tools    []Tool
}

type Chunk struct {
	Delta        string
	FinishReason string
	// ToolCalls carries the complete tool calls once the model finishes a turn.
	ToolCalls []ToolCall
}

// Stream yields chunks that always carry a Delta, a FinishReason, or ToolCalls;
// empty protocol events are skipped by implementations.
type Stream interface {
	Next() bool
	Current() Chunk
	Err() error
	Close() error
}

type Provider interface {
	Chat(ctx context.Context, req Request) (Stream, error)
}

type Options struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
}

func New(opts Options) (Provider, error) {
	switch opts.Provider {
	case ProviderMock:
		return NewMock(), nil
	case ProviderOpenAI:
		return newOpenAI(opts), nil
	default:
		return nil, fmt.Errorf("llm: unknown provider %q", opts.Provider)
	}
}
