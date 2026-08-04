package server_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type jobResponse struct {
	ID         int64      `json:"id"`
	SessionID  *int64     `json:"session_id"`
	Schedule   string     `json:"schedule"`
	Prompt     string     `json:"prompt"`
	Enabled    bool       `json:"enabled"`
	LastStatus string     `json:"last_status"`
	NextRunAt  *time.Time `json:"next_run_at"`
}

func doJobsRequest(t *testing.T, app *fiber.App, method, path, cookie string, body any) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func createJob(t *testing.T, app *fiber.App, cookie string, body map[string]any) *http.Response {
	t.Helper()
	return doJobsRequest(t, app, http.MethodPost, "/api/v1/jobs", cookie, body)
}

func mustCreateJob(t *testing.T, app *fiber.App, cookie, schedule, prompt string) jobResponse {
	t.Helper()
	res := createJob(t, app, cookie, map[string]any{"schedule": schedule, "prompt": prompt})
	require.Equal(t, http.StatusCreated, res.StatusCode)
	return decodeJob(t, res)
}

func decodeJob(t *testing.T, res *http.Response) jobResponse {
	t.Helper()
	var j jobResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&j))
	return j
}

func decodeJobList(t *testing.T, res *http.Response) []jobResponse {
	t.Helper()
	var jobs []jobResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&jobs))
	return jobs
}

func TestCreateJob(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	res := createJob(t, app, cookie, map[string]any{
		"schedule": "0 8 * * *",
		"prompt":   "morning briefing",
	})
	require.Equal(t, http.StatusCreated, res.StatusCode)

	job := decodeJob(t, res)
	assert.Positive(t, job.ID)
	assert.Equal(t, "0 8 * * *", job.Schedule)
	assert.Equal(t, "morning briefing", job.Prompt)
	assert.True(t, job.Enabled)
	require.NotNil(t, job.NextRunAt)
	assert.True(t, job.NextRunAt.After(time.Now()))
	assert.Nil(t, job.SessionID)
}

func TestCreateJobValidation(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	t.Run("invalid schedule", func(t *testing.T) {
		res := createJob(t, app, cookie, map[string]any{"schedule": "nope", "prompt": "hi"})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "schedule must be a valid 5-field cron expression", decodeErrorMessage(t, res))
	})

	t.Run("empty prompt", func(t *testing.T) {
		res := createJob(t, app, cookie, map[string]any{"schedule": "0 8 * * *", "prompt": "   "})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "prompt is required", decodeErrorMessage(t, res))
	})

	t.Run("unauthenticated", func(t *testing.T) {
		res := createJob(t, app, "", map[string]any{"schedule": "0 8 * * *", "prompt": "hi"})
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})
}

func TestCreateJobSessionOwnership(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, userID := signUp(t, app, dbConn, "owner@example.com")

	t.Run("own session", func(t *testing.T) {
		session, err := s.CreateSession(t.Context(), db.CreateSessionParams{UserID: userID})
		require.NoError(t, err)

		res := createJob(t, app, cookie, map[string]any{
			"schedule":   "0 8 * * *",
			"prompt":     "hi",
			"session_id": session.ID,
		})
		require.Equal(t, http.StatusCreated, res.StatusCode)
		job := decodeJob(t, res)
		require.NotNil(t, job.SessionID)
		assert.Equal(t, session.ID, *job.SessionID)
	})

	t.Run("unknown session", func(t *testing.T) {
		res := createJob(t, app, cookie, map[string]any{
			"schedule":   "0 8 * * *",
			"prompt":     "hi",
			"session_id": 99999,
		})
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
	})

	t.Run("someone else's session", func(t *testing.T) {
		seedUser(t, dbConn, 42)
		session, err := s.CreateSession(t.Context(), db.CreateSessionParams{UserID: 42})
		require.NoError(t, err)

		res := createJob(t, app, cookie, map[string]any{
			"schedule":   "0 8 * * *",
			"prompt":     "hi",
			"session_id": session.ID,
		})
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
	})
}

func TestListJobsIsolation(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	require.Equal(t, http.StatusCreated, createJob(t, app, cookie, map[string]any{"schedule": "0 8 * * *", "prompt": "mine"}).StatusCode)

	// A second user is seeded directly (sign-up locks after the first user) and
	// gets a job through the store, bypassing the API.
	seedUser(t, dbConn, 42)
	_, err := s.CreateJob(t.Context(), db.CreateJobParams{
		UserID:    42,
		Schedule:  "0 9 * * *",
		Prompt:    "theirs",
		Enabled:   1,
		NextRunAt: nullTimeIn(24 * time.Hour),
	})
	require.NoError(t, err)

	res := doJobsRequest(t, app, http.MethodGet, "/api/v1/jobs", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	jobs := decodeJobList(t, res)
	require.Len(t, jobs, 1)
	assert.Equal(t, "mine", jobs[0].Prompt)
}

func TestUpdateJob(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	created := mustCreateJob(t, app, cookie, "0 8 * * *", "hi")

	t.Run("disable clears next run", func(t *testing.T) {
		res := doJobsRequest(t, app, http.MethodPatch, "/api/v1/jobs/"+itoa(created.ID), cookie, map[string]any{"enabled": false})
		require.Equal(t, http.StatusOK, res.StatusCode)
		job := decodeJob(t, res)
		assert.False(t, job.Enabled)
		assert.Nil(t, job.NextRunAt)
	})

	t.Run("re-enable recomputes next run", func(t *testing.T) {
		res := doJobsRequest(t, app, http.MethodPatch, "/api/v1/jobs/"+itoa(created.ID), cookie, map[string]any{"enabled": true})
		require.Equal(t, http.StatusOK, res.StatusCode)
		job := decodeJob(t, res)
		assert.True(t, job.Enabled)
		require.NotNil(t, job.NextRunAt)
		assert.True(t, job.NextRunAt.After(time.Now()))
	})

	t.Run("invalid schedule", func(t *testing.T) {
		res := doJobsRequest(t, app, http.MethodPatch, "/api/v1/jobs/"+itoa(created.ID), cookie, map[string]any{"schedule": "nope"})
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("unknown job", func(t *testing.T) {
		res := doJobsRequest(t, app, http.MethodPatch, "/api/v1/jobs/99999", cookie, map[string]any{"enabled": false})
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
	})
}

func TestUpdateJobOtherUser(t *testing.T) {
	app, s, dbConn := newTestApp(t, llm.NewMock())
	intruderCookie, _ := signUp(t, app, dbConn, "intruder@example.com")

	// The victim is seeded directly and owns a job created through the store.
	seedUser(t, dbConn, 42)
	victimJob, err := s.CreateJob(t.Context(), db.CreateJobParams{
		UserID:    42,
		Schedule:  "0 8 * * *",
		Prompt:    "victim",
		Enabled:   1,
		NextRunAt: nullTimeIn(24 * time.Hour),
	})
	require.NoError(t, err)

	res := doJobsRequest(t, app, http.MethodPatch, "/api/v1/jobs/"+itoa(victimJob.ID), intruderCookie, map[string]any{"enabled": false})
	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	res = doJobsRequest(t, app, http.MethodDelete, "/api/v1/jobs/"+itoa(victimJob.ID), intruderCookie, nil)
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestDeleteJob(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "owner@example.com")

	created := mustCreateJob(t, app, cookie, "0 8 * * *", "hi")

	res := doJobsRequest(t, app, http.MethodDelete, "/api/v1/jobs/"+itoa(created.ID), cookie, nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)

	res = doJobsRequest(t, app, http.MethodGet, "/api/v1/jobs", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Empty(t, decodeJobList(t, res))

	res = doJobsRequest(t, app, http.MethodDelete, "/api/v1/jobs/"+itoa(created.ID), cookie, nil)
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}

func nullTimeIn(d time.Duration) sql.NullTime {
	return sql.NullTime{Time: time.Now().Add(d), Valid: true}
}
