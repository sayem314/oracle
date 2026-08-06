package permission_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/permission"
)

func TestEvaluate(t *testing.T) {
	rules := []permission.Rule{
		{Tool: "*", Verdict: permission.Allow},
		{Tool: "web_*", Verdict: permission.Ask},
		{Tool: "web_fetch", Verdict: permission.Allow},
		{Tool: "danger_*", Verdict: permission.Deny},
		{Tool: "danger_read", Verdict: permission.Allow},
	}
	rs := permission.NewRuleset(permission.Ask, rules)

	tests := []struct {
		tool string
		want permission.Verdict
	}{
		{"get_time", permission.Allow},
		{"unknown_tool", permission.Allow},
		{"web_search", permission.Ask},
		{"web_fetch", permission.Allow},
		{"danger_write", permission.Deny},
		{"danger_read", permission.Deny},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			assert.Equal(t, tt.want, rs.Evaluate(tt.tool))
		})
	}
}

func TestEvaluateLastMatchWins(t *testing.T) {
	rs := permission.NewRuleset(permission.Ask, []permission.Rule{
		{Tool: "*", Verdict: permission.Allow},
		{Tool: "risky_*", Verdict: permission.Ask},
	})
	assert.Equal(t, permission.Allow, rs.Evaluate("get_time"))
	assert.Equal(t, permission.Ask, rs.Evaluate("risky_call"))
}

func TestEvaluateDefaultAllow(t *testing.T) {
	rs := permission.NewRuleset(permission.Allow, []permission.Rule{
		{Tool: "risky_*", Verdict: permission.Ask},
	})
	assert.Equal(t, permission.Allow, rs.Evaluate("get_time"))
	assert.Equal(t, permission.Ask, rs.Evaluate("risky_call"))
}

func TestEvaluateNoRules(t *testing.T) {
	rs := permission.NewRuleset(permission.Ask, nil)
	assert.Equal(t, permission.Ask, rs.Evaluate("anything"))
}

func TestWithUserOverrides(t *testing.T) {
	base := permission.NewRuleset(permission.Allow, []permission.Rule{
		{Tool: "web_*", Verdict: permission.Ask},
		{Tool: "shutdown", Verdict: permission.Deny},
	})

	t.Run("no default override keeps the base default", func(t *testing.T) {
		rs := base.WithUserOverrides(false, permission.Deny, []permission.Rule{
			{Tool: "web_search", Verdict: permission.Allow},
		})
		assert.Equal(t, permission.Allow, rs.Evaluate("get_time"))
		// Per-user allow beats the base ask for web_* (last match wins).
		assert.Equal(t, permission.Allow, rs.Evaluate("web_search"))
		// Base deny stays absolute against a per-user allow.
		assert.Equal(t, permission.Deny, rs.Evaluate("shutdown"))
	})

	t.Run("default override replaces the fallback", func(t *testing.T) {
		rs := base.WithUserOverrides(true, permission.Deny, nil)
		assert.Equal(t, permission.Deny, rs.Evaluate("get_time"))
		assert.Equal(t, permission.Ask, rs.Evaluate("web_search"))
	})

	t.Run("per-user deny beats base allow and ask", func(t *testing.T) {
		rs := base.WithUserOverrides(false, permission.Allow, []permission.Rule{
			{Tool: "get_time", Verdict: permission.Deny},
			{Tool: "web_*", Verdict: permission.Deny},
		})
		assert.Equal(t, permission.Deny, rs.Evaluate("get_time"))
		assert.Equal(t, permission.Deny, rs.Evaluate("web_search"))
	})

	t.Run("original ruleset is untouched", func(t *testing.T) {
		_ = base.WithUserOverrides(true, permission.Deny, []permission.Rule{
			{Tool: "*", Verdict: permission.Deny},
		})
		assert.Equal(t, permission.Allow, base.Evaluate("anything"))
	})
}

func TestWithUserOverridesHeadless(t *testing.T) {
	base := permission.NewRuleset(permission.Allow, nil)
	rs := base.WithUserOverrides(true, permission.Ask, nil)
	assert.Equal(t, permission.Deny, rs.EvaluateHeadless("get_time"))
}

func TestEvaluateHeadless(t *testing.T) {
	rs := permission.NewRuleset(permission.Ask, []permission.Rule{
		{Tool: "get_time", Verdict: permission.Allow},
		{Tool: "risky_*", Verdict: permission.Ask},
		{Tool: "danger_*", Verdict: permission.Deny},
	})

	// allow and deny stand; ask (explicit or default) becomes deny.
	assert.Equal(t, permission.Allow, rs.EvaluateHeadless("get_time"))
	assert.Equal(t, permission.Deny, rs.EvaluateHeadless("risky_call"))
	assert.Equal(t, permission.Deny, rs.EvaluateHeadless("danger_write"))
	assert.Equal(t, permission.Deny, rs.EvaluateHeadless("unmatched_defaults_to_ask"))
}

func TestParseVerdict(t *testing.T) {
	for _, valid := range []string{"allow", "deny", "ask", " ALLOW ", "Ask"} {
		v, err := permission.ParseVerdict(valid)
		require.NoError(t, err)
		assert.NotEmpty(t, v)
	}
	_, err := permission.ParseVerdict("maybe")
	require.ErrorContains(t, err, "invalid verdict")
}

func TestParseRules(t *testing.T) {
	rules, err := permission.ParseRules("get_time:allow, web_*:ask, rm_*:deny")
	require.NoError(t, err)
	require.Len(t, rules, 3)
	assert.Equal(t, permission.Rule{Tool: "get_time", Verdict: permission.Allow}, rules[0])
	assert.Equal(t, permission.Rule{Tool: "web_*", Verdict: permission.Ask}, rules[1])
	assert.Equal(t, permission.Rule{Tool: "rm_*", Verdict: permission.Deny}, rules[2])
}

func TestParseRulesEmpty(t *testing.T) {
	rules, err := permission.ParseRules("  ")
	require.NoError(t, err)
	assert.Empty(t, rules)
}

func TestParseRulesInvalid(t *testing.T) {
	tests := []string{
		"get_time",
		"get_time:maybe",
		":allow",
		"[bad:allow",
	}
	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			_, err := permission.ParseRules(tt)
			require.Error(t, err)
		})
	}
}
