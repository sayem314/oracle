package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainClient dials anything; injected so tests can hit httptest servers on
// loopback without relying on the shared production httpClient.
func plainClient() *http.Client { return http.DefaultClient }

func TestWebFetchRegistered(t *testing.T) {
	found := false
	for _, tl := range NewBuiltin() {
		if tl.Definition.Name == "web_fetch" {
			found = true
		}
	}
	assert.True(t, found, "web_fetch should be part of NewBuiltin")
}

func TestWebFetchHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>Oracle Doc</title><script>alert("x")</script></head><body><h1>Hello world</h1><p>Some body text.</p></body></html>`))
	}))
	defer srv.Close()

	tl := webFetchTool(plainClient())
	out, err := tl.Execute(context.Background(), mustArgs(`{"url":"`+srv.URL+`"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "Hello world")
	assert.Contains(t, out, "Some body text.")
	assert.NotContains(t, out, "alert")
}

func TestWebFetchTextPlain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("plain body"))
	}))
	defer srv.Close()

	tl := webFetchTool(plainClient())
	out, err := tl.Execute(context.Background(), mustArgs(`{"url":"`+srv.URL+`"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "plain body")
}

func TestWebFetchSizeLimit(t *testing.T) {
	big := strings.Repeat("a", 600*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	tl := webFetchTool(plainClient())
	out, err := tl.Execute(context.Background(), mustArgs(`{"url":"`+srv.URL+`"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "truncated: true")
}

func TestWebFetchHTTPErrorStatusKept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	tl := webFetchTool(plainClient())
	out, err := tl.Execute(context.Background(), mustArgs(`{"url":"`+srv.URL+`"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "status: 404")
}

func TestWebFetchUnsupportedScheme(t *testing.T) {
	tl := webFetchTool(plainClient())
	_, err := tl.Execute(context.Background(), mustArgs(`{"url":"file:///etc/passwd"}`))
	assert.ErrorContains(t, err, `scheme "file"`)
}

func TestWebFetchMissingHost(t *testing.T) {
	tl := webFetchTool(plainClient())
	_, err := tl.Execute(context.Background(), mustArgs(`{"url":"http://"}`))
	assert.ErrorContains(t, err, "missing a host")
}

func TestWebFetchNonsensicalURL(t *testing.T) {
	tl := webFetchTool(plainClient())
	_, err := tl.Execute(context.Background(), mustArgs(`{"url":"%%%"}`))
	assert.Error(t, err)
}

func TestWebFetchInvalidArgs(t *testing.T) {
	tl := webFetchTool(plainClient())
	_, err := tl.Execute(context.Background(), json.RawMessage(`not json`))
	require.Error(t, err)
}

func mustArgs(s string) json.RawMessage {
	return json.RawMessage(s)
}
