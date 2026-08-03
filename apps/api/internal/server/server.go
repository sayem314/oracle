package server

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store"
)

type Deps struct {
	Store store.Store
	LLM   llm.Provider
}

func New(deps Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "oracle",
		ErrorHandler: errorHandler,
	})

	app.Get("/health", health)
	app.Post("/api/v1/chat", newChatHandler(deps))

	return app
}

func errorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if fe, ok := errors.AsType[*fiber.Error](err); ok {
		code = fe.Code
	}
	return c.Status(code).JSON(fiber.Map{"error": err.Error()})
}
