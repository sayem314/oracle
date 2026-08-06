package calc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

func New() []tool.Tool {
	return []tool.Tool{
		convertTool(),
		mathEvalTool(),
	}
}

func mathEvalTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string"}},"required":["expression"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "math_eval",
			Description: "Evaluate a math expression and return the numeric result. " +
				"Supports + - * / % ^ (power, right-associative), unary minus/plus, parentheses, " +
				"constants pi and e, and functions sqrt, abs, round, floor, ceil, sin, cos, tan, exp, " +
				"ln, log10, min, max (trig in radians).",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Expression string `json:"expression"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("math_eval: %w", err)
			}
			if strings.TrimSpace(in.Expression) == "" {
				return "", errors.New("math_eval: expression is required")
			}
			v, err := evalMath(in.Expression)
			if err != nil {
				return "", err
			}
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return "", errors.New("math_eval: result is not a finite number (division by zero or overflow)")
			}
			return formatMathResult(v), nil
		},
	}
}

// evalMath tokenizes and evaluates expr with a small recursive-descent parser.
// No variables or side channels exist by construction, so it is safe to run
// on untrusted input.
func evalMath(expr string) (float64, error) {
	toks, err := mathTokenize(expr)
	if err != nil {
		return 0, err
	}
	p := &mathParser{toks: toks}
	v, err := p.parseSum()
	if err != nil {
		return 0, err
	}
	if p.peek().kind != mathEOF {
		return 0, fmt.Errorf("math_eval: unexpected token %q", p.peek().text)
	}
	return v, nil
}

// formatMathResult prints integers without a decimal point and everything
// else in the shortest round-trippable form.
func formatMathResult(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

type mathTokenKind int

const (
	mathEOF mathTokenKind = iota
	mathNumber
	mathIdent
	mathPlus
	mathMinus
	mathStar
	mathSlash
	mathPercent
	mathCaret
	mathLParen
	mathRParen
	mathComma
)

type mathToken struct {
	kind mathTokenKind
	text string
}

func mathTokenize(expr string) ([]mathToken, error) {
	var toks []mathToken
	i := 0
	for i < len(expr) {
		c := expr[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case strings.ContainsRune("+-*/%^(),", rune(c)):
			kinds := map[byte]mathTokenKind{
				'+': mathPlus, '-': mathMinus, '*': mathStar, '/': mathSlash,
				'%': mathPercent, '^': mathCaret, '(': mathLParen, ')': mathRParen,
				',': mathComma,
			}
			toks = append(toks, mathToken{kind: kinds[c], text: string(c)})
			i++
		case c >= '0' && c <= '9' || c == '.':
			j := i
			for j < len(expr) && (expr[j] >= '0' && expr[j] <= '9' || expr[j] == '.') {
				j++
			}
			if j < len(expr) && (expr[j] == 'e' || expr[j] == 'E') {
				k := j + 1
				if k < len(expr) && (expr[k] == '+' || expr[k] == '-') {
					k++
				}
				if k < len(expr) && expr[k] >= '0' && expr[k] <= '9' {
					j = k
					for j < len(expr) && expr[j] >= '0' && expr[j] <= '9' {
						j++
					}
				}
			}
			text := expr[i:j]
			if _, err := strconv.ParseFloat(text, 64); err != nil {
				return nil, fmt.Errorf("math_eval: invalid number %q", text)
			}
			toks = append(toks, mathToken{kind: mathNumber, text: text})
			i = j
		case isMathLetter(c):
			j := i
			for j < len(expr) && (isMathLetter(expr[j]) || expr[j] >= '0' && expr[j] <= '9') {
				j++
			}
			toks = append(toks, mathToken{kind: mathIdent, text: expr[i:j]})
			i = j
		default:
			return nil, fmt.Errorf("math_eval: unexpected character %q", string(c))
		}
	}
	toks = append(toks, mathToken{kind: mathEOF})
	return toks, nil
}

func isMathLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

type mathParser struct {
	toks []mathToken
	pos  int
}

func (p *mathParser) peek() mathToken {
	return p.toks[p.pos]
}

func (p *mathParser) next() mathToken {
	t := p.toks[p.pos]
	p.pos++
	return t
}

// Grammar (loosest to tightest): sum -> product -> unary -> power -> primary.
// Power is right-associative and binds tighter than unary, so -2^2 = -(2^2).

func (p *mathParser) parseSum() (float64, error) {
	left, err := p.parseProduct()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek().kind {
		case mathPlus:
			p.next()
			right, err := p.parseProduct()
			if err != nil {
				return 0, err
			}
			left += right
		case mathMinus:
			p.next()
			right, err := p.parseProduct()
			if err != nil {
				return 0, err
			}
			left -= right
		default:
			return left, nil
		}
	}
}

func (p *mathParser) parseProduct() (float64, error) {
	left, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek().kind {
		case mathStar:
			p.next()
			right, err := p.parseUnary()
			if err != nil {
				return 0, err
			}
			left *= right
		case mathSlash:
			p.next()
			right, err := p.parseUnary()
			if err != nil {
				return 0, err
			}
			if right == 0 {
				return 0, errors.New("math_eval: division by zero")
			}
			left /= right
		case mathPercent:
			p.next()
			right, err := p.parseUnary()
			if err != nil {
				return 0, err
			}
			if right == 0 {
				return 0, errors.New("math_eval: modulo by zero")
			}
			left = math.Mod(left, right)
		default:
			return left, nil
		}
	}
}

func (p *mathParser) parseUnary() (float64, error) {
	switch p.peek().kind {
	case mathMinus:
		p.next()
		v, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return -v, nil
	case mathPlus:
		p.next()
		return p.parseUnary()
	default:
		return p.parsePower()
	}
}

func (p *mathParser) parsePower() (float64, error) {
	base, err := p.parsePrimary()
	if err != nil {
		return 0, err
	}
	if p.peek().kind == mathCaret {
		p.next()
		exp, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

func (p *mathParser) parsePrimary() (float64, error) {
	t := p.next()
	switch t.kind {
	case mathNumber:
		v, _ := strconv.ParseFloat(t.text, 64)
		return v, nil
	case mathIdent:
		switch t.text {
		case "pi":
			return math.Pi, nil
		case "e":
			return math.E, nil
		default:
			if p.peek().kind != mathLParen {
				return 0, fmt.Errorf("math_eval: unknown symbol %q", t.text)
			}
			p.next()
			return p.parseCall(t.text)
		}
	case mathLParen:
		v, err := p.parseSum()
		if err != nil {
			return 0, err
		}
		if p.next().kind != mathRParen {
			return 0, errors.New("math_eval: expected a closing parenthesis")
		}
		return v, nil
	default:
		return 0, fmt.Errorf("math_eval: unexpected token %q", t.text)
	}
}

func (p *mathParser) parseCall(name string) (float64, error) {
	arity, ok := mathFuncArity[name]
	if !ok {
		return 0, fmt.Errorf("math_eval: unknown function %q", name)
	}
	var args []float64
	if p.peek().kind == mathRParen {
		return 0, fmt.Errorf("math_eval: %s expects %d argument(s)", name, arity)
	}
	for {
		v, err := p.parseSum()
		if err != nil {
			return 0, err
		}
		args = append(args, v)
		switch p.next().kind {
		case mathComma:
			continue
		case mathRParen:
			if len(args) != arity {
				return 0, fmt.Errorf("math_eval: %s expects %d argument(s)", name, arity)
			}
			return applyMathFunc(name, args), nil
		default:
			return 0, fmt.Errorf("math_eval: expected ',' or ')' in %s call", name)
		}
	}
}

var mathFuncArity = map[string]int{
	"sqrt": 1, "abs": 1, "round": 1, "floor": 1, "ceil": 1,
	"sin": 1, "cos": 1, "tan": 1, "exp": 1, "ln": 1, "log10": 1,
	"min": 2, "max": 2,
}

func applyMathFunc(name string, args []float64) float64 {
	switch name {
	case "sqrt":
		return math.Sqrt(args[0])
	case "abs":
		return math.Abs(args[0])
	case "round":
		return math.Round(args[0])
	case "floor":
		return math.Floor(args[0])
	case "ceil":
		return math.Ceil(args[0])
	case "sin":
		return math.Sin(args[0])
	case "cos":
		return math.Cos(args[0])
	case "tan":
		return math.Tan(args[0])
	case "exp":
		return math.Exp(args[0])
	case "ln":
		return math.Log(args[0])
	case "log10":
		return math.Log10(args[0])
	case "min":
		return math.Min(args[0], args[1])
	case "max":
		return math.Max(args[0], args[1])
	}
	return 0
}
