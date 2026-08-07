package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/config"
	"github.com/sayem314/oracle/apps/api/internal/permission"
)

const testAuthSecret = "0123456789abcdef0123456789abcdef"

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ORACLE_PORT", "ORACLE_LOG_LEVEL", "ORACLE_DATABASE_URL", "ORACLE_AUTH_SECRET", "PORT",
		"ORACLE_LLM_PROVIDER", "ORACLE_LLM_BASE_URL", "ORACLE_LLM_API_KEY", "ORACLE_LLM_MODEL",
		"ORACLE_PERMISSION_DEFAULT", "ORACLE_PERMISSION_RULES",
		"ORACLE_CONTEXT_WINDOW", "ORACLE_CONTEXT_RESERVE", "ORACLE_CONTEXT_TAIL_TURNS",
		"ORACLE_CONTEXT_KEEP_RECENT_TOKENS", "ORACLE_TOOL_OUTPUT_CHARS",
	} {
		if v, ok := os.LookupEnv(key); ok {
			t.Cleanup(func() { _ = os.Setenv(key, v) })
			_ = os.Unsetenv(key)
		}
	}
}

func withAuthSecret(t *testing.T) {
	t.Helper()
	t.Setenv("ORACLE_AUTH_SECRET", testAuthSecret)
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, zerolog.InfoLevel, cfg.LogLevel)
	assert.Contains(t, cfg.DatabaseURL, "file:oracle.db")
	assert.Contains(t, cfg.DatabaseURL, "foreign_keys(ON)")
	assert.Equal(t, testAuthSecret, cfg.AuthSecret)
	assert.Equal(t, "mock", cfg.LLMProvider)
	assert.Equal(t, permission.Ask, cfg.PermissionDefault)
	assert.Empty(t, cfg.PermissionRules)
	assert.Equal(t, 32000, cfg.ContextWindow)
	assert.Equal(t, 20000, cfg.ContextReserve)
	assert.Equal(t, 2, cfg.ContextTailTurns)
	assert.Equal(t, 8000, cfg.ContextKeepRecentTokens)
	assert.Equal(t, 2000, cfg.ToolOutputChars)
	assert.Equal(t, 5*time.Second, cfg.LoopPollInterval)
	assert.Equal(t, 10*time.Minute, cfg.LoopRunTimeout)
}

func TestLoopEnvOverride(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_LOOP_POLL_INTERVAL", "2s")
	t.Setenv("ORACLE_LOOP_RUN_TIMEOUT", "30s")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 2*time.Second, cfg.LoopPollInterval)
	assert.Equal(t, 30*time.Second, cfg.LoopRunTimeout)
}

func TestLoopInvalidDuration(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_LOOP_POLL_INTERVAL", "soon")

	_, err := config.Load()
	require.Error(t, err)
}

func TestDatabaseURLEnvOverride(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_DATABASE_URL", "file::memory:")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "file::memory:", cfg.DatabaseURL)
}

func TestLoadEmptyDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("ORACLE_DATABASE_URL", "")

	_, err := config.Load()
	require.ErrorContains(t, err, "database_url is required")
}

func TestLoadEnvOverride(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_PORT", "9090")
	t.Setenv("ORACLE_LOG_LEVEL", "debug")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, zerolog.DebugLevel, cfg.LogLevel)
}

func TestContextBudgetEnvOverride(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_CONTEXT_WINDOW", "64000")
	t.Setenv("ORACLE_CONTEXT_RESERVE", "40000")
	t.Setenv("ORACLE_CONTEXT_TAIL_TURNS", "4")
	t.Setenv("ORACLE_CONTEXT_KEEP_RECENT_TOKENS", "12000")
	t.Setenv("ORACLE_TOOL_OUTPUT_CHARS", "3000")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 64000, cfg.ContextWindow)
	assert.Equal(t, 40000, cfg.ContextReserve)
	assert.Equal(t, 4, cfg.ContextTailTurns)
	assert.Equal(t, 12000, cfg.ContextKeepRecentTokens)
	assert.Equal(t, 3000, cfg.ToolOutputChars)
}

func TestLoadDotenvFile(t *testing.T) {
	clearEnv(t)
	t.Chdir(t.TempDir())
	env := "ORACLE_LOG_LEVEL=warn\nORACLE_AUTH_SECRET=" + testAuthSecret + "\n"
	require.NoError(t, os.WriteFile(".env", []byte(env), 0o600))

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, zerolog.WarnLevel, cfg.LogLevel)
	assert.Equal(t, testAuthSecret, cfg.AuthSecret)
}

func TestEnvOverridesDotenv(t *testing.T) {
	clearEnv(t)
	t.Chdir(t.TempDir())
	withAuthSecret(t)
	require.NoError(t, os.WriteFile(".env", []byte("ORACLE_LOG_LEVEL=warn\n"), 0o600))
	t.Setenv("ORACLE_LOG_LEVEL", "debug")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, zerolog.DebugLevel, cfg.LogLevel)
}

func TestPlainPortFallback(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("PORT", "9091")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 9091, cfg.Port)
}

func TestOraclePortWinsOverPlainPort(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("PORT", "9091")
	t.Setenv("ORACLE_PORT", "9092")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 9092, cfg.Port)
}

func TestLoadInvalidLogLevel(t *testing.T) {
	clearEnv(t)
	t.Setenv("ORACLE_LOG_LEVEL", "loud")

	_, err := config.Load()
	require.ErrorContains(t, err, "invalid log_level")
}

func TestLoadInvalidPlainPort(t *testing.T) {
	clearEnv(t)
	t.Setenv("PORT", "abc")

	_, err := config.Load()
	require.ErrorContains(t, err, "invalid port")
}

func TestLoadInvalidOraclePort(t *testing.T) {
	clearEnv(t)
	t.Setenv("ORACLE_PORT", "abc")

	_, err := config.Load()
	require.ErrorContains(t, err, "invalid port")
}

func TestLLMProviderRealProvider(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_LLM_PROVIDER", "openai")
	t.Setenv("ORACLE_LLM_API_KEY", "test-key")
	t.Setenv("ORACLE_LLM_MODEL", "test-model")
	t.Setenv("ORACLE_LLM_BASE_URL", "http://localhost:11434/v1")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "openai", cfg.LLMProvider)
	assert.Equal(t, "test-key", cfg.LLMAPIKey)
	assert.Equal(t, "test-model", cfg.LLMModel)
	assert.Equal(t, "http://localhost:11434/v1", cfg.LLMBaseURL)
}

func TestLLMProviderMissingAPIKey(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_LLM_PROVIDER", "openai")
	t.Setenv("ORACLE_LLM_MODEL", "gpt-4o")

	_, err := config.Load()
	require.ErrorContains(t, err, `llm_api_key is required for llm_provider "openai"`)
}

func TestLLMProviderMissingModel(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_LLM_PROVIDER", "openai")
	t.Setenv("ORACLE_LLM_API_KEY", "test-key")

	_, err := config.Load()
	require.ErrorContains(t, err, `llm_model is required for llm_provider "openai"`)
}

func TestLLMProviderInvalid(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_LLM_PROVIDER", "gemini")

	_, err := config.Load()
	require.ErrorContains(t, err, `invalid llm_provider "gemini"`)
}

func TestLoadMissingAuthSecret(t *testing.T) {
	clearEnv(t)

	_, err := config.Load()
	require.ErrorContains(t, err, "auth_secret must be exactly 32 bytes, got 0")
}

func TestLoadShortAuthSecret(t *testing.T) {
	clearEnv(t)
	t.Setenv("ORACLE_AUTH_SECRET", "too-short")

	_, err := config.Load()
	require.ErrorContains(t, err, "auth_secret must be exactly 32 bytes, got 9")
}

func TestPermissionRulesEnv(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_PERMISSION_DEFAULT", "allow")
	t.Setenv("ORACLE_PERMISSION_RULES", "get_time:allow, danger_*:deny")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, permission.Allow, cfg.PermissionDefault)
	require.Len(t, cfg.PermissionRules, 2)
	assert.Equal(t, "get_time", cfg.PermissionRules[0].Tool)
	assert.Equal(t, permission.Allow, cfg.PermissionRules[0].Verdict)
	assert.Equal(t, "danger_*", cfg.PermissionRules[1].Tool)
	assert.Equal(t, permission.Deny, cfg.PermissionRules[1].Verdict)
}

func TestPermissionDefaultInvalid(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_PERMISSION_DEFAULT", "maybe")

	_, err := config.Load()
	require.ErrorContains(t, err, "invalid permission_default")
}

func TestPermissionRulesInvalid(t *testing.T) {
	clearEnv(t)
	withAuthSecret(t)
	t.Setenv("ORACLE_PERMISSION_RULES", "get_time")

	_, err := config.Load()
	require.ErrorContains(t, err, "invalid permission_rules")
}
