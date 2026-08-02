package llm

import (
	"context"
	"fmt"
)

const (
	ProviderMock      = "mock"
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type Request struct {
	Model    string
	System   string
	Messages []Message
}

type Chunk struct {
	Delta        string
	FinishReason string
}

// Stream yields chunks that always carry a Delta or a FinishReason;
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
	case ProviderAnthropic:
		return newAnthropic(opts), nil
	default:
		return nil, fmt.Errorf("llm: unknown provider %q", opts.Provider)
	}
}
