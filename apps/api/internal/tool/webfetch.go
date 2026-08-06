package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

const (
	maxFetchBytes = 512 << 10
	fetchTimeout  = 15 * time.Second
)

// blockedNets lists special-purpose ranges a fetch must never dial (SSRF
// guard). Private/loopback/link-local/metadata/benchmark/doc ranges.
var blockedNets = func() []netip.Prefix {
	raw := []string{
		// IPv4 special-purpose ranges (RFC 5735, 1918, 6598, 6890).
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
		// IPv6 special-purpose ranges (RFC 4291, 3849, 5180).
		"::/128",
		"::1/128",
		"64:ff9b::/96",
		"100::/64",
		"2001:db8::/32",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
	}
	out := make([]netip.Prefix, 0, len(raw))
	for _, s := range raw {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func ipBlocked(ip netip.Addr) bool {
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	for _, p := range blockedNets {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// ssrfDialContext resolves names itself and refuses private targets, then
// dials the validated IP directly so the check cannot be re-pointed at a
// sneaky address between resolution and connect.
func ssrfDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	var lastErr error
	for _, ip := range ips {
		a, ok := netip.AddrFromSlice(ip.IP)
		if !ok {
			continue
		}
		if ipBlocked(a) {
			lastErr = fmt.Errorf("target %s is in a private or reserved range", a)
			continue
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(a.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no usable addresses")
	}
	return nil, lastErr
}

// ssrfClient is the transport web_fetch and web_search use in production.
// Proxying is disabled so the caller's network cannot be used to hop around
// the guard. The cookie jar persists DDG's ax session cookie between the
// seed GET and the search POST.
func ssrfClient() *http.Client {
	return &http.Client{
		Timeout: fetchTimeout,
		Transport: &http.Transport{
			DialContext: ssrfDialContext,
			Proxy:       nil,
		},
		Jar: func() http.CookieJar {
			jar, _ := cookiejar.New(nil)
			return jar
		}(),
	}
}

func webFetchTool(client *http.Client) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`)
	return Tool{
		Definition: llm.Tool{
			Name: "web_fetch",
			Description: "Fetch a URL over HTTP or HTTPS and return its content as text. " +
				"HTML pages have tags stripped. Private, loopback, and reserved addresses are refused.",
			Parameters: schema,
		},
		Execute: webFetchExecute(client),
	}
}

func webFetchExecute(client *http.Client) ExecuteFunc {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("web_fetch: %w", err)
		}
		u, err := url.Parse(in.URL)
		if err != nil {
			return "", fmt.Errorf("web_fetch: invalid url: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("web_fetch: scheme %q is not allowed", u.Scheme)
		}
		if u.Host == "" {
			return "", errors.New("web_fetch: url is missing a host")
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return "", fmt.Errorf("web_fetch: %w", err)
		}
		req.Header.Set("User-Agent", "oracle/1.0")
		req.Header.Set("Accept", "text/html,text/plain,application/json,text/markdown")

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("web_fetch: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
		if err != nil {
			return "", fmt.Errorf("web_fetch: read body: %w", err)
		}
		truncated := len(body) > maxFetchBytes
		if truncated {
			body = body[:maxFetchBytes]
		}

		media := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
		var content string
		switch {
		case media == "text/html" || strings.EqualFold(media, "application/xhtml+xml"):
			text, err := htmlToText(bytes.NewReader(body))
			if err != nil {
				return "", fmt.Errorf("web_fetch: parse html: %w", err)
			}
			content = strings.Join(strings.Fields(text), " ")
		case strings.HasPrefix(media, "text/") || media == "application/json" || media == "application/xml":
			content = string(body)
		default:
			return "", fmt.Errorf("web_fetch: unsupported content type %q", media)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "status: %d\n", resp.StatusCode)
		fmt.Fprintf(&sb, "content-type: %s\n", media)
		if truncated {
			sb.WriteString("truncated: true\n")
		}
		sb.WriteString("\n")
		sb.WriteString(content)
		return sb.String(), nil
	}
}

// htmlToText walks the token stream, keeping visible text and dropping
// script/style/noscript blocks.
func htmlToText(r io.Reader) (string, error) {
	tok := html.NewTokenizer(r)
	var sb strings.Builder
	skip := 0
	for {
		switch tok.Next() {
		case html.ErrorToken:
			if errors.Is(tok.Err(), io.EOF) {
				return sb.String(), nil
			}
			return sb.String(), tok.Err()
		case html.StartTagToken:
			name, _ := tok.TagName()
			if isHtmlHidden(name) {
				skip++
			}
		case html.EndTagToken:
			name, _ := tok.TagName()
			if isHtmlHidden(name) && skip > 0 {
				skip--
			}
		case html.TextToken:
			if skip == 0 {
				sb.Write(tok.Text())
				sb.WriteByte(' ')
			}
		}
	}
}

func isHtmlHidden(tag []byte) bool {
	switch string(tag) {
	case "script", "style", "noscript", "template":
		return true
	default:
		return false
	}
}
