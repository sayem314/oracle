package server

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type settingsResponse struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	HasAPIKey bool   `json:"has_api_key"`
}

func settingsToResponse(s db.UserSetting) settingsResponse {
	return settingsResponse{
		Provider:  s.LlmProvider,
		BaseURL:   s.LlmBaseUrl,
		Model:     s.LlmModel,
		HasAPIKey: s.LlmApiKey != "",
	}
}

func newGetSettingsHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		userID := c.Locals(userIDKey{}).(int64)
		setting, err := deps.Store.GetUserSettings(c.Context(), userID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return c.JSON(settingsToResponse(setting))
	}
}

type updateSettingsRequest struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// newUpdateSettingsHandler replaces the caller's LLM settings. An empty
// api_key keeps the stored key so the UI never has to round-trip it; an
// all-empty payload clears the settings and restores the server default.
func newUpdateSettingsHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		userID := c.Locals(userIDKey{}).(int64)

		var req updateSettingsRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		provider := strings.TrimSpace(req.Provider)
		baseURL := strings.TrimSpace(req.BaseURL)
		apiKey := strings.TrimSpace(req.APIKey)
		model := strings.TrimSpace(req.Model)

		switch provider {
		case "":
			if baseURL != "" || apiKey != "" || model != "" {
				return fiber.NewError(fiber.StatusBadRequest, "provider is required when other fields are set")
			}
			if err := deps.Store.DeleteUserSettings(c.Context(), userID); err != nil {
				return err
			}
			return c.JSON(settingsResponse{})
		case llm.ProviderOpenAI:
			if baseURL == "" {
				return fiber.NewError(fiber.StatusBadRequest, "base_url is required")
			}
			if model == "" {
				return fiber.NewError(fiber.StatusBadRequest, "model is required")
			}
		default:
			return fiber.NewError(fiber.StatusBadRequest, "provider must be openai")
		}

		if apiKey == "" {
			existing, err := deps.Store.GetUserSettings(c.Context(), userID)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				return fiber.NewError(fiber.StatusBadRequest, "api_key is required")
			}
			if existing.LlmApiKey == "" {
				return fiber.NewError(fiber.StatusBadRequest, "api_key is required")
			}
			apiKey = existing.LlmApiKey
		}

		setting, err := deps.Store.UpsertUserSettings(c.Context(), db.UpsertUserSettingsParams{
			UserID:      userID,
			LlmProvider: provider,
			LlmBaseUrl:  baseURL,
			LlmApiKey:   apiKey,
			LlmModel:    model,
		})
		if err != nil {
			return err
		}
		return c.JSON(settingsToResponse(setting))
	}
}
