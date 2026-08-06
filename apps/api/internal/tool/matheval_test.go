package tool

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMathEvalRegistered(t *testing.T) {
	found := false
	for _, tl := range NewBuiltin() {
		if tl.Definition.Name == "math_eval" {
			found = true
		}
	}
	assert.True(t, found, "math_eval should be part of NewBuiltin")
}

func TestMathEvalExpressions(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		{"2+2", 4},
		{"1+2*3", 7},
		{"(1+2)*3", 9},
		{"2^10", 1024},
		{"2^3^2", 512}, // right-associative
		{"-2^2", -4},   // power binds tighter than unary minus
		{"(-2)^2", 4},
		{"10%3", 1},
		{"2.5*4", 10},
		{"7/2", 3.5},
		{"1e3", 1000},
		{"1.5e2", 150},
		{"sqrt(9)", 3},
		{"abs(-5)", 5},
		{"round(2.5)", 3},
		{"floor(2.9)", 2},
		{"ceil(2.1)", 3},
		{"sin(pi/2)", 1},
		{"cos(0)", 1},
		{"exp(0)", 1},
		{"ln(e)", 1},
		{"log10(100)", 2},
		{"min(3,7)", 3},
		{"max(3,7)", 7},
		{"  2 +  3  ", 5},
		{"+5", 5},
		{"-(-3)", 3},
		{"pi", math.Pi},
		{"2*pi", 2 * math.Pi},
	}
	for _, tc := range cases {
		got, err := evalMath(tc.expr)
		require.NoError(t, err, "expr %q", tc.expr)
		assert.InDelta(t, tc.want, got, 1e-9, "expr %q", tc.expr)
	}
}

func TestMathEvalErrors(t *testing.T) {
	cases := []string{
		"2+",
		"*3",
		"(2+3",
		"2+3)",
		"foo(2)",
		"x+1",
		"2..3",
		"sin()",
		"sin(1,2)",
		"min(1)",
		"min(1,2,3)",
		"1@2",
	}
	for _, expr := range cases {
		_, err := evalMath(expr)
		require.Error(t, err, "expr %q", expr)
	}
}

func TestMathEvalDivisionByZero(t *testing.T) {
	_, err := evalMath("1/0")
	require.ErrorContains(t, err, "division by zero")
}

func TestMathEvalNonFiniteViaTool(t *testing.T) {
	tl := mathEvalTool()
	_, err := tl.Execute(context.Background(), mustArgs(`{"expression":"sqrt(-1)"}`))
	require.ErrorContains(t, err, "not a finite number")
}

func TestMathEvalEmptyExpression(t *testing.T) {
	tl := mathEvalTool()
	_, err := tl.Execute(context.Background(), mustArgs(`{"expression":"  "}`))
	require.ErrorContains(t, err, "expression is required")
}

func TestMathEvalInvalidArgs(t *testing.T) {
	tl := mathEvalTool()
	_, err := tl.Execute(context.Background(), json.RawMessage(`not json`))
	require.Error(t, err)
}
