package net

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

const (
	maxSearchBytes = 512 << 10
	maxSearchHits  = 5
)

// browserUA avoids DDG's bot wall, which blocks the default Go user agent.
const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36 oracle"

// defaultSearchEndpoint is the keyless DuckDuckGo HTML search form.
var defaultSearchEndpoint = "https://html.duckduckgo.com/html/"

type searchHit struct {
	title   string
	url     string
	snippet string
}

func webSearchTool(client *http.Client) tool.Tool {
	return webSearchAt(client, defaultSearchEndpoint)
}

func webSearchAt(client *http.Client, endpoint string) tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name:        "web_search",
			Description: "Search the web (DuckDuckGo) for a query and return the top result titles, URLs, and snippets.",
			Parameters:  schema,
		},
		Execute: webSearchExecute(client, endpoint),
	}
}

func webSearchExecute(client *http.Client, endpoint string) tool.ExecuteFunc {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("web_search: %w", err)
		}
		if strings.TrimSpace(in.Query) == "" {
			return "", errors.New("web_search: query is required")
		}

		// Seed the endpoint first: DDG issues an ax cookie on GET that the
		// POST originates from.
		seed, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", fmt.Errorf("web_search: %w", err)
		}
		seed.Header.Set("User-Agent", browserUA)
		seedResp, err := client.Do(seed)
		if err != nil {
			return "", fmt.Errorf("web_search: %w", err)
		}
		defer seedResp.Body.Close() //nolint:errcheck
		_, _ = io.Copy(io.Discard, io.LimitReader(seedResp.Body, 1<<10))

		form := url.Values{"q": {in.Query}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return "", fmt.Errorf("web_search: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", browserUA)
		req.Header.Set("Referer", endpoint)

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("web_search: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchBytes))
		if err != nil {
			return "", fmt.Errorf("web_search: read body: %w", err)
		}

		hits, err := parseSearchHits(string(body))
		if err != nil {
			return "", fmt.Errorf("web_search: parse results: %w", err)
		}
		if len(hits) == 0 {
			return "No results found.", nil
		}
		if len(hits) > maxSearchHits {
			hits = hits[:maxSearchHits]
		}

		var sb strings.Builder
		for i, h := range hits {
			fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n", i+1, h.title, h.url, h.snippet)
		}
		return sb.String(), nil
	}
}

// parseSearchHits extracts DuckDuckGo html-endpoint results: an anchor with
// class result__a carries the title and a relay URL (uddg holds the real
// target); result__snippet carries the excerpt. Samples:
//
//	<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=...">Title</a>
//	<a class="result__snippet" href="...">snippet</a>
func parseSearchHits(page string) ([]searchHit, error) {
	tok := html.NewTokenizer(strings.NewReader(page))
	var hits []searchHit
	var cur *searchHit
	onTitle := false
	onSnippet := false

	for {
		switch tok.Next() {
		case html.ErrorToken:
			if tok.Err() == io.EOF {
				return hits, nil
			}
			return nil, tok.Err()
		case html.StartTagToken:
			name, _ := tok.TagName()
			if string(name) != "a" {
				continue
			}
			var cls, href string
			for {
				k, v, more := tok.TagAttr()
				if string(k) == "class" {
					cls = string(v)
				}
				if string(k) == "href" {
					href = string(v)
				}
				if !more {
					break
				}
			}
			switch {
			case strings.Contains(cls, "result__a"):
				hits = append(hits, searchHit{url: unwrapSearchURL(href)})
				cur = &hits[len(hits)-1]
				onTitle, onSnippet = true, false
			case strings.Contains(cls, "result__snippet"):
				onTitle = false
				onSnippet = cur != nil
			default:
				onTitle, onSnippet = false, false
			}
		case html.EndTagToken:
			name, _ := tok.TagName()
			if string(name) == "a" {
				onTitle, onSnippet = false, false
			}
		case html.TextToken:
			text := strings.TrimSpace(string(tok.Text()))
			if text == "" {
				continue
			}
			if cur == nil {
				continue
			}
			if onTitle {
				cur.title = text
				onTitle = false
			} else if onSnippet {
				cur.snippet = text
				onSnippet = false
			}
		}
	}
}

// unwrapSearchURL decodes DDG's base64 relay URL, falling back to the raw
// href when the record is not a relay.
func unwrapSearchURL(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	raw := u.Query().Get("uddg")
	if raw == "" {
		return href
	}
	// DDG emits raw URL base64, sometimes with trailing = padding.
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(raw, "="))
	if err != nil {
		return href
	}
	return string(decoded)
}
