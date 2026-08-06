package server

import (
	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/auth"
)

type profileResponse struct {
	Email   string `json:"email"`
	Role    string `json:"role"`
	IsAdmin bool   `json:"is_admin"`
}

func newProfileHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		userID := c.Locals(userIDKey{}).(int64)
		user, err := deps.Auth.GetUser(c.Context(), userID)
		if err != nil {
			return err
		}
		return c.JSON(profileResponse{
			Email:   user.Email,
			Role:    user.Role,
			IsAdmin: user.Role == auth.RoleAdmin,
		})
	}
}
