package server

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/scheduler"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type jobResponse struct {
	ID         int64      `json:"id"`
	SessionID  *int64     `json:"session_id"`
	Schedule   string     `json:"schedule"`
	Prompt     string     `json:"prompt"`
	Enabled    bool       `json:"enabled"`
	Model      string     `json:"model"`
	LastRunAt  *time.Time `json:"last_run_at"`
	LastStatus string     `json:"last_status"`
	NextRunAt  *time.Time `json:"next_run_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func jobIDParam(c fiber.Ctx) (int64, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fiber.NewError(fiber.StatusBadRequest, "invalid job id")
	}
	return id, nil
}

func jobToResponse(j db.Job) jobResponse {
	resp := jobResponse{
		ID:         j.ID,
		Schedule:   j.Schedule,
		Prompt:     j.Prompt,
		Enabled:    j.Enabled == 1,
		LastStatus: j.LastStatus,
		CreatedAt:  j.CreatedAt,
		UpdatedAt:  j.UpdatedAt,
		Model:      j.Model,
	}
	if j.SessionID.Valid {
		resp.SessionID = &j.SessionID.Int64
	}
	if j.LastRunAt.Valid {
		resp.LastRunAt = &j.LastRunAt.Time
	}
	if j.NextRunAt.Valid {
		resp.NextRunAt = &j.NextRunAt.Time
	}
	return resp
}

func newListJobsHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		jobs, err := deps.Store.ListJobs(c.Context())
		if err != nil {
			return err
		}
		out := make([]jobResponse, 0, len(jobs))
		for _, j := range jobs {
			out = append(out, jobToResponse(j))
		}
		return c.JSON(out)
	}
}

type createJobRequest struct {
	Schedule  string `json:"schedule"`
	Prompt    string `json:"prompt"`
	SessionID *int64 `json:"session_id"`
	Model     string `json:"model"`
}

func newCreateJobHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req createJobRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		schedule := strings.TrimSpace(req.Schedule)
		if _, err := scheduler.NextAfter(schedule, time.Now()); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "schedule must be a valid 5-field cron expression")
		}

		prompt := strings.TrimSpace(req.Prompt)
		if prompt == "" {
			return fiber.NewError(fiber.StatusBadRequest, "prompt is required")
		}

		ctx := c.Context()

		var sessionID sql.NullInt64
		if req.SessionID != nil {
			session, err := deps.Store.GetSession(ctx, *req.SessionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fiber.NewError(fiber.StatusNotFound, "session not found")
				}
				return err
			}
			sessionID = sql.NullInt64{Int64: session.ID, Valid: true}
		}

		next, _ := scheduler.NextAfter(schedule, time.Now())

		job, err := deps.Store.CreateJob(ctx, db.CreateJobParams{
			SessionID: sessionID,
			Schedule:  schedule,
			Prompt:    prompt,
			Enabled:   1,
			NextRunAt: sql.NullTime{Time: next, Valid: true},
			Model:     strings.TrimSpace(req.Model),
		})
		if err != nil {
			return err
		}

		return c.Status(fiber.StatusCreated).JSON(jobToResponse(job))
	}
}

type updateJobRequest struct {
	Schedule *string `json:"schedule"`
	Prompt   *string `json:"prompt"`
	Enabled  *bool   `json:"enabled"`
	Model    *string `json:"model"`
}

func newUpdateJobHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := jobIDParam(c)
		if err != nil {
			return err
		}

		var req updateJobRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}

		ctx := c.Context()

		job, err := deps.Store.GetJob(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fiber.NewError(fiber.StatusNotFound, "job not found")
			}
			return err
		}

		schedule := job.Schedule
		if req.Schedule != nil {
			schedule = strings.TrimSpace(*req.Schedule)
		}
		if _, err := scheduler.NextAfter(schedule, time.Now()); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "schedule must be a valid 5-field cron expression")
		}

		prompt := job.Prompt
		if req.Prompt != nil {
			prompt = strings.TrimSpace(*req.Prompt)
			if prompt == "" {
				return fiber.NewError(fiber.StatusBadRequest, "prompt is required")
			}
		}

		enabled := job.Enabled == 1
		if req.Enabled != nil {
			enabled = *req.Enabled
		}

		model := job.Model
		if req.Model != nil {
			model = strings.TrimSpace(*req.Model)
		}

		var enabledInt int64
		var next sql.NullTime
		if enabled {
			enabledInt = 1
			t, _ := scheduler.NextAfter(schedule, time.Now())
			next = sql.NullTime{Time: t, Valid: true}
		}

		updated, err := deps.Store.UpdateJob(ctx, db.UpdateJobParams{
			Schedule:  schedule,
			Prompt:    prompt,
			Enabled:   enabledInt,
			NextRunAt: next,
			Model:     model,
			ID:        job.ID,
		})
		if err != nil {
			return err
		}

		return c.JSON(jobToResponse(updated))
	}
}

func newDeleteJobHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := jobIDParam(c)
		if err != nil {
			return err
		}

		ctx := c.Context()

		job, err := deps.Store.GetJob(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fiber.NewError(fiber.StatusNotFound, "job not found")
			}
			return err
		}

		if err := deps.Store.DeleteJob(ctx, job.ID); err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}
