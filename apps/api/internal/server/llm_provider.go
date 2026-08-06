package server

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type providerResponse struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Provider     string   `json:"provider"`
	BaseURL      string   `json:"base_url"`
	HasAPIKey    bool     `json:"has_api_key"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"default_model"`
	Default      bool     `json:"default"`
}

func (deps Deps) providerToResponse(ctx context.Context, p db.LlmProvider) (providerResponse, error) {
	models, err := deps.Store.ListLLMModelsByProvider(ctx, p.ID)
	if err != nil {
		return providerResponse{}, err
	}
	names := make([]string, 0, len(models))
	var defaultModel string
	for _, m := range models {
		names = append(names, m.Name)
		if m.IsDefault == 1 {
			defaultModel = m.Name
		}
	}
	return providerResponse{
		ID:           p.ID,
		Name:         p.Name,
		Provider:     p.Provider,
		BaseURL:      p.BaseUrl,
		HasAPIKey:    p.ApiKey != "",
		Models:       names,
		DefaultModel: defaultModel,
		Default:      p.IsDefault == 1,
	}, nil
}

type llmProviderRequest struct {
	Name         string   `json:"name"`
	Provider     string   `json:"provider"`
	BaseURL      string   `json:"base_url"`
	APIKey       string   `json:"api_key"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"default_model"`
	Default      bool     `json:"default"`
}

func (req llmProviderRequest) normalize() (string, string, []string, string, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return "", "", nil, "", fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	provider := strings.TrimSpace(req.Provider)
	if provider != llm.ProviderOpenAI {
		return "", "", nil, "", fiber.NewError(fiber.StatusBadRequest, "provider must be openai")
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		return "", "", nil, "", fiber.NewError(fiber.StatusBadRequest, "base_url is required")
	}

	seen := make(map[string]bool)
	models := make([]string, 0, len(req.Models))
	for _, m := range req.Models {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		models = append(models, m)
	}

	defaultModel := strings.TrimSpace(req.DefaultModel)
	if defaultModel != "" && !seen[defaultModel] {
		return "", "", nil, "", fiber.NewError(fiber.StatusBadRequest, "default_model must be one of models")
	}
	return name, baseURL, models, defaultModel, nil
}

// resolveProvider loads a provider or returns 404 so existence is never leaked.
func resolveProvider(deps Deps, c fiber.Ctx) (db.LlmProvider, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return db.LlmProvider{}, fiber.NewError(fiber.StatusBadRequest, "invalid provider id")
	}

	provider, err := deps.Store.GetLLMProvider(c.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.LlmProvider{}, fiber.NewError(fiber.StatusNotFound, "provider not found")
		}
		return db.LlmProvider{}, err
	}
	return provider, nil
}

func newListLLMProvidersHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		providers, err := deps.Store.ListLLMProviders(c.Context())
		if err != nil {
			return err
		}
		out := make([]providerResponse, 0, len(providers))
		for _, p := range providers {
			res, err := deps.providerToResponse(c.Context(), p)
			if err != nil {
				return err
			}
			out = append(out, res)
		}
		return c.JSON(out)
	}
}

func newCreateLLMProviderHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req llmProviderRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
		name, baseURL, models, defaultModel, err := req.normalize()
		if err != nil {
			return err
		}
		apiKey := strings.TrimSpace(req.APIKey)
		if apiKey == "" {
			return fiber.NewError(fiber.StatusBadRequest, "api_key is required")
		}

		ctx := c.Context()

		// The first profile on the instance becomes the global default.
		existing, err := deps.Store.ListLLMProviders(ctx)
		if err != nil {
			return err
		}
		isDefault := req.Default || len(existing) == 0
		if isDefault {
			if err := deps.Store.ClearDefaultLLMProviders(ctx); err != nil {
				return err
			}
		}

		created, err := deps.Store.CreateLLMProvider(ctx, db.CreateLLMProviderParams{
			Name:      name,
			Provider:  req.Provider,
			BaseUrl:   baseURL,
			ApiKey:    apiKey,
			IsDefault: boolToInt(isDefault),
		})
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fiber.NewError(fiber.StatusConflict, "a provider with this name already exists")
			}
			return err
		}

		if err := insertModels(deps, ctx, created.ID, models, defaultModel); err != nil {
			return err
		}

		res, err := deps.providerToResponse(ctx, created)
		if err != nil {
			return err
		}
		return c.Status(fiber.StatusCreated).JSON(res)
	}
}

func newUpdateLLMProviderHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		provider, err := resolveProvider(deps, c)
		if err != nil {
			return err
		}

		var req llmProviderRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
		name, baseURL, models, defaultModel, err := req.normalize()
		if err != nil {
			return err
		}

		apiKey := strings.TrimSpace(req.APIKey)
		if apiKey == "" {
			apiKey = provider.ApiKey
		}

		ctx := c.Context()

		// default:true promotes this profile; false leaves the flag untouched so
		// an edit cannot silently drop the global default.
		isDefault := provider.IsDefault
		if req.Default {
			if err := deps.Store.ClearDefaultLLMProviders(ctx); err != nil {
				return err
			}
			isDefault = 1
		}

		updated, err := deps.Store.UpdateLLMProvider(ctx, db.UpdateLLMProviderParams{
			Name:      name,
			Provider:  req.Provider,
			BaseUrl:   baseURL,
			ApiKey:    apiKey,
			IsDefault: isDefault,
			ID:        provider.ID,
		})
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fiber.NewError(fiber.StatusConflict, "a provider with this name already exists")
			}
			return err
		}

		if err := deps.Store.DeleteLLMModelsByProvider(ctx, provider.ID); err != nil {
			return err
		}
		if err := insertModels(deps, ctx, provider.ID, models, defaultModel); err != nil {
			return err
		}

		res, err := deps.providerToResponse(ctx, updated)
		if err != nil {
			return err
		}
		return c.JSON(res)
	}
}

func newDeleteLLMProviderHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		provider, err := resolveProvider(deps, c)
		if err != nil {
			return err
		}
		if err := deps.Store.DeleteLLMProvider(c.Context(), provider.ID); err != nil {
			return err
		}
		// Deleting the global default promotes the next profile instead of
		// leaving the instance with no default at all.
		if provider.IsDefault == 1 {
			if _, err := deps.Store.PromoteNextLLMProvider(c.Context()); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

func newFetchLLMProviderModelsHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		provider, err := resolveProvider(deps, c)
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

func insertModels(deps Deps, ctx context.Context, providerID int64, models []string, defaultModel string) error {
	for _, m := range models {
		if err := deps.Store.InsertLLMModel(ctx, db.InsertLLMModelParams{
			ProviderID: providerID,
			Name:       m,
			IsDefault:  boolToInt(m == defaultModel),
		}); err != nil {
			return err
		}
	}
	return nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
