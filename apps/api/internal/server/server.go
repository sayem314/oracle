package server

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"

	"github.com/sayem314/oracle/apps/api/internal/auth"
	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/store"
)

type Deps struct {
	Store store.Store
	Auth  auth.Auth
	Chat  *chat.Engine
}

type userIDKey struct{}

func New(deps Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "oracle",
		ErrorHandler: errorHandler,
	})

	app.Get("/health", health)
	app.All("/auth/*", signupGate(deps.Auth), adaptor.HTTPHandler(deps.Auth.Handler()))

	api := app.Group("/api/v1", requireSession(deps.Auth))
	api.Post("/chat", newChatHandler(deps))
	api.Post("/approvals", newApprovalHandler(deps))

	api.Get("/llm/provider", newGetLLMProviderHandler(deps))
	admin := api.Group("/llm/provider", requireAdmin(deps.Auth))
	admin.Put("", newUpsertLLMProviderHandler(deps))
	admin.Post("/fetch-models", newFetchLLMModelsHandler(deps))
	admin.Put("/model", newSetLLMModelHandler(deps))

	adminSettings := api.Group("/settings", requireAdmin(deps.Auth))
	adminSettings.Get("", newGetSettingsHandler(deps))
	adminSettings.Put("", newUpsertSettingsHandler(deps))

	api.Get("/profile", newProfileHandler(deps))

	api.Get("/sessions", newListSessionsHandler(deps))
	api.Get("/sessions/:id/messages", newListMessagesHandler(deps))
	api.Patch("/sessions/:id", newUpdateSessionHandler(deps))
	api.Delete("/sessions/:id", newDeleteSessionHandler(deps))

	return app
}

// signupGate keeps registration open only until the first user exists.
func signupGate(a auth.Auth) fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Method() != fiber.MethodPost || !strings.Contains(c.Path(), "/signup") {
			return c.Next()
		}
		hasUsers, err := a.HasUsers(c.Context())
		if err != nil {
			return err
		}
		if hasUsers {
			return fiber.NewError(fiber.StatusForbidden, "sign-up is disabled")
		}
		return c.Next()
	}
}

func requireSession(a auth.Auth) fiber.Handler {
	return func(c fiber.Ctx) error {
		r, err := adaptor.ConvertRequest(c, false)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
		}
		userID, err := a.UserID(r)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
		}
		c.Locals(userIDKey{}, userID)
		return c.Next()
	}
}

// requireAdmin runs after requireSession and rejects non-admin callers.
func requireAdmin(a auth.Auth) fiber.Handler {
	return func(c fiber.Ctx) error {
		userID := c.Locals(userIDKey{}).(int64)
		role, err := a.Role(c.Context(), userID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
			}
			return err
		}
		if role != auth.RoleAdmin {
			return fiber.NewError(fiber.StatusForbidden, "admin access required")
		}
		return c.Next()
	}
}

func errorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if fe, ok := errors.AsType[*fiber.Error](err); ok {
		code = fe.Code
	}
	return c.Status(code).JSON(fiber.Map{"error": err.Error()})
}
