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

// LLMResolver serves the admin-managed provider profiles, falling back to the
// server-wide default provider built from env config.
type LLMResolver struct {
	Store   store.Store
	Default llm.Provider
}

// Resolve picks the provider for a run. Precedence: providerID from the chat
// request, then the user's stored default preference, then the global default
// profile, then the server default. model overrides the provider's default
// model when set.
func (r *LLMResolver) Resolve(ctx context.Context, userID, providerID int64, model string) (llm.Provider, error) {
	var profile *db.LlmProvider
	var err error

	if providerID != 0 {
		profile, err = r.byID(ctx, providerID)
	} else {
		profile, err = r.byPref(ctx, userID)
		if err != nil {
			return nil, err
		}
		if profile == nil {
			profile, err = r.byGlobalDefault(ctx)
		}
	}
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return r.Default, nil
	}

	// The user's preference may pin a model as well; a per-request model wins.
	if model == "" {
		if model, err = r.prefModel(ctx, userID, providerID, profile.ID); err != nil {
			return nil, err
		}
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

func (r *LLMResolver) byID(ctx context.Context, providerID int64) (*db.LlmProvider, error) {
	profile, err := r.Store.GetLLMProvider(ctx, providerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("llm provider not found")
		}
		return nil, fmt.Errorf("resolve llm provider: %w", err)
	}
	return &profile, nil
}

func (r *LLMResolver) byPref(ctx context.Context, userID int64) (*db.LlmProvider, error) {
	pref, err := r.Store.GetUserLLMPrefs(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve llm prefs: %w", err)
	}
	// A deleted provider leaves the pref behind with a NULL provider_id
	// (SET NULL); treat it as no preference.
	if !pref.ProviderID.Valid {
		return nil, nil
	}
	profile, err := r.Store.GetLLMProvider(ctx, pref.ProviderID.Int64)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve llm provider: %w", err)
	}
	return &profile, nil
}

func (r *LLMResolver) byGlobalDefault(ctx context.Context) (*db.LlmProvider, error) {
	profile, err := r.Store.GetDefaultLLMProvider(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve default llm provider: %w", err)
	}
	return &profile, nil
}

// prefModel returns the user's preferred model when their pref points at the
// profile actually selected (explicit provider_id calls still honor the pref's
// model as long as it belongs to that provider).
func (r *LLMResolver) prefModel(ctx context.Context, userID, providerID, profileID int64) (string, error) {
	pref, err := r.Store.GetUserLLMPrefs(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("resolve llm prefs: %w", err)
	}
	if !pref.ProviderID.Valid || pref.ProviderID.Int64 != profileID {
		return "", nil
	}
	return pref.Model, nil
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
