package server

import "github.com/gofiber/fiber/v3"

func New() *fiber.App {
	app := fiber.New(fiber.Config{
		AppName: "oracle",
	})

	app.Get("/health", health)

	return app
}
