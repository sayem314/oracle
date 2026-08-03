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
		Messages: []llm.Message{{Role: llm.Role("tool"), Content: "x"}},
	})
	require.ErrorContains(t, err, `unsupported role "tool"`)
}
