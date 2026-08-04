package permission

import (
	"fmt"
	"path"
	"strings"
)

type Verdict string

const (
	Allow Verdict = "allow"
	Deny  Verdict = "deny"
	Ask   Verdict = "ask"
)

// Rule matches tool names with a glob pattern and assigns a verdict.
type Rule struct {
	Tool    string
	Verdict Verdict
}

// Ruleset decides whether a tool call may run. Rules are evaluated in order
// and the last matching rule wins, except that any matching deny rule is
// absolute and cannot be overridden by later allows. Unmatched tools fall
// back to Default.
type Ruleset struct {
	rules   []Rule
	Default Verdict
}

func NewRuleset(defaultVerdict Verdict, rules []Rule) *Ruleset {
	return &Ruleset{rules: rules, Default: defaultVerdict}
}

func (rs *Ruleset) Evaluate(toolName string) Verdict {
	for _, rule := range rs.rules {
		if rule.Verdict == Deny && matches(rule.Tool, toolName) {
			return Deny
		}
	}
	verdict := rs.Default
	for _, rule := range rs.rules {
		if matches(rule.Tool, toolName) {
			verdict = rule.Verdict
		}
	}
	return verdict
}

func matches(pattern, name string) bool {
	ok, err := path.Match(pattern, name)
	return err == nil && ok
}

func ParseVerdict(s string) (Verdict, error) {
	switch v := Verdict(strings.ToLower(strings.TrimSpace(s))); v {
	case Allow, Deny, Ask:
		return v, nil
	default:
		return "", fmt.Errorf("permission: invalid verdict %q", s)
	}
}

// ParseRules parses "tool:verdict" entries separated by commas, e.g.
// "get_time:allow, web_*:ask".
func ParseRules(s string) ([]Rule, error) {
	var rules []Rule
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		tool, verdictStr, ok := strings.Cut(entry, ":")
		if !ok {
			return nil, fmt.Errorf("permission: rule %q must be tool:verdict", entry)
		}
		tool = strings.TrimSpace(tool)
		if tool == "" {
			return nil, fmt.Errorf("permission: rule %q is missing a tool pattern", entry)
		}
		if _, err := path.Match(tool, "probe"); err != nil {
			return nil, fmt.Errorf("permission: rule %q has an invalid pattern: %w", entry, err)
		}
		verdict, err := ParseVerdict(verdictStr)
		if err != nil {
			return nil, fmt.Errorf("permission: rule %q: %w", entry, err)
		}
		rules = append(rules, Rule{Tool: tool, Verdict: verdict})
	}
	return rules, nil
}
