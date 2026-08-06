package net

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

var allowedMethods = map[string]bool{
	http.MethodGet:    true,
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

func httpRequestTool(client *http.Client) tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"method":{"type":"string","enum":["GET","POST","PUT","PATCH","DELETE"]},"url":{"type":"string"},"headers":{"type":"object","additionalProperties":{"type":"string"}},"body":{"type":"string"}},"required":["method","url"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "http_request",
			Description: "Issue an HTTP request and return the status and response body. " +
				"method is GET, POST, PUT, PATCH, or DELETE. url must be http or https. " +
				"headers is an optional object of string headers and body is an optional " +
				"raw string. This can reach any host and mutate state, so it is gated by " +
				"the permission ruleset.",
			Parameters: schema,
		},
		Execute: httpRequestExecute(client),
	}
}

func httpRequestExecute(client *http.Client) tool.ExecuteFunc {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("http_request: %w", err)
		}
		method := strings.ToUpper(strings.TrimSpace(in.Method))
		if !allowedMethods[method] {
			return "", fmt.Errorf("http_request: unsupported method %q", in.Method)
		}
		u, err := url.Parse(in.URL)
		if err != nil {
			return "", fmt.Errorf("http_request: invalid url: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("http_request: scheme %q is not allowed", u.Scheme)
		}
		if u.Host == "" {
			return "", errors.New("http_request: url is missing a host")
		}

		var body io.Reader
		if in.Body != "" {
			body = strings.NewReader(in.Body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
		if err != nil {
			return "", fmt.Errorf("http_request: %w", err)
		}
		req.Header.Set("User-Agent", "oracle/1.0")
		req.Header.Set("Accept", "*/*")
		for k, v := range in.Headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("http_request: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
		if err != nil {
			return "", fmt.Errorf("http_request: read body: %w", err)
		}
		truncated := len(bodyBytes) > maxFetchBytes
		if truncated {
			bodyBytes = bodyBytes[:maxFetchBytes]
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "%s %s\n", method, resp.Status)
		fmt.Fprintf(&sb, "status: %d\n", resp.StatusCode)
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			fmt.Fprintf(&sb, "content-type: %s\n", ct)
		}
		if truncated {
			sb.WriteString("truncated: true\n")
		}
		sb.WriteString("\n")
		sb.WriteString(string(bodyBytes))
		return sb.String(), nil
	}
}
