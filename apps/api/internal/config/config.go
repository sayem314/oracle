package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"

	"github.com/sayem314/oracle/apps/api/internal/permission"
)

const envPrefix = "ORACLE_"

const defaultDatabaseURL = "file:oracle.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"

const defaultLLMProvider = "mock"

const (
	defaultContextWindow     = 32000
	defaultContextReserve    = 20000
	defaultContextTailTurns  = 2
	defaultContextKeepRecent = 8000
	defaultToolOutputChars   = 2000
	defaultLoopPollInterval  = 5 * time.Second
	defaultLoopRunTimeout    = 10 * time.Minute
	defaultLoopMaxRuns       = 0
)

type Config struct {
	Port              int
	LogLevel          zerolog.Level
	DatabaseURL       string
	AuthSecret        string
	AuthCookieSecure  bool
	LLMProvider       string
	LLMBaseURL        string
	LLMAPIKey         string
	LLMModel          string
	PermissionDefault permission.Verdict
	PermissionRules   []permission.Rule

	// ContextWindow is the estimated LLM context budget in tokens, kept well
	// under the real model window so compaction has room.
	// Compaction and tool-output truncation both derive from it.
	ContextWindow           int
	ContextReserve          int
	ContextTailTurns        int
	ContextKeepRecentTokens int
	ToolOutputChars         int

	// LoopPollInterval is how often the scheduler scans sessions for due loop
	// iterations. LoopRunTimeout bounds each headless iteration. LoopMaxRuns
	// caps total iterations per session loop. Zero means unlimited.
	LoopPollInterval time.Duration
	LoopRunTimeout   time.Duration
	LoopMaxRuns      int
}

func Load() (Config, error) {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(map[string]any{
		"port":                       8080,
		"log_level":                  "info",
		"database_url":               defaultDatabaseURL,
		"llm_provider":               defaultLLMProvider,
		"auth_cookie_secure":         false,
		"permission_default":         "ask",
		"permission_rules":           "",
		"context_window":             defaultContextWindow,
		"context_reserve":            defaultContextReserve,
		"context_tail_turns":         defaultContextTailTurns,
		"context_keep_recent_tokens": defaultContextKeepRecent,
		"tool_output_chars":          defaultToolOutputChars,
		"loop_poll_interval":         defaultLoopPollInterval.String(),
		"loop_run_timeout":           defaultLoopRunTimeout.String(),
		"loop_max_runs":              strconv.Itoa(defaultLoopMaxRuns),
	}, "."), nil); err != nil {
		return Config{}, fmt.Errorf("load defaults: %w", err)
	}

	// Loaded before real env vars so the environment always wins over .env.
	if err := loadDotenv(k); err != nil {
		return Config{}, err
	}

	if err := k.Load(env.Provider(envPrefix, ".", func(s string) string {
		return strings.ToLower(strings.TrimPrefix(s, envPrefix))
	}), nil); err != nil {
		return Config{}, fmt.Errorf("load env: %w", err)
	}

	if p := os.Getenv("PORT"); p != "" && os.Getenv(envPrefix+"PORT") == "" {
		if err := k.Load(confmap.Provider(map[string]any{"port": p}, "."), nil); err != nil {
			return Config{}, fmt.Errorf("load PORT: %w", err)
		}
	}

	port, err := strconv.Atoi(k.String("port"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid port: %w", err)
	}

	lvl, err := zerolog.ParseLevel(k.String("log_level"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid log_level: %w", err)
	}

	databaseURL := k.String("database_url")
	if databaseURL == "" {
		return Config{}, errors.New("database_url is required")
	}

	authSecret := k.String("auth_secret")
	if len(authSecret) != 32 {
		return Config{}, fmt.Errorf("auth_secret must be exactly 32 bytes, got %d", len(authSecret))
	}

	llmProvider := k.String("llm_provider")
	switch llmProvider {
	case "mock":
	case "openai":
		if k.String("llm_api_key") == "" {
			return Config{}, fmt.Errorf("llm_api_key is required for llm_provider %q", llmProvider)
		}
		if k.String("llm_model") == "" {
			return Config{}, fmt.Errorf("llm_model is required for llm_provider %q", llmProvider)
		}
	default:
		return Config{}, fmt.Errorf("invalid llm_provider %q", llmProvider)
	}

	permissionDefault, err := permission.ParseVerdict(k.String("permission_default"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid permission_default: %w", err)
	}
	permissionRules, err := permission.ParseRules(k.String("permission_rules"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid permission_rules: %w", err)
	}

	loopPollInterval := k.Duration("loop_poll_interval")
	if loopPollInterval <= 0 {
		return Config{}, fmt.Errorf("loop_poll_interval must be a positive duration, got %q", k.String("loop_poll_interval"))
	}
	loopRunTimeout := k.Duration("loop_run_timeout")
	if loopRunTimeout <= 0 {
		return Config{}, fmt.Errorf("loop_run_timeout must be a positive duration, got %q", k.String("loop_run_timeout"))
	}
	loopMaxRuns := k.Int("loop_max_runs")
	if loopMaxRuns < 0 {
		return Config{}, fmt.Errorf("loop_max_runs must be zero or positive, got %q", k.String("loop_max_runs"))
	}

	return Config{
		Port:                    port,
		LogLevel:                lvl,
		DatabaseURL:             databaseURL,
		AuthSecret:              authSecret,
		AuthCookieSecure:        k.Bool("auth_cookie_secure"),
		LLMProvider:             llmProvider,
		LLMBaseURL:              k.String("llm_base_url"),
		LLMAPIKey:               k.String("llm_api_key"),
		LLMModel:                k.String("llm_model"),
		PermissionDefault:       permissionDefault,
		PermissionRules:         permissionRules,
		ContextWindow:           k.Int("context_window"),
		ContextReserve:          k.Int("context_reserve"),
		ContextTailTurns:        k.Int("context_tail_turns"),
		ContextKeepRecentTokens: k.Int("context_keep_recent_tokens"),
		ToolOutputChars:         k.Int("tool_output_chars"),
		LoopPollInterval:        loopPollInterval,
		LoopRunTimeout:          loopRunTimeout,
		LoopMaxRuns:             loopMaxRuns,
	}, nil
}

func loadDotenv(k *koanf.Koanf) error {
	b, err := os.ReadFile(".env")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read .env: %w", err)
	}

	parsed, err := dotenv.Parser().Unmarshal(b)
	if err != nil {
		return fmt.Errorf("parse .env: %w", err)
	}

	values := make(map[string]any, len(parsed))
	for key, value := range parsed {
		if !strings.HasPrefix(key, envPrefix) {
			continue
		}
		values[strings.ToLower(strings.TrimPrefix(key, envPrefix))] = value
	}

	if err := k.Load(confmap.Provider(values, "."), nil); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	return nil
}
