package fs

import (
	"bufio"
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

const (
	maxReadBytes     = 50 << 10
	maxLineChars     = 2000
	defaultReadLimit = 2000
	lineTruncSuffix  = "... (line truncated to 2000 chars)"
)

func fileReadTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer","minimum":0,"description":"The line number to start reading from (1-indexed)"},"limit":{"type":"integer","minimum":0,"description":"The maximum number of lines to read (defaults to 2000)"}},"required":["path"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "file_read",
			Description: "Read a local text file and return its contents as numbered lines. path is an " +
				"absolute or relative filesystem path. offset is the 1-indexed line to start from " +
				"(default 1) and limit is the maximum number of lines to read (default 2000). Each " +
				"line is prefixed with its line number so file_patch hunks can be targeted " +
				"precisely. Output is capped at 50 KB and lines longer than 2000 chars are " +
				"truncated. The result names the lines shown and the offset to continue from. " +
				"Binary files will not decode cleanly.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path   string `json:"path"`
				Offset int    `json:"offset"`
				Limit  int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("file_read: %w", err)
			}
			if strings.TrimSpace(in.Path) == "" {
				return "", errors.New("file_read: path is required")
			}
			offset := in.Offset
			if offset <= 0 {
				offset = 1
			}
			limit := in.Limit
			if limit <= 0 {
				limit = defaultReadLimit
			}
			f, err := os.Open(filepath.Clean(in.Path))
			if err != nil {
				return "", fmt.Errorf("file_read: %w", err)
			}
			defer f.Close() //nolint:errcheck

			r := bufio.NewReader(f)
			readLine := func() (string, error) {
				var b strings.Builder
				for {
					chunk, isPrefix, err := r.ReadLine()
					if b.Len() <= maxLineChars {
						b.Write(chunk)
					}
					if err != nil {
						return b.String(), err
					}
					if !isPrefix {
						return b.String(), nil
					}
				}
			}

			var (
				lines []string
				total int
				bytes int
				cut   bool
				more  bool
			)
			for {
				line, err := readLine()
				if line == "" && errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return "", fmt.Errorf("file_read: %w", err)
				}
				total++
				if total < offset {
					continue
				}
				if len(lines) >= limit {
					more = true
					continue
				}
				if len(line) > maxLineChars {
					line = line[:maxLineChars] + lineTruncSuffix
				}
				if bytes+len(line)+1 > maxReadBytes {
					cut = true
					more = true
					break
				}
				lines = append(lines, fmt.Sprintf("%d: %s", total, line))
				bytes += len(line) + 1
			}

			if offset > 1 && total < offset {
				return "", fmt.Errorf("file_read: Offset %d is out of range for this file (%d lines)", offset, total)
			}

			var end string
			switch {
			case cut:
				end = fmt.Sprintf("(Output capped at 50 KB. Showing lines %d-%d. Use offset=%d to continue.)", offset, offset+len(lines)-1, offset+len(lines))
			case more:
				end = fmt.Sprintf("(Showing lines %d-%d of %d. Use offset=%d to continue.)", offset, offset+len(lines)-1, total, offset+len(lines))
			default:
				end = fmt.Sprintf("(End of file - total %d lines)", total)
			}
			return strings.Join(lines, "\n") + "\n\n" + end, nil
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
