package server

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type llmPrefsResponse struct {
	ProviderID int64  `json:"provider_id"`
	Model      string `json:"model"`
}

type llmPrefsRequest struct {
	ProviderID int64  `json:"provider_id"`
	Model      string `json:"model"`
}

func newGetLLMPrefsHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		userID := c.Locals(userIDKey{}).(int64)
		pref, err := deps.Store.GetUserLLMPrefs(c.Context(), userID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return c.JSON(llmPrefsResponse{})
			}
			return err
		}
		res := llmPrefsResponse{Model: pref.Model}
		if pref.ProviderID.Valid {
			res.ProviderID = pref.ProviderID.Int64
		}
		return c.JSON(res)
	}
}

// checkProviderModel verifies a non-zero provider_id points at an existing
// provider and that model, when set, belongs to it.
func checkProviderModel(deps Deps, c fiber.Ctx, providerID int64, model string) error {
	if providerID == 0 {
		return nil
	}
	provider, err := deps.Store.GetLLMProvider(c.Context(), providerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fiber.NewError(fiber.StatusNotFound, "provider not found")
		}
		return err
	}

	if model == "" {
		return nil
	}
	models, err := deps.Store.ListLLMModelsByProvider(c.Context(), provider.ID)
	if err != nil {
		return err
	}
	for _, m := range models {
		if m.Name == model {
			return nil
		}
	}
	return fiber.NewError(fiber.StatusBadRequest, "model must be one of the provider's models")
}

func newPutLLMPrefsHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req llmPrefsRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		userID := c.Locals(userIDKey{}).(int64)
		ctx := c.Context()

		if req.ProviderID == 0 {
			if err := deps.Store.DeleteUserLLMPrefs(ctx, userID); err != nil {
				return err
			}
			return c.JSON(llmPrefsResponse{})
		}

		if err := checkProviderModel(deps, c, req.ProviderID, req.Model); err != nil {
			return err
		}

		pref, err := deps.Store.UpsertUserLLMPrefs(ctx, db.UpsertUserLLMPrefsParams{
			UserID:     userID,
			ProviderID: sql.NullInt64{Int64: req.ProviderID, Valid: true},
			Model:      strings.TrimSpace(req.Model),
		})
		if err != nil {
			return err
		}
		return c.JSON(llmPrefsResponse{ProviderID: pref.ProviderID.Int64, Model: pref.Model})
	}
}
