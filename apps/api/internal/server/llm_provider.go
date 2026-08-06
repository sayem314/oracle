package server

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type providerResponse struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	HasAPIKey bool   `json:"has_api_key"`
	Model     string `json:"model"`
}

func providerToResponse(p db.LlmProvider) providerResponse {
	return providerResponse{
		Provider:  p.Provider,
		BaseURL:   p.BaseUrl,
		HasAPIKey: p.ApiKey != "",
		Model:     p.Model,
	}
}

type llmProviderRequest struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

func (req llmProviderRequest) normalize() (string, string, error) {
	provider := strings.TrimSpace(req.Provider)
	switch provider {
	case llm.ProviderOpenAI:
		if strings.TrimSpace(req.BaseURL) == "" {
			return "", "", fiber.NewError(fiber.StatusBadRequest, "base_url is required")
		}
	case llm.ProviderMock:
	default:
		return "", "", fiber.NewError(fiber.StatusBadRequest, "provider must be mock or openai")
	}
	return provider, strings.TrimSpace(req.BaseURL), nil
}

func newGetLLMProviderHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		provider, err := deps.Store.GetLLMProvider(c.Context())
		if err != nil {
			return err
		}
		return c.JSON(providerToResponse(provider))
	}
}

func newUpsertLLMProviderHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req llmProviderRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
		provider, baseURL, err := req.normalize()
		if err != nil {
			return err
		}
		apiKey := strings.TrimSpace(req.APIKey)
		model := strings.TrimSpace(req.Model)

		// Keep the stored key when the form leaves it blank so an unrelated
		// edit does not wipe credentials.
		existing, err := deps.Store.GetLLMProvider(c.Context())
		if err == nil && apiKey == "" {
			apiKey = existing.ApiKey
		}

		updated, err := deps.Store.UpsertLLMProvider(c.Context(), db.UpsertLLMProviderParams{
			Provider: provider,
			BaseUrl:  baseURL,
			ApiKey:   apiKey,
			Model:    model,
		})
		if err != nil {
			return err
		}
		return c.JSON(providerToResponse(updated))
	}
}

// newSetLLMModelHandler updates just the active model so the chat picker can
// switch it instance-wide without resubmitting credentials.
func newSetLLMModelHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			Model string `json:"model"`
		}
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
		model := strings.TrimSpace(req.Model)
		if model == "" {
			return fiber.NewError(fiber.StatusBadRequest, "model is required")
		}
		updated, err := deps.Store.SetLLMModel(c.Context(), model)
		if err != nil {
			return err
		}
		return c.JSON(providerToResponse(updated))
	}
}

func newFetchLLMModelsHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		provider, err := deps.Store.GetLLMProvider(c.Context())
		if err != nil {
			return err
		}
		models, err := llm.ListModels(c.Context(), provider.BaseUrl, provider.ApiKey)
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, "failed to fetch models from gateway: "+err.Error())
		}
		return c.JSON(fiber.Map{"models": models})
	}
}
