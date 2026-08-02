package llm

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const defaultAnthropicMaxTokens = 4096

type anthropicProvider struct {
	client anthropic.Client
	model  string
}

type rawAnthropicStream interface {
	Next() bool
	Current() anthropic.MessageStreamEventUnion
	Err() error
	Close() error
}

func newAnthropic(opts Options) *anthropicProvider {
	reqOpts := []option.RequestOption{option.WithAPIKey(opts.APIKey)}
	if opts.BaseURL != "" {
		reqOpts = append(reqOpts, option.WithBaseURL(opts.BaseURL))
	}
	return &anthropicProvider{
		client: anthropic.NewClient(reqOpts...),
		model:  opts.Model,
	}
}

func (p *anthropicProvider) Chat(ctx context.Context, req Request) (Stream, error) {
	messages, err := toAnthropicMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	params := anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: defaultAnthropicMaxTokens,
		Messages:  messages,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	stream := p.client.Messages.NewStreaming(ctx, params)
	return &anthropicStream{raw: stream}, nil
}

func toAnthropicMessages(messages []Message) ([]anthropic.MessageParam, error) {
	params := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case RoleUser:
			params = append(params, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case RoleAssistant:
			params = append(params, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		default:
			return nil, fmt.Errorf("llm: unsupported role %q", m.Role)
		}
	}
	return params, nil
}

type anthropicStream struct {
	raw     rawAnthropicStream
	current Chunk
}

func (s *anthropicStream) Next() bool {
	for s.raw.Next() {
		s.current = translateAnthropicEvent(s.raw.Current())
		if s.current.Delta != "" || s.current.FinishReason != "" {
			return true
		}
	}
	return false
}

func (s *anthropicStream) Current() Chunk { return s.current }

func translateAnthropicEvent(event anthropic.MessageStreamEventUnion) Chunk {
	switch event.Type {
	case "content_block_delta":
		delta := event.AsContentBlockDelta().Delta
		if delta.Type == "text_delta" {
			return Chunk{Delta: delta.Text}
		}
	case "message_delta":
		return Chunk{FinishReason: string(event.AsMessageDelta().Delta.StopReason)}
	}
	return Chunk{}
}

func (s *anthropicStream) Err() error { return s.raw.Err() }

func (s *anthropicStream) Close() error { return s.raw.Close() }
