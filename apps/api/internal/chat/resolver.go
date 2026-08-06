package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

// LLMResolver serves the user's LLM provider profiles when they have any,
// and falls back to the server-wide default provider built from env config.
type LLMResolver struct {
	Store   store.Store
	Default llm.Provider
}

// Resolve picks the provider for a run. providerID 0 means the user's default
// profile, or the server default when they have none. model overrides the
// profile's default model when set.
func (r *LLMResolver) Resolve(ctx context.Context, userID, providerID int64, model string) (llm.Provider, error) {
	profile, err := r.profile(ctx, userID, providerID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return r.Default, nil
	}

	if model == "" {
		if model, err = r.defaultModel(ctx, profile.ID); err != nil {
			return nil, err
		}
	}
	if model == "" {
		return nil, fmt.Errorf("provider %q has no model configured", profile.Name)
	}

	return llm.New(llm.Options{
		Provider: profile.Provider,
		BaseURL:  profile.BaseUrl,
		APIKey:   profile.ApiKey,
		Model:    model,
	})
}

func (r *LLMResolver) profile(ctx context.Context, userID, providerID int64) (*db.LlmProvider, error) {
	if providerID != 0 {
		profile, err := r.Store.GetLLMProvider(ctx, providerID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errors.New("llm provider not found")
			}
			return nil, fmt.Errorf("resolve llm provider: %w", err)
		}
		if profile.UserID != userID {
			return nil, errors.New("llm provider not found")
		}
		return &profile, nil
	}

	profile, err := r.Store.GetDefaultLLMProvider(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve default llm provider: %w", err)
	}
	return &profile, nil
}

func (r *LLMResolver) defaultModel(ctx context.Context, providerID int64) (string, error) {
	models, err := r.Store.ListLLMModelsByProvider(ctx, providerID)
	if err != nil {
		return "", fmt.Errorf("resolve llm models: %w", err)
	}
	for _, m := range models {
		if m.IsDefault == 1 {
			return m.Name, nil
		}
	}
	return "", nil
}
