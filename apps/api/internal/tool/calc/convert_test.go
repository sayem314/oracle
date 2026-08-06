package calc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertRegistered(t *testing.T) {
	found := false
	for _, tl := range New() {
		if tl.Definition.Name == "convert" {
			found = true
		}
	}
	assert.True(t, found, "convert should be part of the calc group")
}

func TestConvertLength(t *testing.T) {
	cases := []struct {
		from  string
		to    string
		value float64
		want  float64
	}{
		{"cm", "m", 100, 1},
		{"m", "cm", 1, 100},
		{"mi", "km", 10, 16.09344},
		{"ft", "m", 1, 0.3048},
		{"kilometers", "m", 1, 1000},
	}
	for _, tc := range cases {
		got, err := convertUnit(tc.value, tc.from, tc.to)
		require.NoError(t, err, "%s to %s", tc.from, tc.to)
		assert.InDelta(t, tc.want, got, 1e-9, "%s to %s", tc.from, tc.to)
	}
}

func TestConvertMass(t *testing.T) {
	got, err := convertUnit(1, "kg", "lb")
	require.NoError(t, err)
	assert.InDelta(t, 2.2046226218, got, 1e-9)

	got, err = convertUnit(16, "oz", "lb")
	require.NoError(t, err)
	assert.InDelta(t, 1, got, 1e-9)
}

func TestConvertTemperature(t *testing.T) {
	cases := []struct {
		from, to string
		value    float64
		want     float64
	}{
		{"celsius", "fahrenheit", 0, 32},
		{"celsius", "fahrenheit", 100, 212},
		{"fahrenheit", "celsius", 32, 0},
		{"fahrenheit", "celsius", 212, 100},
		{"celsius", "kelvin", 0, 273.15},
		{"kelvin", "celsius", 273.15, 0},
		{"fahrenheit", "kelvin", 32, 273.15},
	}
	for _, tc := range cases {
		got, err := convertUnit(tc.value, tc.from, tc.to)
		require.NoError(t, err, "%s to %s", tc.from, tc.to)
		assert.InDelta(t, tc.want, got, 1e-9, "%s to %s", tc.from, tc.to)
	}
}

func TestConvertVolumeAndCapacity(t *testing.T) {
	got, err := convertUnit(1, "gallon", "litre")
	require.NoError(t, err)
	assert.InDelta(t, 3.785411784, got, 1e-9)

	got, err = convertUnit(1, "l", "ml")
	require.NoError(t, err)
	assert.InDelta(t, 1000, got, 1e-9)
}

func TestConvertAreaTimeSpeedData(t *testing.T) {
	got, err := convertUnit(1, "hectare", "m2")
	require.NoError(t, err)
	assert.InDelta(t, 10000, got, 1e-9)

	got, err = convertUnit(1, "hour", "minutes")
	require.NoError(t, err)
	assert.InDelta(t, 60, got, 1e-9)

	got, err = convertUnit(60, "kmh", "mph")
	require.NoError(t, err)
	assert.InDelta(t, 37.2822715, got, 1e-6)

	got, err = convertUnit(1, "gb", "mb")
	require.NoError(t, err)
	assert.InDelta(t, 1024, got, 1e-9)
}

func TestConvertIncompatibleDimensions(t *testing.T) {
	_, err := convertUnit(1, "kg", "m")
	require.ErrorContains(t, err, "cannot convert mass to length")
}

func TestConvertUnknownUnit(t *testing.T) {
	_, err := convertUnit(1, "furlong", "m")
	require.ErrorContains(t, err, `unknown unit "furlong"`)

	_, err = convertUnit(1, "m", "stones")
	require.ErrorContains(t, err, `unknown unit "stones"`)
}

func TestConvertToolOutput(t *testing.T) {
	out, err := convertTool().Execute(context.Background(), mustArgs(`{"value":10,"from":"kg","to":"lb"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "10 kg =")
	assert.Contains(t, out, "lb")
}

func TestConvertToolRequiresFromAndTo(t *testing.T) {
	_, err := convertTool().Execute(context.Background(), mustArgs(`{"value":10,"from":"","to":"lb"}`))
	require.ErrorContains(t, err, "from is required")

	_, err = convertTool().Execute(context.Background(), mustArgs(`{"value":10,"from":"kg","to":""}`))
	require.ErrorContains(t, err, "to is required")
}
