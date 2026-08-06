package datetime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	_ "time/tzdata"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

func New() []tool.Tool {
	return []tool.Tool{
		getTimeTool(),
		dateCalcTool(),
		timezoneConvertTool(),
	}
}

func getTimeTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string"}},"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "get_time",
			Description: "Return the current date and time. Returns datetime (RFC 3339), " +
				"date, weekday, and timezone for the given IANA timezone (default UTC).",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Timezone string `json:"timezone"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("get_time: %w", err)
			}
			loc, err := loadLocation(in.Timezone)
			if err != nil {
				return "", err
			}
			return formatNow(time.Now().In(loc)), nil
		},
	}
}

func dateCalcTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"base":{"type":"string"},"timezone":{"type":"string"},"weeks":{"type":"integer"},"months":{"type":"integer"}},"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "date_calc",
			Description: "Shift a date by weeks and/or months and return the resulting date and time. " +
				"base is an RFC 3339 timestamp or a YYYY-MM-DD date, interpreted in timezone (default UTC); " +
				"if omitted, base is now. Months use calendar normalization, so surplus days roll into the " +
				"next month (Jan 31 plus 1 month lands on Mar 3). Returns datetime, date, weekday, and timezone.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Base     string `json:"base"`
				Timezone string `json:"timezone"`
				Weeks    int    `json:"weeks"`
				Months   int    `json:"months"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("date_calc: %w", err)
			}
			loc, err := loadLocation(in.Timezone)
			if err != nil {
				return "", err
			}
			base, err := parseBase(in.Base, loc, time.Now())
			if err != nil {
				return "", err
			}
			result := base.AddDate(0, in.Months, in.Weeks*7)
			return formatNow(result.In(loc)), nil
		},
	}
}

func timezoneConvertTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"datetime":{"type":"string"},"from":{"type":"string"},"to":{"type":"string"}},"required":["datetime","from","to"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "timezone_convert",
			Description: "Render an instant in another timezone. datetime is an RFC 3339 timestamp; " +
				"from and to are IANA timezone names. Returns datetime, date, weekday, and timezone in the target zone.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Datetime string `json:"datetime"`
				From     string `json:"from"`
				To       string `json:"to"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("timezone_convert: %w", err)
			}
			from, err := loadLocation(in.From)
			if err != nil {
				return "", err
			}
			to, err := loadLocation(in.To)
			if err != nil {
				return "", err
			}
			t, err := time.Parse(time.RFC3339, in.Datetime)
			if err != nil {
				return "", fmt.Errorf("timezone_convert: datetime must be RFC 3339: %w", err)
			}
			return formatNow(t.In(from).In(to)), nil
		},
	}
}

// loadLocation resolves an IANA zone name. An empty name means UTC. time/tzdata
// is embedded so named zones resolve even in stripped runtime images.
func loadLocation(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q", name)
	}
	return loc, nil
}

// parseBase turns an optional base string into a time, defaulting to now in loc.
func parseBase(base string, loc *time.Location, fallback time.Time) (time.Time, error) {
	if base == "" {
		return fallback.In(loc), nil
	}
	if t, err := time.Parse(time.RFC3339, base); err == nil {
		return t, nil
	}
	if d, err := time.Parse("2006-01-02", base); err == nil {
		return d.In(loc), nil
	}
	return time.Time{}, fmt.Errorf("date_calc: base must be RFC 3339 or YYYY-MM-DD")
}

func formatNow(t time.Time) string {
	loc := t.Location().String()
	if loc == "" {
		loc = "UTC"
	}
	out := struct {
		Datetime string `json:"datetime"`
		Date     string `json:"date"`
		Weekday  string `json:"weekday"`
		Timezone string `json:"timezone"`
	}{
		Datetime: t.Format(time.RFC3339),
		Date:     t.Format("2006-01-02"),
		Weekday:  t.Weekday().String(),
		Timezone: loc,
	}
	b, _ := json.Marshal(out)
	return string(b)
}
