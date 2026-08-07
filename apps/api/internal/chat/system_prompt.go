package chat

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sayem314/oracle/apps/api/internal/store"
)

const baseSystemPrompt = `You are oracle, a self-hosted personal assistant. You answer questions and carry out tasks on the user's behalf using the tools available to you.

Behavior:
- Be concise and direct. Reply in the language the user writes in.
- Prefer the tools over guessing: check facts, read files, and fetch web content when the answer depends on them.
- When a tool fails or is denied by policy, say so plainly and suggest an alternative when one exists.
- Never claim work you did not do or results you did not observe.`

// DetectEnvironment renders the runtime facts the model needs to anchor
// relative paths and answer where-am-I questions. Process-static, built once.
func DetectEnvironment() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "(unavailable)"
	}
	return fmt.Sprintf("Environment:\n- OS: %s/%s\n- Working directory: %s\n- Timezone: %s\n\nRelative paths in the file and exec tools resolve against the working directory.",
		runtime.GOOS, runtime.GOARCH, cwd, currentTimezone())
}

func currentTimezone() string {
	zone, offset := time.Now().Zone()
	if offset == 0 {
		return zone
	}
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	return fmt.Sprintf("%s (UTC%s%02d:%02d)", zone, sign, offset/3600, offset%3600/60)
}

// buildSystemPrompt composes the system prompt from the base text, the
// environment block, and the administrator-configured instructions stored in
// settings, when present.
func buildSystemPrompt(ctx context.Context, st store.Store, env string) string {
	prompt := baseSystemPrompt
	if env != "" {
		prompt += "\n\n" + env
	}
	if st == nil {
		return prompt
	}
	settings, err := st.GetSettings(ctx)
	if err != nil {
		return prompt
	}
	instructions := strings.TrimSpace(settings.Instructions)
	if instructions == "" {
		return prompt
	}
	return prompt + "\n\nAdministrator instructions:\n" + instructions
}
