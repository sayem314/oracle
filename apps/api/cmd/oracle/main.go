package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/term"

	"github.com/sayem314/oracle/apps/api/internal/auth"
	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/config"
	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/permission"
	"github.com/sayem314/oracle/apps/api/internal/scheduler"
	"github.com/sayem314/oracle/apps/api/internal/server"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("load config")
	}

	var out io.Writer = os.Stdout
	if term.IsTerminal(int(os.Stdout.Fd())) {
		out = zerolog.ConsoleWriter{Out: os.Stdout}
	}
	log.Logger = zerolog.New(out).Level(cfg.LogLevel).With().Timestamp().Logger()

	sqlDB, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("open database")
	}
	defer sqlDB.Close() //nolint:errcheck

	applied, err := store.Migrate(sqlDB)
	if err != nil {
		log.Fatal().Err(err).Msg("migrate database")
	}
	if applied > 0 {
		log.Info().Int("applied", applied).Msg("migrations applied")
	}

	authenticator, err := auth.New(sqlDB, auth.Options{
		Secret:       []byte(cfg.AuthSecret),
		CookieSecure: cfg.AuthCookieSecure,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("init auth")
	}

	provider, err := llm.New(llm.Options{
		Provider: cfg.LLMProvider,
		BaseURL:  cfg.LLMBaseURL,
		APIKey:   cfg.LLMAPIKey,
		Model:    cfg.LLMModel,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("init llm provider")
	}
	log.Info().Str("provider", cfg.LLMProvider).Msg("llm provider ready")

	tools := tool.NewRegistry()
	for _, t := range tool.NewBuiltin() {
		if err := tools.Register(t); err != nil {
			log.Fatal().Err(err).Msg("register tool")
		}
	}

	st := store.New(sqlDB)
	ruleset := permission.NewRuleset(cfg.PermissionDefault, cfg.PermissionRules)
	engine := &chat.Engine{
		Store:       st,
		LLM:         &chat.LLMResolver{Store: st, Default: provider},
		Tools:       tools,
		Permissions: ruleset,
	}

	sched := scheduler.New(st, engine.AsHeadless(), scheduler.DefaultInterval)
	sched.Start()
	log.Info().Msg("scheduler started")

	app := server.New(server.Deps{
		Store: st,
		Auth:  authenticator,
		Chat:  engine,
	})

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-quit
		log.Info().Msg("shutting down")
		sched.Stop()
		if err := app.Shutdown(); err != nil {
			log.Error().Err(err).Msg("shutdown")
		}
	}()

	log.Info().Int("port", cfg.Port).Msg("oracle api listening")
	if err := app.Listen(fmt.Sprintf(":%d", cfg.Port), fiber.ListenConfig{
		DisableStartupMessage: true,
	}); err != nil {
		log.Fatal().Err(err).Msg("listen")
	}
}
