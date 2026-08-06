package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWeatherRegistered(t *testing.T) {
	found := false
	for _, tl := range NewBuiltin() {
		if tl.Definition.Name == "weather" {
			found = true
		}
	}
	assert.True(t, found, "weather should be part of NewBuiltin")
}

func TestWeatherRejectsInvalidCoordinates(t *testing.T) {
	_, err := weatherExecute(plainClient(), weatherEndpoint)(
		context.Background(), mustArgs(`{"latitude":91,"longitude":0}`))
	require.ErrorContains(t, err, "latitude must be between -90 and 90")

	_, err = weatherExecute(plainClient(), weatherEndpoint)(
		context.Background(), mustArgs(`{"latitude":0,"longitude":181}`))
	require.ErrorContains(t, err, "longitude must be between -180 and 180")
}

func TestWeatherRejectsBadUnits(t *testing.T) {
	_, err := weatherExecute(plainClient(), weatherEndpoint)(
		context.Background(), mustArgs(`{"latitude":10,"longitude":10,"units":"kelvin"}`))
	require.ErrorContains(t, err, `unknown units "kelvin"`)
}

func TestWeatherRejectsBadDays(t *testing.T) {
	_, err := weatherExecute(plainClient(), weatherEndpoint)(
		context.Background(), mustArgs(`{"latitude":10,"longitude":10,"days":8}`))
	require.ErrorContains(t, err, "days must be between 1 and 7")
}

func TestWeatherSendsForecastQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"current":{},"daily":{}}`))
	}))
	defer srv.Close()

	_, err := weatherExecute(plainClient(), srv.URL)(
		context.Background(), mustArgs(`{"latitude":40.7128,"longitude":-74.006,"days":3}`))
	require.NoError(t, err)

	assert.Contains(t, gotQuery, "latitude=40.71280")
	assert.Contains(t, gotQuery, "longitude=-74.00600")
	assert.Contains(t, gotQuery, "forecast_days=3")
	assert.Contains(t, gotQuery, "temperature_unit=celsius")
	assert.Contains(t, gotQuery, "timezone=auto")
}

func TestWeatherRendersCurrentAndDaily(t *testing.T) {
	body := `{
		"current": {
			"temperature_2m": 21.5,
			"apparent_temperature": 23.0,
			"relative_humidity_2m": 58,
			"wind_speed_10m": 11.2,
			"weather_code": 2
		},
		"daily": {
			"time": ["2026-08-06"],
			"temperature_2m_max": [26.1],
			"temperature_2m_min": [17.4],
			"precipitation_probability_max": [30],
			"weather_code": [2]
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	out, err := weatherExecute(plainClient(), srv.URL)(
		context.Background(), mustArgs(`{"latitude":40.7,"longitude":-74.0}`))
	require.NoError(t, err)

	assert.Contains(t, out, "current (WMO code 2)")
	assert.Contains(t, out, "temperature: 21.5 \u00b0C")
	assert.Contains(t, out, "apparent temperature: 23.0 \u00b0C")
	assert.Contains(t, out, "humidity: 58%")
	assert.Contains(t, out, "wind speed: 11 km/h")
	assert.Contains(t, out, "2026-08-06: low 17.4 \u00b0C, high 26.1 \u00b0C, rain chance 30%, code 2")
}

func TestWeatherFahrenheitUnit(t *testing.T) {
	var gotUnit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUnit = r.URL.Query().Get("temperature_unit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"current": {"temperature_2m":72,"apparent_temperature":74,"relative_humidity_2m":40,"wind_speed_10m":5,"weather_code":1},
			"daily": {"time":[],"temperature_2m_max":[],"temperature_2m_min":[],"precipitation_probability_max":[],"weather_code":[]}
		}`))
	}))
	defer srv.Close()

	out, err := weatherExecute(plainClient(), srv.URL)(
		context.Background(), mustArgs(`{"latitude":40.7,"longitude":-74.0,"units":"fahrenheit"}`))
	require.NoError(t, err)

	assert.Equal(t, "fahrenheit", gotUnit)
	assert.Contains(t, out, "temperature: 72.0 \u00b0F")
}

func TestWeatherNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer srv.Close()

	_, err := weatherExecute(plainClient(), srv.URL)(
		context.Background(), mustArgs(`{"latitude":40.7,"longitude":-74.0}`))
	require.ErrorContains(t, err, "server returned 502")
}

func TestWeatherMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	_, err := weatherExecute(plainClient(), srv.URL)(
		context.Background(), mustArgs(`{"latitude":40.7,"longitude":-74.0}`))
	require.ErrorContains(t, err, "parse response")
}

func TestWeatherOversizeResponse(t *testing.T) {
	big := `{"current":{"temperature_2m":` + strings.Repeat("9", 70000) + `}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	_, err := weatherExecute(plainClient(), srv.URL)(
		context.Background(), mustArgs(`{"latitude":40.7,"longitude":-74.0}`))
	require.ErrorContains(t, err, "response too large")
}
