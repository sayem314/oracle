package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

// sessionLoopControl is the set_loop tool's view of the running session,
// mirroring the session PATCH handler's loop semantics.
type sessionLoopControl struct {
	store     store.Store
	sessionID int64
}

func (c *sessionLoopControl) SetLoop(ctx context.Context, enabled *bool, interval *string) (string, error) {
	session, err := c.store.GetSession(ctx, c.sessionID)
	if err != nil {
		return "", fmt.Errorf("set_loop: load session: %w", err)
	}

	loopEnabled := session.LoopEnabled == 1
	if enabled != nil {
		loopEnabled = *enabled
	}
	loopInterval := session.LoopInterval
	if interval != nil {
		if err := ValidateLoopInterval(*interval); err != nil {
			return "", err
		}
		loopInterval = *interval
	}

	var nextRunAt sql.NullInt64
	switch {
	case enabled == nil:
		nextRunAt = session.LoopNextRunAt
	case loopEnabled:
		nextRunAt = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}
	default:
		nextRunAt = sql.NullInt64{}
	}

	if err := c.store.UpdateSessionLoop(ctx, db.UpdateSessionLoopParams{
		ID:            session.ID,
		LoopEnabled:   LoopEnabledInt(loopEnabled),
		LoopInterval:  loopInterval,
		LoopNextRunAt: nextRunAt,
	}); err != nil {
		return "", fmt.Errorf("set_loop: update session: %w", err)
	}

	state := "disabled"
	if loopEnabled {
		label := strings.TrimSpace(loopInterval)
		if label == "" {
			label = "continuous"
		}
		state = "enabled, interval " + label + ", next run scheduled now"
	}
	return "session loop " + state, nil
}

// LoopEnabledInt maps a loop enabled flag to the sessions column value.
func LoopEnabledInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// ValidateLoopInterval accepts an empty string (continuous) or a Go duration
// like "30s" or "5m". Shared by the session PATCH handler and the set_loop
// tool so both surfaces reject the same input.
func ValidateLoopInterval(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return errors.New("loop_interval must be a duration like \"30s\" or \"5m\"")
	}
	return nil
}
