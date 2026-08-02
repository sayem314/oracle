package llm

import (
	"context"
	"strings"
)

const defaultMockReply = "This is a mock response from oracle."

type Mock struct {
	Reply string
}

func NewMock() *Mock { return &Mock{} }

func (m *Mock) Chat(_ context.Context, _ Request) (Stream, error) {
	reply := m.Reply
	if reply == "" {
		reply = defaultMockReply
	}
	return &mockStream{chunks: mockChunks(reply)}, nil
}

func mockChunks(reply string) []Chunk {
	words := strings.Fields(reply)
	chunks := make([]Chunk, 0, len(words)+1)
	for i, word := range words {
		if i < len(words)-1 {
			word += " "
		}
		chunks = append(chunks, Chunk{Delta: word})
	}
	return append(chunks, Chunk{FinishReason: "stop"})
}

type mockStream struct {
	chunks []Chunk
	index  int
}

func (s *mockStream) Next() bool {
	if s.index >= len(s.chunks) {
		return false
	}
	s.index++
	return true
}

func (s *mockStream) Current() Chunk { return s.chunks[s.index-1] }

func (s *mockStream) Err() error { return nil }

func (s *mockStream) Close() error { return nil }
