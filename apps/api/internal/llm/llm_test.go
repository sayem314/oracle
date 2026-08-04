package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

func collect(t *testing.T, stream llm.Stream) ([]llm.Chunk, string) {
	t.Helper()
	var chunks []llm.Chunk
	var text strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		chunks = append(chunks, chunk)
		text.WriteString(chunk.Delta)
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	return chunks, text.String()
}

func TestNewUnknownProvider(t *testing.T) {
	_, err := llm.New(llm.Options{Provider: "gemini"})
	require.ErrorContains(t, err, `unknown provider "gemini"`)
}

func TestNewProviderSelection(t *testing.T) {
	for _, provider := range []string{llm.ProviderMock, llm.ProviderOpenAI} {
		t.Run(provider, func(t *testing.T) {
			p, err := llm.New(llm.Options{Provider: provider, APIKey: "test", Model: "test-model"})
			require.NoError(t, err)
			assert.NotNil(t, p)
		})
	}
}

func TestMockDefaultReply(t *testing.T) {
	p := llm.NewMock()

	stream, err := p.Chat(context.Background(), llm.Request{})
	require.NoError(t, err)

	chunks, text := collect(t, stream)
	assert.Equal(t, "This is a mock response from oracle.", text)
	assert.Empty(t, chunks[len(chunks)-1].Delta)
	assert.Equal(t, "stop", chunks[len(chunks)-1].FinishReason)
	assert.Greater(t, len(chunks), 2)
}

func TestMockCustomReply(t *testing.T) {
	p := &llm.Mock{Reply: "hello world"}

	stream, err := p.Chat(context.Background(), llm.Request{})
	require.NoError(t, err)

	_, text := collect(t, stream)
	assert.Equal(t, "hello world", text)
}

func TestOpenAIStream(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	var decodeErr error

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		decodeErr = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "text/event-stream")
		lines := []string{
			`data: {"choices":[{"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"content":" world"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		}
		for _, line := range lines {
			_, _ = w.Write([]byte(line + "\n\n"))
		}
	}))
	defer server.Close()

	p, err := llm.New(llm.Options{
		Provider: llm.ProviderOpenAI,
		BaseURL:  server.URL,
		APIKey:   "test-key",
	})
	require.NoError(t, err)

	stream, err := p.Chat(context.Background(), llm.Request{
		Model:  "gpt-test",
		System: "be brief",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, Content: "hello"},
			{Role: llm.RoleUser, Content: "how are you?"},
		},
	})
	require.NoError(t, err)

	chunks, text := collect(t, stream)
	assert.Equal(t, "Hello world", text)
	assert.Equal(t, "stop", chunks[len(chunks)-1].FinishReason)

	assert.Equal(t, "/chat/completions", gotPath)
	assert.Equal(t, "Bearer test-key", gotAuth)
	require.NoError(t, decodeErr)
	assert.Equal(t, "gpt-test", gotBody["model"])

	messages, ok := gotBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 4)
	first := messages[0].(map[string]any)
	assert.Equal(t, "system", first["role"])
	assert.Equal(t, "be brief", first["content"])
	last := messages[3].(map[string]any)
	assert.Equal(t, "user", last["role"])
	assert.Equal(t, "how are you?", last["content"])
}

func TestOpenAIStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()

	p, err := llm.New(llm.Options{Provider: llm.ProviderOpenAI, BaseURL: server.URL, APIKey: "test-key"})
	require.NoError(t, err)

	stream, err := p.Chat(context.Background(), llm.Request{Model: "gpt-test"})
	require.NoError(t, err)

	assert.False(t, stream.Next())
	require.Error(t, stream.Err())
	_ = stream.Close()
}

func TestOpenAIUnsupportedRole(t *testing.T) {
	p, err := llm.New(llm.Options{Provider: llm.ProviderOpenAI, APIKey: "test-key"})
	require.NoError(t, err)

	_, err = p.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.Role("function"), Content: "x"}},
	})
	require.ErrorContains(t, err, `unsupported role "function"`)
}

func TestOpenAIToolMessageRequiresID(t *testing.T) {
	p, err := llm.New(llm.Options{Provider: llm.ProviderOpenAI, APIKey: "test-key"})
	require.NoError(t, err)

	_, err = p.Chat(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleTool, Content: "result"}},
	})
	require.ErrorContains(t, err, "missing tool_call_id")
}

func TestMockToolCalls(t *testing.T) {
	calls := []llm.ToolCall{{ID: "call_1", Name: "get_time", Arguments: "{}"}}
	p := &llm.Mock{ToolCalls: calls}

	stream, err := p.Chat(context.Background(), llm.Request{})
	require.NoError(t, err)

	chunks, text := collect(t, stream)
	assert.Empty(t, text)
	require.Len(t, chunks, 1)
	assert.Equal(t, "tool_calls", chunks[0].FinishReason)
	assert.Equal(t, calls, chunks[0].ToolCalls)
}

func TestOpenAISendsTools(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	p, err := llm.New(llm.Options{Provider: llm.ProviderOpenAI, BaseURL: server.URL, APIKey: "test-key"})
	require.NoError(t, err)

	stream, err := p.Chat(context.Background(), llm.Request{
		Model: "gpt-test",
		Tools: []llm.Tool{
			{
				Name:        "get_time",
				Description: "Return the current time.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
		},
	})
	require.NoError(t, err)
	collect(t, stream)

	tools, ok := gotBody["tools"].([]any)
	require.True(t, ok, "expected tools in request body")
	require.Len(t, tools, 1)
	first := tools[0].(map[string]any)
	assert.Equal(t, "function", first["type"])
	fn := first["function"].(map[string]any)
	assert.Equal(t, "get_time", fn["name"])
	assert.Equal(t, "Return the current time.", fn["description"])
	assert.NotNil(t, fn["parameters"])
}

func TestOpenAIToolCallStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		lines := []string{
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"get_time","arguments":""}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"echo","arguments":""}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\":"}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"b\":"}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"2}"}}]}}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		}
		for _, line := range lines {
			_, _ = w.Write([]byte(line + "\n\n"))
		}
	}))
	defer server.Close()

	p, err := llm.New(llm.Options{Provider: llm.ProviderOpenAI, BaseURL: server.URL, APIKey: "test-key"})
	require.NoError(t, err)

	stream, err := p.Chat(context.Background(), llm.Request{Model: "gpt-test"})
	require.NoError(t, err)

	chunks, text := collect(t, stream)
	assert.Empty(t, text)
	require.Len(t, chunks, 1)
	assert.Equal(t, "tool_calls", chunks[0].FinishReason)
	require.Len(t, chunks[0].ToolCalls, 2)
	assert.Equal(t, llm.ToolCall{ID: "call_a", Name: "get_time", Arguments: `{"a":1}`}, chunks[0].ToolCalls[0])
	assert.Equal(t, llm.ToolCall{ID: "call_b", Name: "echo", Arguments: `{"b":2}`}, chunks[0].ToolCalls[1])
}

func TestOpenAIInvalidToolSchema(t *testing.T) {
	p, err := llm.New(llm.Options{Provider: llm.ProviderOpenAI, APIKey: "test-key"})
	require.NoError(t, err)

	_, err = p.Chat(context.Background(), llm.Request{
		Tools: []llm.Tool{{Name: "bad", Parameters: json.RawMessage(`{not json`)}},
	})
	require.ErrorContains(t, err, "invalid parameters schema")
}

func TestOpenAIToolHistoryRoundTrip(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	p, err := llm.New(llm.Options{Provider: llm.ProviderOpenAI, BaseURL: server.URL, APIKey: "test-key"})
	require.NoError(t, err)

	stream, err := p.Chat(context.Background(), llm.Request{
		Model: "gpt-test",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "what time is it?"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "get_time", Arguments: "{}"}}},
			{Role: llm.RoleTool, Content: "2026-08-04T00:00:00Z", ToolCallID: "call_1"},
		},
	})
	require.NoError(t, err)
	collect(t, stream)

	messages, ok := gotBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 3)

	asst := messages[1].(map[string]any)
	assert.Equal(t, "assistant", asst["role"])
	asstCalls, ok := asst["tool_calls"].([]any)
	require.True(t, ok)
	require.Len(t, asstCalls, 1)
	call := asstCalls[0].(map[string]any)
	assert.Equal(t, "call_1", call["id"])
	fn := call["function"].(map[string]any)
	assert.Equal(t, "get_time", fn["name"])
	assert.Equal(t, "{}", fn["arguments"])

	toolMsg := messages[2].(map[string]any)
	assert.Equal(t, "tool", toolMsg["role"])
	assert.Equal(t, "call_1", toolMsg["tool_call_id"])
	assert.Equal(t, "2026-08-04T00:00:00Z", toolMsg["content"])
}
