package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
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
	"github.com/sayem314/oracle/apps/api/internal/server"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
	"github.com/sayem314/oracle/apps/api/internal/tool"
	"github.com/sayem314/oracle/apps/api/internal/tool/all"
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

	st := store.New(sqlDB)
	seedProvider(st, cfg)
	seedSettings(st, cfg)
	ruleset := loadRuleset(st, cfg)

	tools := tool.NewRegistry()
	if err := tools.RegisterGroups(all.Groups()...); err != nil {
		log.Fatal().Err(err).Msg("register tools")
	}

	engine := &chat.Engine{
		Store:       st,
		LLM:         &chat.LLMResolver{Store: st, Default: provider},
		Tools:       tools,
		Permissions: ruleset,
		Environment: chat.DetectEnvironment(),
		Compaction: chat.CompactionConfig{
			ContextWindow:    cfg.ContextWindow,
			ReserveTokens:    cfg.ContextReserve,
			TailTurns:        cfg.ContextTailTurns,
			KeepRecentTokens: cfg.ContextKeepRecentTokens,
			ToolOutputChars:  cfg.ToolOutputChars,
		},
	}

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

// seedProvider writes the singleton LLM provider row from env config unless
// one already exists, so a fresh install boots with the configured gateway.
func seedProvider(st store.Store, cfg config.Config) {
	if _, err := st.GetLLMProvider(context.Background()); err == nil {
		return
	}
	if _, err := st.UpsertLLMProvider(context.Background(), db.UpsertLLMProviderParams{
		Provider: cfg.LLMProvider,
		BaseUrl:  cfg.LLMBaseURL,
		ApiKey:   cfg.LLMAPIKey,
		Model:    cfg.LLMModel,
	}); err != nil {
		log.Fatal().Err(err).Msg("seed llm provider")
	}
}

// seedSettings writes the global permission ruleset from env config unless a
// row already exists.
func seedSettings(st store.Store, cfg config.Config) {
	if _, err := st.GetSettings(context.Background()); err == nil {
		return
	}
	if _, err := st.UpsertSettings(context.Background(), db.UpsertSettingsParams{
		PermissionDefault: string(cfg.PermissionDefault),
		PermissionRules:   permissionRulesString(cfg.PermissionRules),
	}); err != nil {
		log.Fatal().Err(err).Msg("seed settings")
	}
}

// loadRuleset builds the global permission ruleset from stored settings or env
// config when storage is empty.
func loadRuleset(st store.Store, cfg config.Config) *permission.Ruleset {
	s, err := st.GetSettings(context.Background())
	if err != nil {
		return permission.NewRuleset(cfg.PermissionDefault, cfg.PermissionRules)
	}
	def, err := permission.ParseVerdict(s.PermissionDefault)
	if err != nil {
		def = cfg.PermissionDefault
	}
	rules, err := permission.ParseRules(s.PermissionRules)
	if err != nil {
		rules = cfg.PermissionRules
	}
	return permission.NewRuleset(def, rules)
}

func permissionRulesString(rules []permission.Rule) string {
	var out []string
	for _, r := range rules {
		out = append(out, string(r.Tool)+":"+string(r.Verdict))
	}
	return strings.Join(out, ",")
}
