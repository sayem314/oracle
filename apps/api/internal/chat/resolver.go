package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store"
)

// LLMResolver serves the user's stored LLM settings when they have any, and
// falls back to the server-wide default provider built from env config.
type LLMResolver struct {
	Store   store.Store
	Default llm.Provider
}

func (r *LLMResolver) Resolve(ctx context.Context, userID int64) (llm.Provider, error) {
	setting, err := r.Store.GetUserSettings(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r.Default, nil
		}
		return nil, fmt.Errorf("resolve llm settings: %w", err)
	}
	if setting.LlmProvider == "" {
		return r.Default, nil
	}
	if setting.LlmProvider == llm.ProviderOpenAI && setting.LlmApiKey == "" {
		return nil, errors.New("llm settings are missing an api key")
	}
	return llm.New(llm.Options{
		Provider: setting.LlmProvider,
		BaseURL:  setting.LlmBaseUrl,
		APIKey:   setting.LlmApiKey,
		Model:    setting.LlmModel,
	})
}
