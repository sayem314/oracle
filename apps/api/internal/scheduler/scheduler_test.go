package scheduler_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/permission"
	"github.com/sayem314/oracle/apps/api/internal/scheduler"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type noTools struct{}

func (noTools) Definitions() []llm.Tool { return nil }

func (noTools) Execute(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("no tools registered")
}

// blockingProvider stalls every request until the run context is canceled.
type blockingProvider struct {
	started chan struct{}
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{started: make(chan struct{}, 1)}
}

func (p *blockingProvider) Chat(ctx context.Context, _ llm.Request) (llm.Stream, error) {
	select {
	case p.started <- struct{}{}:
	default:
	}
	return &blockingStream{done: ctx.Done()}, nil
}

type blockingStream struct {
	done <-chan struct{}
}

func (s *blockingStream) Next() bool {
	<-s.done
	return false
}

func (s *blockingStream) Current() llm.Chunk { return llm.Chunk{} }

func (s *blockingStream) Err() error { return nil }

func (s *blockingStream) Close() error { return nil }

type panicProvider struct{}

func (panicProvider) Chat(context.Context, llm.Request) (llm.Stream, error) {
	panic("boom")
}

func newLoopStore(t *testing.T) (store.Store, *chat.Engine) {
	t.Helper()

	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	dbConn, err := store.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	applied, err := store.Migrate(dbConn)
	require.NoError(t, err)
	require.Positive(t, applied)

	st := store.New(dbConn)
	resolver := &chat.LLMResolver{Store: st, Default: llm.NewMock()}
	engine := &chat.Engine{
		Store:       st,
		LLM:         resolver,
		Tools:       noTools{},
		Permissions: permission.NewRuleset(permission.Allow, nil),
	}
	return st, engine
}

// queueLoop creates a session whose loop is due now.
func queueLoop(t *testing.T, st store.Store, enabled int64, interval string) db.Session {
	t.Helper()
	session, err := st.CreateSession(t.Context(), "loop")
	require.NoError(t, err)
	require.NoError(t, st.UpdateSessionLoop(t.Context(), db.UpdateSessionLoopParams{
		ID:            session.ID,
		LoopEnabled:   enabled,
		LoopInterval:  interval,
		LoopNextRunAt: sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
	}))
	return session
}

func TestStartRunsDueLoopAndSchedulesNext(t *testing.T) {
	st, engine := newLoopStore(t)
	session := queueLoop(t, st, 1, "30s")

	loops := scheduler.New(st, engine, 10*time.Millisecond, time.Minute)
	loops.Start()
	defer loops.Stop()

	require.Eventually(t, func() bool {
		got, err := st.GetSession(t.Context(), session.ID)
		return err == nil && got.LoopLastStatus == "done"
	}, 5*time.Second, 20*time.Millisecond)

	got, err := st.GetSession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Empty(t, got.LoopError)
	assert.True(t, got.LoopNextRunAt.Valid, "enabled loop should schedule the next iteration")
}

func TestRunOnceOnDisabledLoopStaysIdle(t *testing.T) {
	st, engine := newLoopStore(t)
	session := queueLoop(t, st, 0, "")

	loops := scheduler.New(st, engine, 10*time.Millisecond, time.Minute)
	loops.Start()
	defer loops.Stop()

	require.Eventually(t, func() bool {
		got, err := st.GetSession(t.Context(), session.ID)
		return err == nil && got.LoopLastStatus == "done"
	}, 5*time.Second, 20*time.Millisecond)

	got, err := st.GetSession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.False(t, got.LoopNextRunAt.Valid, "one-shot run must not reschedule")
}

func TestStartRecoversStaleRuns(t *testing.T) {
	st, engine := newLoopStore(t)
	session := queueLoop(t, st, 1, "30s")
	require.NoError(t, st.UpdateSessionLoopResult(t.Context(), db.UpdateSessionLoopResultParams{
		ID:             session.ID,
		LoopLastStatus: "running",
		LoopError:      "",
		LoopNextRunAt:  sql.NullInt64{},
	}))

	loops := scheduler.New(st, engine, 10*time.Millisecond, time.Minute)
	loops.Start()
	defer loops.Stop()

	// Recovery marks the stale run and reschedules the enabled loop, which
	// then runs to completion.
	require.Eventually(t, func() bool {
		got, err := st.GetSession(t.Context(), session.ID)
		return err == nil && got.LoopLastStatus == "done"
	}, 5*time.Second, 20*time.Millisecond)
}

func TestStopCancelsInFlightRun(t *testing.T) {
	st, engine := newLoopStore(t)
	session := queueLoop(t, st, 0, "")
	provider := newBlockingProvider()
	engine.LLM = &chat.LLMResolver{Store: st, Default: provider}

	loops := scheduler.New(st, engine, 10*time.Millisecond, time.Minute)
	loops.Start()

	require.Eventually(t, func() bool {
		select {
		case <-provider.started:
			return true
		default:
			return false
		}
	}, 5*time.Second, 20*time.Millisecond)

	start := time.Now()
	loops.Stop()
	assert.Less(t, time.Since(start), 30*time.Second)

	got, err := st.GetSession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, "error", got.LoopLastStatus)
}

func TestPanicInRunMarksError(t *testing.T) {
	st, engine := newLoopStore(t)
	session := queueLoop(t, st, 0, "")
	engine.LLM = &chat.LLMResolver{Store: st, Default: panicProvider{}}

	loops := scheduler.New(st, engine, 10*time.Millisecond, time.Minute)
	loops.Start()
	defer loops.Stop()

	require.Eventually(t, func() bool {
		got, err := st.GetSession(t.Context(), session.ID)
		return err == nil && got.LoopLastStatus == "error"
	}, 5*time.Second, 20*time.Millisecond)

	got, err := st.GetSession(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Contains(t, got.LoopError, "panic")
	assert.False(t, got.LoopNextRunAt.Valid)
}

func TestIntervalOf(t *testing.T) {
	tests := []struct {
		raw  string
		want time.Duration
	}{
		{"", 0},
		{"   ", 0},
		{"0", 0},
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h30m", 90 * time.Minute},
		{"-5s", 0},
		{"every day", 0},
	}
	for _, tc := range tests {
		assert.Equalf(t, tc.want, scheduler.IntervalOf(tc.raw), "interval %q", tc.raw)
	}
}
