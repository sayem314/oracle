package llm

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type openAIProvider struct {
	client openai.Client
	model  string
}

type rawOpenAIStream interface {
	Next() bool
	Current() openai.ChatCompletionChunk
	Err() error
	Close() error
}

func newOpenAI(opts Options) *openAIProvider {
	reqOpts := []option.RequestOption{option.WithAPIKey(opts.APIKey)}
	if opts.BaseURL != "" {
		reqOpts = append(reqOpts, option.WithBaseURL(opts.BaseURL))
	}
	return &openAIProvider{
		client: openai.NewClient(reqOpts...),
		model:  opts.Model,
	}
}

func (p *openAIProvider) Chat(ctx context.Context, req Request) (Stream, error) {
	messages, err := toOpenAIMessages(req)
	if err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	stream := p.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:    model,
		Messages: messages,
	})
	return &openAIStream{raw: stream}, nil
}

func toOpenAIMessages(req Request) ([]openai.ChatCompletionMessageParamUnion, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openai.SystemMessage(req.System))
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			messages = append(messages, openai.UserMessage(m.Content))
		case RoleAssistant:
			messages = append(messages, openai.AssistantMessage(m.Content))
		default:
			return nil, fmt.Errorf("llm: unsupported role %q", m.Role)
		}
	}
	return messages, nil
}

type openAIStream struct {
	raw     rawOpenAIStream
	current Chunk
}

func (s *openAIStream) Next() bool {
	for s.raw.Next() {
		chunk := s.raw.Current()
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		s.current = Chunk{Delta: choice.Delta.Content, FinishReason: choice.FinishReason}
		if s.current.Delta != "" || s.current.FinishReason != "" {
			return true
		}
	}
	return false
}

func (s *openAIStream) Current() Chunk { return s.current }

func (s *openAIStream) Err() error { return s.raw.Err() }

func (s *openAIStream) Close() error { return s.raw.Close() }
