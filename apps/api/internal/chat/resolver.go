package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store"
)

// LLMResolver serves the single global LLM provider, falling back to the
// server-wide default provider built from env config.
type LLMResolver struct {
	Store   store.Store
	Default llm.Provider
}

// Resolve picks the provider for a run from the singleton global profile. A
// non-empty model overrides the profile's stored model (e.g. a job pin).
func (r *LLMResolver) Resolve(ctx context.Context, model string) (llm.Provider, error) {
	profile, err := r.Store.GetLLMProvider(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r.Default, nil
		}
		return nil, fmt.Errorf("resolve llm provider: %w", err)
	}

	if model == "" {
		if model = profile.Model; model == "" {
			return nil, errors.New("no model configured for the LLM provider")
		}
	}

	return llm.New(llm.Options{
		Provider: profile.Provider,
		BaseURL:  profile.BaseUrl,
		APIKey:   profile.ApiKey,
		Model:    model,
	})
}
