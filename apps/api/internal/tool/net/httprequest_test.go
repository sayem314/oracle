package net

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPRequestRegistered(t *testing.T) {
	found := false
	for _, tl := range New() {
		if tl.Definition.Name == "http_request" {
			found = true
		}
	}
	assert.True(t, found, "http_request should be part of the net group")
}

func TestHTTPRequestGet(t *testing.T) {
	var gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	out, err := httpRequestExecute(plainClient())(
		context.Background(), mustArgs(`{"method":"GET","url":"`+srv.URL+`"}`))
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Empty(t, gotBody)
	assert.Contains(t, out, "GET")
	assert.Contains(t, out, "status: 200")
	assert.Contains(t, out, "hello")
}

func TestHTTPRequestPostBody(t *testing.T) {
	var gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	out, err := httpRequestExecute(plainClient())(
		context.Background(), mustArgs(`{"method":"POST","url":"`+srv.URL+`","body":"{\"name\":\"x\"}"}`))
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.JSONEq(t, `{"name":"x"}`, gotBody)
	assert.Contains(t, out, "status: 201")
	assert.Contains(t, out, `{"id":1}`)
}

func TestHTTPRequestPutPatchDelete(t *testing.T) {
	for _, method := range []string{"PUT", "PATCH", "DELETE"} {
		var gotMethod string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			_, _ = w.Write([]byte("ok"))
		}))
		out, err := httpRequestExecute(plainClient())(context.Background(),
			mustArgs(`{"method":"`+method+`","url":"`+srv.URL+`"}`))
		require.NoError(t, err)
		assert.Equal(t, method, gotMethod, "method %s", method)
		assert.Contains(t, out, method)
		srv.Close()
	}
}

func TestHTTPRequestCustomHeaders(t *testing.T) {
	var gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	_, err := httpRequestExecute(plainClient())(context.Background(),
		mustArgs(`{"method":"POST","url":"`+srv.URL+`","headers":{"Authorization":"Bearer abc","Content-Type":"application/json"},"body":"{}"}`))
	require.NoError(t, err)

	assert.Equal(t, "Bearer abc", gotAuth)
	assert.Equal(t, "application/json", gotCT)
}

func TestHTTPRequestNon2xxReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	out, err := httpRequestExecute(plainClient())(context.Background(),
		mustArgs(`{"method":"GET","url":"`+srv.URL+`"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "status: 404")
	assert.Contains(t, out, "not found")
}

func TestHTTPRequestOversizeTruncated(t *testing.T) {
	big := strings.Repeat("a", 600*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	out, err := httpRequestExecute(plainClient())(context.Background(),
		mustArgs(`{"method":"GET","url":"`+srv.URL+`"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "truncated: true")
}

func TestHTTPRequestUnsupportedMethod(t *testing.T) {
	_, err := httpRequestExecute(plainClient())(context.Background(),
		mustArgs(`{"method":"OPTIONS","url":"http://example.com"}`))
	require.ErrorContains(t, err, `unsupported method "OPTIONS"`)
}

func TestHTTPRequestInvalidScheme(t *testing.T) {
	_, err := httpRequestExecute(plainClient())(context.Background(),
		mustArgs(`{"method":"GET","url":"file:///etc/passwd"}`))
	require.ErrorContains(t, err, `scheme "file"`)
}

func TestHTTPRequestMissingHost(t *testing.T) {
	_, err := httpRequestExecute(plainClient())(context.Background(),
		mustArgs(`{"method":"GET","url":"http://"}`))
	require.ErrorContains(t, err, "missing a host")
}

func TestHTTPRequestNetworkError(t *testing.T) {
	_, err := httpRequestExecute(plainClient())(context.Background(),
		mustArgs(`{"method":"GET","url":"http://127.0.0.1:1"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http_request")
}
