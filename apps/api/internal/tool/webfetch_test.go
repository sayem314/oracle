package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainClient dials anything; injected so happy-path tests can hit httptest
// servers on loopback, which the production ssrfClient refuses on purpose.
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

func TestIPBlocked(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"169.254.169.254", true},
		{"100.64.0.1", true},
		{"0.0.0.0", true},
		{"224.0.0.1", true},
		{"8.8.8.8", false},
		{"93.184.216.34", false},
		{"2001:db8::1", true},
		{"::1", true},
		{"fe80::1", true},
		{"2606:4700:4700::1111", false},
	}
	for _, tc := range cases {
		addr, err := netip.ParseAddr(tc.ip)
		require.NoError(t, err, "parse %s", tc.ip)
		assert.Equal(t, tc.want, ipBlocked(addr), "%s", tc.ip)
	}
}

func TestWebFetchSSRFBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("should never be reached"))
	}))
	defer srv.Close()

	tl := webFetchTool(ssrfClient())
	_, err := tl.Execute(context.Background(), mustArgs(`{"url":"`+srv.URL+`"}`))
	require.Error(t, err)
	assert.ErrorContains(t, err, "private or reserved")
}

func mustArgs(s string) json.RawMessage {
	return json.RawMessage(s)
}
