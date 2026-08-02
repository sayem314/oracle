package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/server"
)

func main() {
	port := 8080
	if p := os.Getenv("PORT"); p != "" {
		v, err := strconv.Atoi(p)
		if err != nil {
			log.Fatalf("invalid PORT: %v", err)
		}
		port = v
	}

	app := server.New()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-quit
		if err := app.Shutdown(); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("oracle api listening on :%d", port)
	if err := app.Listen(":"+strconv.Itoa(port), fiber.ListenConfig{
		DisableStartupMessage: true,
	}); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
