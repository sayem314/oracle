package tool

import "context"

// LoopControl lets the set_loop tool read and change the loop settings of the
// session the current run belongs to. The chat engine injects it into the
// tool execution context. Calls made outside a run see no control.
type LoopControl interface {
	SetLoop(ctx context.Context, enabled *bool, interval *string) (string, error)
}

type loopControlKey struct{}

// WithLoopControl attaches a session's loop control to a tool execution
// context.
func WithLoopControl(ctx context.Context, c LoopControl) context.Context {
	return context.WithValue(ctx, loopControlKey{}, c)
}

// LoopControlFrom returns the control attached by WithLoopControl.
func LoopControlFrom(ctx context.Context) (LoopControl, bool) {
	c, ok := ctx.Value(loopControlKey{}).(LoopControl)
	return c, ok
}
