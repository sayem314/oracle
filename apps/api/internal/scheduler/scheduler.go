package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

// LoopToolRoundLimit is the model->tool->model round cap for loop iterations,
// higher than the interactive default because a goal may need many tool calls
// to make progress in one iteration.
const LoopToolRoundLimit = 50

// Scheduler polls the sessions table for goal-loop iterations that are due and
// runs them headlessly on the session's conversation. Each session is claimed
// optimistically so a crash or concurrent instance cannot double-run it.
type Scheduler struct {
	store      store.Store
	engine     *chat.Engine
	interval   time.Duration
	runTimeout time.Duration

	cancel context.CancelFunc
	wg     sync.WaitGroup
	done   chan struct{}
}

func New(st store.Store, engine *chat.Engine, interval, runTimeout time.Duration) *Scheduler {
	return &Scheduler{
		store:      st,
		engine:     engine,
		interval:   interval,
		runTimeout: runTimeout,
		done:       make(chan struct{}),
	}
}

// Start launches the poll loop. Runs interrupted by a previous crash are
// recovered first: enabled loops are rescheduled, one-shot runs are marked
// error.
func (s *Scheduler) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	recovered, err := s.store.RecoverStaleLoops(ctx)
	if err != nil {
		log.Error().Err(err).Msg("recover stale loop runs")
	} else if recovered > 0 {
		log.Info().Int64("loops", recovered).Msg("recovered interrupted loop runs")
	}

	go s.loop(ctx)
}

// Stop cancels the poll loop and waits for in-flight iterations to give up on
// the canceled context, bounded by the run timeout.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	<-s.done
	s.wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDue(ctx)
		}
	}
}

func (s *Scheduler) runDue(ctx context.Context) {
	due, err := s.store.ListDueSessionLoops(ctx, sql.NullInt64{Int64: time.Now().Unix(), Valid: true})
	if err != nil {
		log.Error().Err(err).Msg("list due session loops")
		return
	}
	for _, session := range due {
		if ctx.Err() != nil {
			return
		}
		session := session
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.recoverPanic(session.ID)
			s.runOnce(ctx, session)
		}()
	}
}

func (s *Scheduler) recoverPanic(sessionID int64) {
	if r := recover(); r != nil {
		log.Error().Any("panic", r).Int64("session", sessionID).Msg("loop run panicked")
		if err := s.markError(context.Background(), sessionID, fmt.Sprintf("panic: %v", r)); err != nil {
			log.Error().Err(err).Msg("mark loop run error")
		}
	}
}

// runOnce claims the session and runs one iteration. A zero claim count means
// another runner won the race and this call is a no-op.
func (s *Scheduler) runOnce(ctx context.Context, session db.Session) {
	claimed, err := s.store.ClaimSessionLoop(ctx, db.ClaimSessionLoopParams{
		ID:            session.ID,
		LoopNextRunAt: session.LoopNextRunAt,
		LoopLastRunAt: sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
	})
	if err != nil {
		log.Error().Err(err).Int64("session", session.ID).Msg("claim session loop")
		return
	}
	if claimed == 0 {
		return
	}
	log.Info().Int64("session", session.ID).Msg("loop run started")

	runCtx, cancel := context.WithTimeout(ctx, s.runTimeout)
	defer cancel()

	engine := s.engine.AsHeadless()
	engine.ToolRoundLimit = LoopToolRoundLimit

	history, err := engine.BuildHistory(runCtx, session.ID)
	if err != nil {
		s.finish(runCtx, session.ID, err)
		return
	}
	s.finish(runCtx, session.ID, engine.Run(runCtx, chat.DiscardSink{}, session.ID, llm.Request{
		Model:    "",
		Messages: history,
	}))
}

// finish records the iteration outcome and schedules the next run. Enabled
// loops keep retrying at the interval on error; a loop disabled mid-run just
// stops and its one-shot run is left as-is. The status write uses a fresh
// context so a canceled or timed-out run still records its outcome.
func (s *Scheduler) finish(ctx context.Context, sessionID int64, runErr error) {
	writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status, loopError := chat.StatusDone, ""
	if runErr != nil {
		status, loopError = chat.StatusError, runErr.Error()
	}

	var next sql.NullInt64
	current, err := s.store.GetSession(writeCtx, sessionID)
	if err != nil {
		status, loopError = chat.StatusError, "loop state unavailable after run"
	} else if current.LoopEnabled == 1 {
		next = sql.NullInt64{Int64: time.Now().Add(IntervalOf(current.LoopInterval)).Unix(), Valid: true}
	}

	if err := s.store.UpdateSessionLoopResult(writeCtx, db.UpdateSessionLoopResultParams{
		ID:             sessionID,
		LoopLastStatus: status,
		LoopError:      loopError,
		LoopNextRunAt:  next,
	}); err != nil {
		log.Error().Err(err).Int64("session", sessionID).Msg("update loop result")
		return
	}

	switch {
	case runErr == nil:
		log.Info().Int64("session", sessionID).Msg("loop run finished")
	case errors.Is(runErr, context.Canceled):
		log.Info().Int64("session", sessionID).Msg("loop run canceled")
	default:
		log.Warn().Err(runErr).Int64("session", sessionID).Msg("loop run finished with error")
	}
}

// markError writes an error status for a run that died without a usable
// context, used from panic recovery.
func (s *Scheduler) markError(ctx context.Context, sessionID int64, msg string) error {
	return s.store.UpdateSessionLoopResult(ctx, db.UpdateSessionLoopResultParams{
		ID:             sessionID,
		LoopLastStatus: chat.StatusError,
		LoopError:      msg,
		LoopNextRunAt:  sql.NullInt64{},
	})
}

// IntervalOf parses a loop interval; empty, zero, or unparsable values mean
// continuous (run again immediately).
func IntervalOf(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return 0
	}
	return d
}
