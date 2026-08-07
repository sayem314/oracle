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

// resolvePath anchors a tool path against an optional cwd. Absolute paths win;
// an empty cwd leaves the path relative to the process working directory.
func resolvePath(cwd, path string) string {
	p := filepath.Clean(path)
	if !filepath.IsAbs(p) && strings.TrimSpace(cwd) != "" {
		p = filepath.Join(filepath.Clean(cwd), p)
	}
	return p
}

func fileReadTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer","minimum":0,"description":"The line number to start reading from (1-indexed)"},"limit":{"type":"integer","minimum":0,"description":"The maximum number of lines to read (defaults to 2000)"},"cwd":{"type":"string","description":"Base directory for resolving a relative path (default: the process working directory)"}},"required":["path"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "file_read",
			Description: "Read a local text file and return its contents as numbered lines. path is an " +
				"absolute or relative filesystem path, resolved against the optional cwd when " +
				"relative. offset is the 1-indexed line to start from " +
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
				Cwd    string `json:"cwd"`
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
			f, err := os.Open(resolvePath(in.Cwd, in.Path))
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
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"},"cwd":{"type":"string","description":"Base directory for resolving a relative path (default: the process working directory)"}},"required":["path","content"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "file_write",
			Description: "Write content to a local file, creating it if it does not exist and " +
				"overwriting it if it does. path is an absolute or relative filesystem path, " +
				"resolved against the optional cwd when relative. " +
				"content is the full text to write. Parent directories are created as needed.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path    string `json:"path"`
				Content string `json:"content"`
				Cwd     string `json:"cwd"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("file_write: %w", err)
			}
			if strings.TrimSpace(in.Path) == "" {
				return "", errors.New("file_write: path is required")
			}
			target := resolvePath(in.Cwd, in.Path)
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return "", fmt.Errorf("file_write: %w", err)
			}
			if err := os.WriteFile(target, []byte(in.Content), 0o600); err != nil {
				return "", fmt.Errorf("file_write: %w", err)
			}
			return "wrote " + in.Path, nil
		},
	}
}

func fileListTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer","minimum":0,"description":"The entry number to start from (1-indexed)"},"limit":{"type":"integer","minimum":0,"description":"The maximum number of entries to list (defaults to 2000)"},"hidden":{"type":"boolean","description":"Include hidden files (default false)"},"ignore":{"type":"boolean","description":"Apply .gitignore rules (default true)"},"cwd":{"type":"string","description":"Base directory for resolving a relative path (default: the process working directory)"}},"required":["path"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "file_list",
			Description: "List the entries in a directory, one per line with a trailing slash on " +
				"subdirectories. path is an absolute or relative directory path, resolved " +
				"against the optional cwd when relative. offset is the " +
				"1-indexed entry to start from (default 1) and limit is the maximum number of " +
				"entries (default 2000), so large directories can be paged by passing offset. " +
				"offset and limit apply to the filtered list. Hidden files are omitted by default " +
				"and .git is never listed; pass hidden to include dotfiles. .gitignore rules found " +
				"from the directory up to the repo root are applied by default, which hides build " +
				"output and dependency directories such as node_modules; pass ignore to list " +
				"everything.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path   string `json:"path"`
				Offset int    `json:"offset"`
				Limit  int    `json:"limit"`
				Hidden bool   `json:"hidden"`
				Ignore *bool  `json:"ignore"`
				Cwd    string `json:"cwd"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("file_list: %w", err)
			}
			if strings.TrimSpace(in.Path) == "" {
				return "", errors.New("file_list: path is required")
			}
			offset := in.Offset
			if offset <= 0 {
				offset = 1
			}
			limit := in.Limit
			if limit <= 0 {
				limit = defaultReadLimit
			}
			ignore := in.Ignore == nil || *in.Ignore
			listDir := resolvePath(in.Cwd, in.Path)
			listDirAbs, err := filepath.Abs(listDir)
			if err != nil {
				listDirAbs = listDir
			}
			entries, err := os.ReadDir(listDir)
			if err != nil {
				return "", fmt.Errorf("file_list: %w", err)
			}
			var rules []gitignoreRule
			if ignore {
				rules = loadGitignoreRules(listDirAbs)
			}

			names := make([]string, 0, len(entries))
			for _, e := range entries {
				name := e.Name()
				if name == ".git" {
					continue
				}
				if !in.Hidden && strings.HasPrefix(name, ".") {
					continue
				}
				isDir := e.IsDir()
				if len(rules) > 0 && isGitignored(rules, listDirAbs, name, isDir) {
					continue
				}
				if isDir {
					name += "/"
				}
				names = append(names, name)
			}

			total := len(names)
			if offset > 1 && total < offset {
				return "", fmt.Errorf("file_list: Offset %d is out of range for this directory (%d entries)", offset, total)
			}
			start := offset - 1
			end := min(start+limit, total)
			shown := names[start:end]
			next := start + len(shown) + 1
			out := strings.Join(shown, "\n")
			if next <= total {
				out += fmt.Sprintf("\n\n(Showing entries %d-%d of %d. Use offset=%d to continue.)", offset, offset+len(shown)-1, total, next)
			} else {
				out += fmt.Sprintf("\n\n(%d entries)", total)
			}
			return out, nil
		},
	}
}

func fileDeleteTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"cwd":{"type":"string","description":"Base directory for resolving a relative path (default: the process working directory)"}},"required":["path"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "file_delete",
			Description: "Delete a local file or empty directory. path is an absolute or relative " +
				"filesystem path, resolved against the optional cwd when relative. " +
				"Returns an error if the entry does not exist.",
			Parameters: schema,
		},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path string `json:"path"`
				Cwd  string `json:"cwd"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("file_delete: %w", err)
			}
			if strings.TrimSpace(in.Path) == "" {
				return "", errors.New("file_delete: path is required")
			}
			if err := os.Remove(resolvePath(in.Cwd, in.Path)); err != nil {
				return "", fmt.Errorf("file_delete: %w", err)
			}
			return "deleted " + in.Path, nil
		},
	}
}
