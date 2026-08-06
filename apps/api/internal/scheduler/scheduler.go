package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"

	"github.com/sayem314/oracle/apps/api/internal/chat"
	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

const (
	statusOK    = "ok"
	statusError = "error"
)

// DefaultInterval is how often the scheduler polls for due jobs.
const DefaultInterval = 30 * time.Second

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// NextAfter returns the first activation of a standard 5-field cron schedule
// strictly after from.
func NextAfter(schedule string, from time.Time) (time.Time, error) {
	sched, err := parser.Parse(schedule)
	if err != nil {
		return time.Time{}, fmt.Errorf("scheduler: invalid schedule %q: %w", schedule, err)
	}
	return sched.Next(from), nil
}

// Scheduler runs due jobs on an interval. State lives entirely in the jobs
// table, so a restart simply picks up where it left off.
type Scheduler struct {
	store    store.Store
	engine   *chat.Engine
	interval time.Duration
	now      func() time.Time

	stop chan struct{}
	wg   sync.WaitGroup
}

func New(s store.Store, engine *chat.Engine, interval time.Duration) *Scheduler {
	return &Scheduler{
		store:    s,
		engine:   engine,
		interval: interval,
		now:      time.Now,
		stop:     make(chan struct{}),
	}
}

func (s *Scheduler) Start() {
	s.wg.Add(1)
	go s.loop()
}

func (s *Scheduler) Stop() {
	close(s.stop)
	s.wg.Wait()
}

func (s *Scheduler) loop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			if err := s.RunOnce(context.Background()); err != nil {
				log.Error().Err(err).Msg("scheduler tick failed")
			}
		}
	}
}

// RunOnce claims and executes every job whose next_run_at has arrived. It is
// one tick of the loop, exposed so tests can drive it deterministically.
func (s *Scheduler) RunOnce(ctx context.Context) error {
	now := s.now()
	jobs, err := s.store.ListDueJobs(ctx, sql.NullTime{Time: now, Valid: true})
	if err != nil {
		return fmt.Errorf("list due jobs: %w", err)
	}
	for _, job := range jobs {
		s.runJob(ctx, now, job)
	}
	return nil
}

func (s *Scheduler) runJob(ctx context.Context, now time.Time, job db.Job) {
	next, err := NextAfter(job.Schedule, now)
	if err != nil {
		log.Error().Err(err).Int64("job", job.ID).Msg("scheduler: cannot compute next run")
		s.setStatus(ctx, job.ID, statusError)
		return
	}

	claimed, err := s.store.ClaimJob(ctx, db.ClaimJobParams{
		NewNextRunAt:      sql.NullTime{Time: next, Valid: true},
		LastRunAt:         sql.NullTime{Time: now, Valid: true},
		ID:                job.ID,
		ExpectedNextRunAt: job.NextRunAt,
	})
	if err != nil {
		log.Error().Err(err).Int64("job", job.ID).Msg("scheduler: claim job")
		return
	}
	if claimed == 0 {
		return
	}

	log.Info().Int64("job", job.ID).Msg("scheduler: running job")
	status := statusOK
	if err := s.execute(ctx, job); err != nil {
		log.Error().Err(err).Int64("job", job.ID).Msg("scheduler: job run failed")
		status = statusError
	}
	s.setStatus(ctx, job.ID, status)
}

func (s *Scheduler) execute(ctx context.Context, job db.Job) error {
	sessionID, err := s.resolveSession(ctx, job)
	if err != nil {
		return fmt.Errorf("resolve session: %w", err)
	}

	if _, err := s.store.AppendMessage(ctx, db.AppendMessageParams{
		SessionID: sessionID,
		Role:      string(llm.RoleUser),
		Content:   job.Prompt,
	}); err != nil {
		return fmt.Errorf("append prompt: %w", err)
	}

	history, err := s.engine.BuildHistory(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("build history: %w", err)
	}

	if err := s.engine.Run(ctx, chat.DiscardSink{}, sessionID, llm.Request{Model: job.Model, Messages: history}); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	return nil
}

func (s *Scheduler) resolveSession(ctx context.Context, job db.Job) (int64, error) {
	if job.SessionID.Valid {
		return job.SessionID.Int64, nil
	}
	session, err := s.store.CreateSession(ctx, "")
	if err != nil {
		return 0, err
	}
	if err := s.store.SetJobSession(ctx, db.SetJobSessionParams{
		SessionID: sql.NullInt64{Int64: session.ID, Valid: true},
		ID:        job.ID,
	}); err != nil {
		return 0, err
	}
	return session.ID, nil
}

func (s *Scheduler) setStatus(ctx context.Context, jobID int64, status string) {
	if err := s.store.SetJobStatus(ctx, db.SetJobStatusParams{ID: jobID, LastStatus: status}); err != nil {
		log.Error().Err(err).Int64("job", jobID).Msg("scheduler: set job status")
	}
}
