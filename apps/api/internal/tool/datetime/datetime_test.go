package datetime_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/tool"
	"github.com/sayem314/oracle/apps/api/internal/tool/datetime"
)

// reg builds a Registry with the datetime group's tools, mirroring the chat
// loop's wiring, so tests drive tools through the public Executor surface.
func reg(t *testing.T) tool.Executor {
	r := tool.NewRegistry()
	for _, tl := range datetime.New() {
		require.NoError(t, r.Register(tl))
	}
	return r
}

func runTool(r tool.Executor, name, args string) (string, error) {
	return r.Execute(context.Background(), name, args)
}

func TestGetTimeReturnsCurrentInstant(t *testing.T) {
	r := reg(t)
	out, err := runTool(r, "get_time", `{}`)
	require.NoError(t, err)

	var got struct {
		Datetime string `json:"datetime"`
		Date     string `json:"date"`
		Weekday  string `json:"weekday"`
		Timezone string `json:"timezone"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "UTC", got.Timezone)

	ts, err := time.Parse(time.RFC3339, got.Datetime)
	require.NoError(t, err)
	assert.InDelta(t, time.Now().UTC().Unix(), ts.Unix(), 5, "instant must be within a few seconds of now")

	wall := ts.In(time.UTC)
	assert.Equal(t, wall.Format("2006-01-02"), got.Date)
	assert.Equal(t, wall.Weekday().String(), got.Weekday)
}

func TestGetTimeWithTimezone(t *testing.T) {
	r := reg(t)
	out, err := runTool(r, "get_time", `{"timezone":"America/New_York"}`)
	require.NoError(t, err)

	var got struct {
		Datetime string `json:"datetime"`
		Timezone string `json:"timezone"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "America/New_York", got.Timezone)

	ts, err := time.Parse(time.RFC3339, got.Datetime)
	require.NoError(t, err)
	assert.InDelta(t, time.Now().Unix(), ts.Unix(), 5, "instant must be within a few seconds of now")
}

func TestGetTimeRejectsUnknownTimezone(t *testing.T) {
	_, err := runTool(reg(t), "get_time", `{"timezone":"Mars/Olympus"}`)
	require.ErrorContains(t, err, "unknown timezone")
}

func TestDateCalcShiftsByWeeksAndMonths(t *testing.T) {
	out, err := runTool(reg(t), "date_calc",
		`{"base":"2026-01-15","timezone":"America/New_York","weeks":2,"months":1}`)
	require.NoError(t, err)

	var got struct{ Datetime string }
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "2026-02-28T19:00:00-05:00", got.Datetime)
}

func TestDateCalcMonthNormalization(t *testing.T) {
	out, err := runTool(reg(t), "date_calc",
		`{"base":"2026-01-31","weeks":0,"months":1}`)
	require.NoError(t, err)

	var got struct{ Datetime string }
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "2026-03-03T00:00:00Z", got.Datetime)
}

func TestDateCalcRejectsBadBase(t *testing.T) {
	_, err := runTool(reg(t), "date_calc", `{"base":"not-a-date"}`)
	require.ErrorContains(t, err, "base must be RFC 3339 or YYYY-MM-DD")
}

func TestTimezoneConvert(t *testing.T) {
	out, err := runTool(reg(t), "timezone_convert",
		`{"datetime":"2026-08-06T14:00:00Z","from":"UTC","to":"Asia/Tokyo"}`)
	require.NoError(t, err)

	var got struct {
		Datetime string `json:"datetime"`
		Timezone string `json:"timezone"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "2026-08-06T23:00:00+09:00", got.Datetime)
	assert.Equal(t, "Asia/Tokyo", got.Timezone)
}

func TestTimezoneConvertRejectsBadDatetime(t *testing.T) {
	_, err := runTool(reg(t), "timezone_convert",
		`{"datetime":"bad","from":"UTC","to":"UTC"}`)
	require.ErrorContains(t, err, "datetime must be RFC 3339")
}
