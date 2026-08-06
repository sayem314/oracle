package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

// weatherEndpoint is the keyless Open-Meteo forecast API.
const weatherEndpoint = "https://api.open-meteo.com/v1/forecast"

const (
	maxWeatherBytes = 64 << 10
	minForecastDays = 1
	maxForecastDays = 7
)

func weatherTool(client *http.Client) Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"latitude":{"type":"number"},"longitude":{"type":"number"},"units":{"type":"string","enum":["celsius","fahrenheit"]},"days":{"type":"integer"}},"required":["latitude","longitude"],"additionalProperties":false}`)
	return Tool{
		Definition: llm.Tool{
			Name: "weather",
			Description: "Return the current weather and a daily forecast for a location. " +
				"latitude and longitude are WGS84 coordinates. units is celsius or fahrenheit " +
				"(default celsius). days is the forecast horizon from 1 to 7 (default 1). " +
				"Returns current temperature, apparent temperature, humidity, wind, and per-day " +
				"high, low, precipitation chance, and the WMO weather code.",
			Parameters: schema,
		},
		Execute: weatherExecute(client, weatherEndpoint),
	}
}

func weatherExecute(client *http.Client, endpoint string) ExecuteFunc {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Units     string  `json:"units"`
			Days      int     `json:"days"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("weather: %w", err)
		}
		if in.Latitude < -90 || in.Latitude > 90 {
			return "", errors.New("weather: latitude must be between -90 and 90")
		}
		if in.Longitude < -180 || in.Longitude > 180 {
			return "", errors.New("weather: longitude must be between -180 and 180")
		}
		unit := in.Units
		if unit == "" {
			unit = "celsius"
		}
		if unit != "celsius" && unit != "fahrenheit" {
			return "", fmt.Errorf("weather: unknown units %q", unit)
		}
		days := in.Days
		if days == 0 {
			days = 1
		}
		if days < minForecastDays || days > maxForecastDays {
			return "", fmt.Errorf("weather: days must be between %d and %d", minForecastDays, maxForecastDays)
		}

		query := url.Values{}
		query.Set("latitude", fmt.Sprintf("%.5f", in.Latitude))
		query.Set("longitude", fmt.Sprintf("%.5f", in.Longitude))
		query.Set("current", "temperature_2m,apparent_temperature,relative_humidity_2m,wind_speed_10m,weather_code")
		query.Set("daily", "temperature_2m_max,temperature_2m_min,precipitation_probability_max,weather_code")
		query.Set("temperature_unit", unit)
		query.Set("forecast_days", fmt.Sprintf("%d", days))
		query.Set("timezone", "auto")

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
		if err != nil {
			return "", fmt.Errorf("weather: %w", err)
		}
		req.Header.Set("User-Agent", "oracle/1.0")
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("weather: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxWeatherBytes+1))
		if err != nil {
			return "", fmt.Errorf("weather: read body: %w", err)
		}
		if len(body) > maxWeatherBytes {
			return "", errors.New("weather: response too large")
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("weather: server returned %s", resp.Status)
		}

		var out openMeteoResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return "", fmt.Errorf("weather: parse response: %w", err)
		}
		return renderWeather(out, unit), nil
	}
}

type openMeteoResponse struct {
	Current openMeteoCurrent `json:"current"`
	Daily   openMeteoDaily   `json:"daily"`
}

type openMeteoCurrent struct {
	Temperature         float64 `json:"temperature_2m"`
	ApparentTemperature float64 `json:"apparent_temperature"`
	Humidity            float64 `json:"relative_humidity_2m"`
	WindSpeed           float64 `json:"wind_speed_10m"`
	WeatherCode         int     `json:"weather_code"`
}

type openMeteoDaily struct {
	Time                        []string   `json:"time"`
	TemperatureMax              []float64  `json:"temperature_2m_max"`
	TemperatureMin              []float64  `json:"temperature_2m_min"`
	PrecipitationProbabilityMax []*float64 `json:"precipitation_probability_max"`
	WeatherCode                 []int      `json:"weather_code"`
}

// renderWeather builds the model-facing summary. Weather codes stay as WMO
// numbers so the tool adds no interpretation of its own.
func renderWeather(resp openMeteoResponse, unit string) string {
	degree := "C"
	if unit == "fahrenheit" {
		degree = "F"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "current (WMO code %d):\n", resp.Current.WeatherCode)
	fmt.Fprintf(&sb, "  temperature: %.1f °%s\n", resp.Current.Temperature, degree)
	fmt.Fprintf(&sb, "  apparent temperature: %.1f °%s\n", resp.Current.ApparentTemperature, degree)
	fmt.Fprintf(&sb, "  humidity: %.0f%%\n", resp.Current.Humidity)
	fmt.Fprintf(&sb, "  wind speed: %.0f km/h\n", resp.Current.WindSpeed)
	sb.WriteString("forecast:\n")
	for i := range resp.Daily.Time {
		prob := "n/a"
		if i < len(resp.Daily.PrecipitationProbabilityMax) && resp.Daily.PrecipitationProbabilityMax[i] != nil {
			prob = fmt.Sprintf("%.0f%%", *resp.Daily.PrecipitationProbabilityMax[i])
		}
		fmt.Fprintf(&sb, "  %s: low %.1f °%s, high %.1f °%s, rain chance %s, code %d\n",
			resp.Daily.Time[i],
			resp.Daily.TemperatureMin[i], degree,
			resp.Daily.TemperatureMax[i], degree,
			prob,
			resp.Daily.WeatherCode[i],
		)
	}
	return strings.TrimRight(sb.String(), "\n")
}
