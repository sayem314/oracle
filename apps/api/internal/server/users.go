package server

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/auth"
)

type userResponse struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func userToResponse(u auth.UserInfo) userResponse {
	return userResponse{
		ID:        u.ID,
		Email:     u.Email,
		Role:      u.Role,
		CreatedAt: u.CreatedAt,
	}
}

func newListUsersHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		users, err := deps.Auth.ListUsers(c.Context())
		if err != nil {
			return err
		}
		out := make([]userResponse, 0, len(users))
		for _, u := range users {
			out = append(out, userToResponse(u))
		}
		return c.JSON(out)
	}
}

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func newCreateUserHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req createUserRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		email := strings.TrimSpace(req.Email)
		if email == "" {
			return fiber.NewError(fiber.StatusBadRequest, "email is required")
		}
		if req.Password == "" {
			return fiber.NewError(fiber.StatusBadRequest, "password is required")
		}

		user, err := deps.Auth.CreateUser(c.Context(), email, req.Password)
		if err != nil {
			var aerr *auth.Error
			if errors.As(err, &aerr) {
				return fiber.NewError(aerr.Status, aerr.Message)
			}
			return err
		}

		return c.Status(fiber.StatusCreated).JSON(userToResponse(user))
	}
}

func newDeleteUserHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil || id <= 0 {
			return fiber.NewError(fiber.StatusBadRequest, "invalid user id")
		}

		// An admin cannot delete themselves: it would strand the account mid
		// session and, for the only admin, lock the instance out of admin.
		if id == c.Locals(userIDKey{}).(int64) {
			return fiber.NewError(fiber.StatusBadRequest, "cannot delete yourself")
		}

		if err := deps.Auth.DeleteUser(c.Context(), id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fiber.NewError(fiber.StatusNotFound, "user not found")
			}
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}
