package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"

	osexec "os/exec"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

const (
	defaultTimeout = 30 * time.Second
	maxTimeout     = 5 * time.Minute
	maxOutputBytes = 512 << 10
)

func New() []tool.Tool {
	return []tool.Tool{execTool()}
}

func execTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":300},"cwd":{"type":"string","description":"Directory the command runs in (default: the process working directory)"}},"required":["command"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "exec",
			Description: "Run a shell command and return its combined output. command is executed " +
				"via sh -c in the optional cwd or oracle's own working directory and environment, " +
				"so pipes, redirects, " +
				"and expansions work. A non-zero exit code is reported in the result, not as an " +
				"error. timeout_seconds is optional, defaults to 30, max 300. Output beyond 512 KiB " +
				"is truncated with a notice.",
			Parameters: schema,
		},
		Execute: execute,
	}
}

func execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
		Cwd            string `json:"cwd"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("exec: %w", err)
	}
	if strings.TrimSpace(in.Command) == "" {
		return "", errors.New("exec: command is required")
	}

	timeout := defaultTimeout
	if in.TimeoutSeconds > 0 {
		timeout = time.Duration(in.TimeoutSeconds) * time.Second
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := osexec.CommandContext(runCtx, "sh", "-c", in.Command) //nolint:gosec // running user-approved commands is the tool's purpose
	if in.Cwd != "" {
		cmd.Dir = in.Cwd
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// CommandContext kills only the direct process, so override Cancel to take
	// down the whole process group when the run context ends.
	cmd.Cancel = func() error {
		if cmd.Process == nil || cmd.Process.Pid <= 0 {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	var out cappedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return formatOutput(out.String(), out.capped, fmt.Sprintf("[timed out after %s]", timeout)), nil
		}
		if runCtx.Err() != nil {
			return "", fmt.Errorf("exec: %w", runCtx.Err())
		}
		var exitErr *osexec.ExitError
		if errors.As(err, &exitErr) {
			return formatOutput(out.String(), out.capped, fmt.Sprintf("[exit code %d]", exitErr.ExitCode())), nil
		}
		return "", fmt.Errorf("exec: %w", err)
	}
	return formatOutput(out.String(), out.capped, ""), nil
}

type cappedBuffer struct {
	b      bytes.Buffer
	capped bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := maxOutputBytes - c.b.Len()
	if remaining <= 0 {
		c.capped = true
		return len(p), nil
	}
	if len(p) > remaining {
		c.b.Write(p[:remaining])
		c.capped = true
		return len(p), nil
	}
	return c.b.Write(p)
}

func (c *cappedBuffer) String() string { return c.b.String() }

func formatOutput(out string, truncated bool, notice string) string {
	out = strings.TrimSpace(out)
	if out == "" && notice == "" {
		out = "(no output)"
	}
	if truncated {
		out += "\n[output truncated at 512 KiB]"
	}
	if notice != "" {
		out += "\n" + notice
	}
	return out
}
