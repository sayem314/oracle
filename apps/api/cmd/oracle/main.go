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

	"github.com/sayem314/oracle/apps/api/internal/config"
	"github.com/sayem314/oracle/apps/api/internal/server"
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

	app := server.New()

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
