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

type Config struct {
	Port     int
	LogLevel zerolog.Level
}

func Load() (Config, error) {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(map[string]any{
		"port":      8080,
		"log_level": "info",
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

	return Config{Port: port, LogLevel: lvl}, nil
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
