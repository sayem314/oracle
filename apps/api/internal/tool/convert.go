package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

func convertTool() Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"value":{"type":"number"},"from":{"type":"string"},"to":{"type":"string"}},"required":["value","from","to"],"additionalProperties":false}`)
	return Tool{
		Definition: llm.Tool{
			Name: "convert",
			Description: "Convert a value between compatible units. Supports length, mass, " +
				"temperature, volume, area, time, speed, and data. from and to are unit names " +
				"or common abbreviations. Returns the converted value and the target unit.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Value float64 `json:"value"`
				From  string  `json:"from"`
				To    string  `json:"to"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("convert: %w", err)
			}
			if strings.TrimSpace(in.From) == "" {
				return "", errors.New("convert: from is required")
			}
			if strings.TrimSpace(in.To) == "" {
				return "", errors.New("convert: to is required")
			}
			converted, err := convertUnit(in.Value, in.From, in.To)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s %s = %s %s",
				formatConvertValue(in.Value), canonicalName(in.From),
				formatConvertValue(converted), canonicalName(in.To)), nil
		},
	}
}

// convertUnit converts value from one unit to another, erroring when a unit is
// unknown or the two units measure different dimensions.
func convertUnit(value float64, from, to string) (float64, error) {
	fromUnit, ok := units[strings.ToLower(strings.TrimSpace(from))]
	if !ok {
		return 0, fmt.Errorf("convert: unknown unit %q", from)
	}
	toUnit, ok := units[strings.ToLower(strings.TrimSpace(to))]
	if !ok {
		return 0, fmt.Errorf("convert: unknown unit %q", to)
	}
	if fromUnit.dim != toUnit.dim {
		return 0, fmt.Errorf("convert: cannot convert %s to %s", fromUnit.dim, toUnit.dim)
	}
	base := fromUnit.toBase(value)
	return toUnit.fromBase(base), nil
}

// formatConvertValue trims trailing zeros from integer magnitudes and keeps a
// reasonable precision otherwise.
func formatConvertValue(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}

func canonicalName(name string) string {
	if u, ok := units[strings.ToLower(strings.TrimSpace(name))]; ok && u.canon != "" {
		return u.canon
	}
	return strings.TrimSpace(name)
}

const (
	dimLength = "length"
	dimMass   = "mass"
	dimTemp   = "temperature"
	dimVolume = "volume"
	dimArea   = "area"
	dimTime   = "time"
	dimSpeed  = "speed"
	dimData   = "data"
)

type unitDef struct {
	dim      string
	toBase   func(float64) float64
	fromBase func(float64) float64
	canon    string
}

// factor returns a scalar unit whose base is base * value and stands km/m as canon.
func factor(dim string, canon string, scale float64) unitDef {
	return unitDef{dim: dim, canon: canon,
		toBase:   func(v float64) float64 { return v * scale },
		fromBase: func(v float64) float64 { return v / scale },
	}
}

// affine is used by temperature, where conversion needs an offset, not a scale.
func affine(canon string, scale, offset float64) unitDef {
	return unitDef{dim: dimTemp, canon: canon,
		toBase:   func(v float64) float64 { return v*scale + offset }, // to Celsius
		fromBase: func(v float64) float64 { return (v - offset) / scale },
	}
}

var units = map[string]unitDef{
	// length, base metre
	"m":           factor(dimLength, "m", 1),
	"meter":       factor(dimLength, "m", 1),
	"metre":       factor(dimLength, "m", 1),
	"meters":      factor(dimLength, "m", 1),
	"metres":      factor(dimLength, "m", 1),
	"cm":          factor(dimLength, "cm", 0.01),
	"centimeter":  factor(dimLength, "cm", 0.01),
	"centimetre":  factor(dimLength, "cm", 0.01),
	"centimetres": factor(dimLength, "cm", 0.01),
	"mm":          factor(dimLength, "mm", 0.001),
	"millimeter":  factor(dimLength, "mm", 0.001),
	"millimetre":  factor(dimLength, "mm", 0.001),
	"km":          factor(dimLength, "km", 1000),
	"kilometer":   factor(dimLength, "km", 1000),
	"kilometre":   factor(dimLength, "km", 1000),
	"kilometers":  factor(dimLength, "km", 1000),
	"in":          factor(dimLength, "in", 0.0254),
	"inch":        factor(dimLength, "in", 0.0254),
	"inches":      factor(dimLength, "in", 0.0254),
	"ft":          factor(dimLength, "ft", 0.3048),
	"foot":        factor(dimLength, "ft", 0.3048),
	"feet":        factor(dimLength, "ft", 0.3048),
	"yd":          factor(dimLength, "yd", 0.9144),
	"yard":        factor(dimLength, "yd", 0.9144),
	"yards":       factor(dimLength, "yd", 0.9144),
	"mi":          factor(dimLength, "mi", 1609.344),
	"mile":        factor(dimLength, "mi", 1609.344),
	"miles":       factor(dimLength, "mi", 1609.344),

	// mass, base kilogram
	"kg":         factor(dimMass, "kg", 1),
	"kilogram":   factor(dimMass, "kg", 1),
	"kilograms":  factor(dimMass, "kg", 1),
	"g":          factor(dimMass, "g", 0.001),
	"gram":       factor(dimMass, "g", 0.001),
	"grams":      factor(dimMass, "g", 0.001),
	"mg":         factor(dimMass, "mg", 1e-6),
	"milligram":  factor(dimMass, "mg", 1e-6),
	"milligrams": factor(dimMass, "mg", 1e-6),
	"t":          factor(dimMass, "t", 1000),
	"tonne":      factor(dimMass, "t", 1000),
	"lb":         factor(dimMass, "lb", 0.45359237),
	"lbs":        factor(dimMass, "lb", 0.45359237),
	"pound":      factor(dimMass, "lb", 0.45359237),
	"pounds":     factor(dimMass, "lb", 0.45359237),
	"oz":         factor(dimMass, "oz", 0.028349523125),
	"ounce":      factor(dimMass, "oz", 0.028349523125),
	"ounces":     factor(dimMass, "oz", 0.028349523125),

	// temperature, base celsius
	"c":          affine("°C", 1, 0),
	"celsius":    affine("°C", 1, 0),
	"centigrade": affine("°C", 1, 0),
	"°c":         affine("°C", 1, 0),
	"degreec":    affine("°C", 1, 0),
	"f":          affine("°F", 5.0/9.0, -32.0*5.0/9.0), // to Celsius: (F-32)*5/9
	"fahrenheit": affine("°F", 5.0/9.0, -32.0*5.0/9.0),
	"°f":         affine("°F", 5.0/9.0, -32.0*5.0/9.0),
	"degreef":    affine("°F", 5.0/9.0, -32.0*5.0/9.0),
	"k":          affine("K", 1, -273.15),
	"kelvin":     affine("K", 1, -273.15),

	// volume, base litre
	"l":          factor(dimVolume, "l", 1),
	"liter":      factor(dimVolume, "l", 1),
	"litre":      factor(dimVolume, "l", 1),
	"litres":     factor(dimVolume, "l", 1),
	"ml":         factor(dimVolume, "ml", 0.001),
	"milliliter": factor(dimVolume, "ml", 0.001),
	"gallon":     factor(dimVolume, "gal", 3.785411784),
	"gal":        factor(dimVolume, "gal", 3.785411784),
	"gallons":    factor(dimVolume, "gal", 3.785411784),
	"qt":         factor(dimVolume, "qt", 0.946352946),
	"quart":      factor(dimVolume, "qt", 0.946352946),
	"pt":         factor(dimVolume, "pt", 0.473176473),
	"pint":       factor(dimVolume, "pt", 0.473176473),
	"cup":        factor(dimVolume, "cup", 0.2365882365),
	"cups":       factor(dimVolume, "cup", 0.2365882365),
	"floz":       factor(dimVolume, "fl oz", 0.0295735295625),
	"tbsp":       factor(dimVolume, "tbsp", 0.01478676478125),

	// area, base square metre
	"m2":      factor(dimArea, "m²", 1),
	"sqm":     factor(dimArea, "m²", 1),
	"ha":      factor(dimArea, "ha", 10000),
	"hectare": factor(dimArea, "ha", 10000),
	"acre":    factor(dimArea, "acre", 4046.8564224),
	"acres":   factor(dimArea, "acre", 4046.8564224),
	"ft2":     factor(dimArea, "ft²", 0.09290304),
	"sqft":    factor(dimArea, "ft²", 0.09290304),

	// time, base second
	"s":       factor(dimTime, "s", 1),
	"second":  factor(dimTime, "s", 1),
	"seconds": factor(dimTime, "s", 1),
	"min":     factor(dimTime, "min", 60),
	"minute":  factor(dimTime, "min", 60),
	"minutes": factor(dimTime, "min", 60),
	"h":       factor(dimTime, "h", 3600),
	"hr":      factor(dimTime, "h", 3600),
	"hour":    factor(dimTime, "h", 3600),
	"hours":   factor(dimTime, "h", 3600),
	"day":     factor(dimTime, "days", 86400),
	"d":       factor(dimTime, "days", 86400),
	"days":    factor(dimTime, "days", 86400),
	"week":    factor(dimTime, "weeks", 604800),
	"weeks":   factor(dimTime, "weeks", 604800),

	// speed, base metre per second
	"mps":   factor(dimSpeed, "m/s", 1),
	"kmh":   factor(dimSpeed, "km/h", 1.0/3.6),
	"kph":   factor(dimSpeed, "km/h", 1.0/3.6),
	"mph":   factor(dimSpeed, "mph", 0.44704),
	"knot":  factor(dimSpeed, "knot", 0.514444444),
	"knots": factor(dimSpeed, "knot", 0.514444444),

	// data, base byte
	"b":         factor(dimData, "B", 1),
	"byte":      factor(dimData, "B", 1),
	"bytes":     factor(dimData, "B", 1),
	"kb":        factor(dimData, "KB", 1024),
	"kilobyte":  factor(dimData, "KB", 1024),
	"kilobytes": factor(dimData, "KB", 1024),
	"mb":        factor(dimData, "MB", 1024*1024),
	"megabyte":  factor(dimData, "MB", 1024*1024),
	"megabytes": factor(dimData, "MB", 1024*1024),
	"gb":        factor(dimData, "GB", 1024*1024*1024),
	"gigabyte":  factor(dimData, "GB", 1024*1024*1024),
	"gigabytes": factor(dimData, "GB", 1024*1024*1024),
}
