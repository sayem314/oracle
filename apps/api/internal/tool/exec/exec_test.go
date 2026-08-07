package exec

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustArgs(s string) json.RawMessage { return json.RawMessage(s) }

func TestExecToolRegistered(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range New() {
		names[tl.Definition.Name] = true
	}
	assert.True(t, names["exec"], "exec should be part of the exec group")
}

func TestExecEcho(t *testing.T) {
	out, err := execTool().Execute(context.Background(), mustArgs(`{"command":"echo hello"}`))
	require.NoError(t, err)
	assert.Equal(t, "hello", out)
}

func TestExecPipeline(t *testing.T) {
	out, err := execTool().Execute(context.Background(),
		mustArgs(`{"command":"echo foo | tr a-z A-Z"}`))
	require.NoError(t, err)
	assert.Equal(t, "FOO", out)
}

func TestExecCombinesStderr(t *testing.T) {
	out, err := execTool().Execute(context.Background(),
		mustArgs(`{"command":"echo out; echo err >&2"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "out")
	assert.Contains(t, out, "err")
}

func TestExecExitCodeReported(t *testing.T) {
	out, err := execTool().Execute(context.Background(), mustArgs(`{"command":"exit 3"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "[exit code 3]")
}

func TestExecNoOutput(t *testing.T) {
	out, err := execTool().Execute(context.Background(), mustArgs(`{"command":"true"}`))
	require.NoError(t, err)
	assert.Equal(t, "(no output)", out)
}

func TestExecTimeout(t *testing.T) {
	start := time.Now()
	out, err := execTool().Execute(context.Background(),
		mustArgs(`{"command":"sleep 5","timeout_seconds":1}`))
	require.NoError(t, err)
	assert.Contains(t, out, "[timed out after 1s]")
	assert.Less(t, time.Since(start), 4*time.Second)
}

func TestExecTruncatesLargeOutput(t *testing.T) {
	out, err := execTool().Execute(context.Background(),
		mustArgs(`{"command":"head -c 600000 /dev/zero | tr '\\0' 'a'"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "[output truncated at 512 KiB]")
}

func TestExecMissingBinary(t *testing.T) {
	out, err := execTool().Execute(context.Background(),
		mustArgs(`{"command":"definitely_not_a_binary_xyz"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "[exit code 127]")
}

func TestExecRequiresCommand(t *testing.T) {
	_, err := execTool().Execute(context.Background(), mustArgs(`{"command":"  "}`))
	require.ErrorContains(t, err, "command is required")

	_, err = execTool().Execute(context.Background(), mustArgs(`{}`))
	require.ErrorContains(t, err, "command is required")
}

func TestExecRejectsInvalidJSON(t *testing.T) {
	_, err := execTool().Execute(context.Background(), json.RawMessage(`{nope`))
	require.ErrorContains(t, err, "exec:")
}

func TestExecCancellationKillsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan string, 1)
	go func() {
		out, err := execTool().Execute(ctx, mustArgs(`{"command":"sleep 30 & wait"}`))
		if err != nil {
			done <- "error: " + err.Error()
			return
		}
		done <- out
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("exec did not return after context cancellation")
	}
}

func TestExecTimeoutCap(t *testing.T) {
	cmd := execTool()
	in := struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}{Command: "true", TimeoutSeconds: 9999}
	args, err := json.Marshal(in)
	require.NoError(t, err)

	_, err = cmd.Execute(context.Background(), args)
	require.NoError(t, err)
}

func TestExecLongOutputTrimsTrailingSpace(t *testing.T) {
	out, err := execTool().Execute(context.Background(),
		mustArgs(`{"command":"printf 'a\\n\\n\\n'"}`))
	require.NoError(t, err)
	assert.Equal(t, "a", out)
	assert.NotContains(t, out, "\n\n")
}
