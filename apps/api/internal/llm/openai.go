package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
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
	tools, err := toOpenAITools(req.Tools)
	if err != nil {
		return nil, err
	}
	stream := p.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:    model,
		Messages: messages,
		Tools:    tools,
	})
	return &openAIStream{raw: stream}, nil
}

func toOpenAITools(tools []Tool) ([]openai.ChatCompletionToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		def := shared.FunctionDefinitionParam{Name: t.Name}
		if t.Description != "" {
			def.Description = param.NewOpt(t.Description)
		}
		if len(t.Parameters) > 0 {
			var schema map[string]any
			if err := json.Unmarshal(t.Parameters, &schema); err != nil {
				return nil, fmt.Errorf("llm: tool %q has an invalid parameters schema: %w", t.Name, err)
			}
			def.Parameters = schema
		}
		out = append(out, openai.ChatCompletionFunctionTool(def))
	}
	return out, nil
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
			messages = append(messages, assistantMessage(m))
		case RoleTool:
			if m.ToolCallID == "" {
				return nil, fmt.Errorf("llm: tool message is missing tool_call_id")
			}
			messages = append(messages, openai.ToolMessage(m.Content, m.ToolCallID))
		default:
			return nil, fmt.Errorf("llm: unsupported role %q", m.Role)
		}
	}
	return messages, nil
}

func assistantMessage(m Message) openai.ChatCompletionMessageParamUnion {
	asst := openai.ChatCompletionAssistantMessageParam{}
	if m.Content != "" {
		asst.Content.OfString = param.NewOpt(m.Content)
	}
	for _, call := range m.ToolCalls {
		asst.ToolCalls = append(asst.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: call.ID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			},
		})
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &asst}
}

type toolCallAcc struct {
	id   string
	name string
	args strings.Builder
}

type openAIStream struct {
	raw     rawOpenAIStream
	current Chunk
	accs    map[int64]*toolCallAcc
}

func (s *openAIStream) Next() bool {
	for s.raw.Next() {
		chunk := s.raw.Current()
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		for _, tc := range choice.Delta.ToolCalls {
			s.accumulate(tc)
		}
		s.current = Chunk{Delta: choice.Delta.Content, FinishReason: choice.FinishReason}
		if s.current.FinishReason != "" {
			s.current.ToolCalls = s.finalizeCalls()
			return true
		}
		if s.current.Delta != "" {
			return true
		}
	}
	return false
}

func (s *openAIStream) accumulate(tc openai.ChatCompletionChunkChoiceDeltaToolCall) {
	if s.accs == nil {
		s.accs = make(map[int64]*toolCallAcc)
	}
	acc, ok := s.accs[tc.Index]
	if !ok {
		acc = &toolCallAcc{}
		s.accs[tc.Index] = acc
	}
	if tc.ID != "" {
		acc.id = tc.ID
	}
	if tc.Function.Name != "" {
		acc.name = tc.Function.Name
	}
	acc.args.WriteString(tc.Function.Arguments)
}

func (s *openAIStream) finalizeCalls() []ToolCall {
	if len(s.accs) == 0 {
		return nil
	}
	idx := make([]int64, 0, len(s.accs))
	for i := range s.accs {
		idx = append(idx, i)
	}
	sort.Slice(idx, func(a, b int) bool { return idx[a] < idx[b] })
	calls := make([]ToolCall, 0, len(idx))
	for _, i := range idx {
		acc := s.accs[i]
		calls = append(calls, ToolCall{ID: acc.id, Name: acc.name, Arguments: acc.args.String()})
	}
	s.accs = nil
	return calls
}

func (s *openAIStream) Current() Chunk { return s.current }

func (s *openAIStream) Err() error { return s.raw.Err() }

func (s *openAIStream) Close() error { return s.raw.Close() }
