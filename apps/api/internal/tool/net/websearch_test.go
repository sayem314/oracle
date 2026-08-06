package net

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const searchPage = `<!DOCTYPE html>
<html>
<body>
<div class="result results_links_deep web-result">
  <div class="result__body">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=aHR0cHM6Ly9leGFtcGxlLmNvbS8=&rut=ab">Example Domain</a>
    </h2>
    <a class="result__snippet" href="//duckduckgo.com/l/?uddg=...">An example domain for illustrations.</a>
  </div>
</div>
<div class="result results_links_deep web-result">
  <div class="result__body">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=aHR0cHM6Ly93d3cuZXhhbXBsZS5jb20v&rut=cd">Example WWW</a>
    </h2>
    <a class="result__snippet" href="//duckduckgo.com/l/?uddg=...">Second result snippet.</a>
  </div>
</div>
</body>
</html>`

func searchHTTPServer(page string) (*httptest.Server, *atomic.Bool) {
	var seeded atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			seeded.Store(true)
			http.SetCookie(w, &http.Cookie{Name: "ax", Value: "seed", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			_, _ = w.Write([]byte(page))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	return srv, &seeded
}

func TestWebSearchRegistered(t *testing.T) {
	found := false
	for _, tl := range New() {
		if tl.Definition.Name == "web_search" {
			found = true
		}
	}
	assert.True(t, found, "web_search should be part of the net group")
}

func TestWebSearchHits(t *testing.T) {
	srv, seeded := searchHTTPServer(searchPage)
	defer srv.Close()

	tl := webSearchAt(plainClient(), srv.URL)
	out, err := tl.Execute(context.Background(), mustArgs(`{"query":"example"}`))
	require.NoError(t, err)
	assert.True(t, seeded.Load(), "seed GET should be issued before the POST")
	assert.Contains(t, out, "Example Domain")
	assert.Contains(t, out, "https://example.com/")
	assert.Contains(t, out, "An example domain for illustrations.")
	assert.Contains(t, out, "Example WWW")
	assert.NotContains(t, out, "duckduckgo.com/l")
}

func TestWebSearchCapsResults(t *testing.T) {
	var results strings.Builder
	for i := range 8 {
		results.WriteString(`<div class="result"><h2 class="result__title"><a rel="nofollow" class="result__a" href="//ddg/l/?uddg=aHR0cHM6Ly9leGFtcGxlLmNvbS8=">Result `)
		results.WriteString(string(rune('A' + i)))
		results.WriteString(`</a></h2><a class="result__snippet">snippet</a></div>`)
	}
	srv, _ := searchHTTPServer(results.String())
	defer srv.Close()

	tl := webSearchAt(plainClient(), srv.URL)
	out, err := tl.Execute(context.Background(), mustArgs(`{"query":"x"}`))
	require.NoError(t, err)
	assert.Len(t, strings.Split(out, "\n"), (maxSearchHits*3)+1)
}

func TestWebSearchNoResults(t *testing.T) {
	srv, _ := searchHTTPServer(`<html><body>nothing here</body></html>`)
	defer srv.Close()

	tl := webSearchAt(plainClient(), srv.URL)
	out, err := tl.Execute(context.Background(), mustArgs(`{"query":"zzz"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "No results")
}

func TestWebSearchEmptyQuery(t *testing.T) {
	tl := webSearchAt(plainClient(), "http://x")
	_, err := tl.Execute(context.Background(), mustArgs(`{"query":"  "}`))
	assert.ErrorContains(t, err, "query is required")
}

func TestWebSearchMissingQuery(t *testing.T) {
	tl := webSearchAt(plainClient(), "http://x")
	_, err := tl.Execute(context.Background(), mustArgs(`{}`))
	assert.ErrorContains(t, err, "query is required")
}

func TestWebSearchInvalidArgs(t *testing.T) {
	tl := webSearchAt(plainClient(), "http://x")
	_, err := tl.Execute(context.Background(), json.RawMessage(`not json`))
	require.Error(t, err)
}
