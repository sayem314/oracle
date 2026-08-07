package loop_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/tool"
	"github.com/sayem314/oracle/apps/api/internal/tool/loop"
)

type fakeControl struct {
	enabled  *bool
	interval *string
	reply    string
}

func (f *fakeControl) SetLoop(_ context.Context, enabled *bool, interval *string) (string, error) {
	f.enabled, f.interval = enabled, interval
	return f.reply, nil
}

func run(args string) (string, error) {
	return loop.New()[0].Execute(toolContext(), json.RawMessage(args))
}

func toolContext() context.Context {
	return tool.WithLoopControl(context.Background(), &fakeControl{reply: "session loop enabled"})
}

func TestSetLoopDelegatesToControl(t *testing.T) {
	res, err := run(`{"enabled":true,"interval":"5m"}`)
	require.NoError(t, err)
	assert.Equal(t, "session loop enabled", res)
}

func TestSetLoopPassesIntervalOnly(t *testing.T) {
	_, err := run(`{"interval":"30s"}`)
	require.NoError(t, err)
}

func TestSetLoopWithoutControlFails(t *testing.T) {
	_, err := loop.New()[0].Execute(context.Background(), json.RawMessage(`{"enabled":true}`))
	require.ErrorContains(t, err, "no session loop control")
}

func TestSetLoopRequiresAField(t *testing.T) {
	_, err := run(`{}`)
	require.ErrorContains(t, err, "provide enabled and/or interval")
}

func TestSetLoopInvalidArguments(t *testing.T) {
	_, err := run(`{"enabled":"yes"}`)
	require.ErrorContains(t, err, "set_loop")
}
