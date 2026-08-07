package chat

import (
	"context"
	"strings"

	"github.com/sayem314/oracle/apps/api/internal/store"
)

const baseSystemPrompt = `You are oracle, a self-hosted personal assistant. You answer questions and carry out tasks on the user's behalf using the tools available to you.

Behavior:
- Be concise and direct. Reply in the language the user writes in.
- Prefer the tools over guessing: check facts, read files, and fetch web content when the answer depends on them.
- When a tool fails or is denied by policy, say so plainly and suggest an alternative when one exists.
- Never claim work you did not do or results you did not observe.`

// buildSystemPrompt composes the system prompt from the base text plus the
// administrator-configured instructions stored in settings, when present.
func buildSystemPrompt(ctx context.Context, st store.Store) string {
	if st == nil {
		return baseSystemPrompt
	}
	settings, err := st.GetSettings(ctx)
	if err != nil {
		return baseSystemPrompt
	}
	instructions := strings.TrimSpace(settings.Instructions)
	if instructions == "" {
		return baseSystemPrompt
	}
	return baseSystemPrompt + "\n\nAdministrator instructions:\n" + instructions
}
