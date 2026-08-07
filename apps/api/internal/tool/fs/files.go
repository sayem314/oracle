package fs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

func New() []tool.Tool {
	return []tool.Tool{
		fileReadTool(),
		fileWriteTool(),
		fileListTool(),
		fileDeleteTool(),
		filePatchTool(),
	}
}

const maxReadBytes = 512 << 10

func fileReadTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "file_read",
			Description: "Read a local text file and return its contents. path is an absolute or " +
				"relative filesystem path. Files larger than 512 KiB are truncated with a notice. " +
				"Binary files will not decode cleanly.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("file_read: %w", err)
			}
			if strings.TrimSpace(in.Path) == "" {
				return "", errors.New("file_read: path is required")
			}
			f, err := os.Open(filepath.Clean(in.Path))
			if err != nil {
				return "", fmt.Errorf("file_read: %w", err)
			}
			defer f.Close() //nolint:errcheck

			data, err := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
			if err != nil {
				return "", fmt.Errorf("file_read: %w", err)
			}
			if len(data) <= maxReadBytes {
				return string(data), nil
			}
			return string(data[:maxReadBytes]) + "\n[truncated: file exceeds 512 KiB]", nil
		},
	}
}

func fileWriteTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "file_write",
			Description: "Write content to a local file, creating it if it does not exist and " +
				"overwriting it if it does. path is an absolute or relative filesystem path. " +
				"content is the full text to write. Parent directories are created as needed.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("file_write: %w", err)
			}
			if strings.TrimSpace(in.Path) == "" {
				return "", errors.New("file_write: path is required")
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Clean(in.Path)), 0o700); err != nil {
				return "", fmt.Errorf("file_write: %w", err)
			}
			if err := os.WriteFile(filepath.Clean(in.Path), []byte(in.Content), 0o600); err != nil {
				return "", fmt.Errorf("file_write: %w", err)
			}
			return "wrote " + in.Path, nil
		},
	}
}

func fileListTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "file_list",
			Description: "List the entries in a directory. path is an absolute or relative " +
				"directory path. Returns one entry per line, with a trailing slash on subdirectories.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("file_list: %w", err)
			}
			if strings.TrimSpace(in.Path) == "" {
				return "", errors.New("file_list: path is required")
			}
			entries, err := os.ReadDir(filepath.Clean(in.Path))
			if err != nil {
				return "", fmt.Errorf("file_list: %w", err)
			}
			var sb strings.Builder
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				sb.WriteString(name)
				sb.WriteString("\n")
			}
			return strings.TrimRight(sb.String(), "\n"), nil
		},
	}
}

func fileDeleteTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "file_delete",
			Description: "Delete a local file or empty directory. path is an absolute or relative " +
				"filesystem path. Returns an error if the entry does not exist.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("file_delete: %w", err)
			}
			if strings.TrimSpace(in.Path) == "" {
				return "", errors.New("file_delete: path is required")
			}
			if err := os.Remove(filepath.Clean(in.Path)); err != nil {
				return "", fmt.Errorf("file_delete: %w", err)
			}
			return "deleted " + in.Path, nil
		},
	}
}
