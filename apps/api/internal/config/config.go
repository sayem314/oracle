package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
)

const envPrefix = "ORACLE_"

const defaultDatabaseURL = "file:oracle.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"

const defaultLLMProvider = "mock"

type Config struct {
	Port        int
	LogLevel    zerolog.Level
	DatabaseURL string
	LLMProvider string
	LLMBaseURL  string
	LLMAPIKey   string
	LLMModel    string
}

func Load() (Config, error) {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(map[string]any{
		"port":         8080,
		"log_level":    "info",
		"database_url": defaultDatabaseURL,
		"llm_provider": defaultLLMProvider,
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

	return Config{
		Port:        port,
		LogLevel:    lvl,
		DatabaseURL: databaseURL,
		LLMProvider: llmProvider,
		LLMBaseURL:  k.String("llm_base_url"),
		LLMAPIKey:   k.String("llm_api_key"),
		LLMModel:    k.String("llm_model"),
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
