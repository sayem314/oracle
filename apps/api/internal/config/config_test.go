package config_test

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/config"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"ORACLE_PORT", "ORACLE_LOG_LEVEL", "PORT"} {
		if v, ok := os.LookupEnv(key); ok {
			t.Cleanup(func() { _ = os.Setenv(key, v) })
			_ = os.Unsetenv(key)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, zerolog.InfoLevel, cfg.LogLevel)
}

func TestLoadEnvOverride(t *testing.T) {
	clearEnv(t)
	t.Setenv("ORACLE_PORT", "9090")
	t.Setenv("ORACLE_LOG_LEVEL", "debug")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, zerolog.DebugLevel, cfg.LogLevel)
}

func TestLoadDotenvFile(t *testing.T) {
	clearEnv(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile(".env", []byte("ORACLE_LOG_LEVEL=warn\n"), 0o600))

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, zerolog.WarnLevel, cfg.LogLevel)
}

func TestEnvOverridesDotenv(t *testing.T) {
	clearEnv(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile(".env", []byte("ORACLE_LOG_LEVEL=warn\n"), 0o600))
	t.Setenv("ORACLE_LOG_LEVEL", "debug")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, zerolog.DebugLevel, cfg.LogLevel)
}

func TestPlainPortFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("PORT", "9091")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 9091, cfg.Port)
}

func TestOraclePortWinsOverPlainPort(t *testing.T) {
	clearEnv(t)
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
