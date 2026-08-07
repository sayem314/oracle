package fs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

func filePatchTool() tool.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"diff":{"type":"string"}},"required":["path","diff"],"additionalProperties":false}`)
	return tool.Tool{
		Definition: llm.Tool{
			Name: "file_patch",
			Description: "Apply a unified diff to a local text file. path is the file to edit, diff is a " +
				"git-style patch with @@ hunks against the file's current content, e.g. produced by " +
				"git diff. Read the file first so hunk line numbers and context match exactly. One " +
				"file per patch; diffs that create or delete files are rejected. Diffs larger than " +
				"512 KiB are rejected.",
			Parameters: schema,
		},
		Execute: executePatch,
	}
}

const maxDiffBytes = 512 << 10

func executePatch(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("file_patch: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", errors.New("file_patch: path is required")
	}
	if len(in.Diff) == 0 {
		return "", errors.New("file_patch: diff is required")
	}
	if len(in.Diff) > maxDiffBytes {
		return "", fmt.Errorf("file_patch: diff exceeds %d bytes", maxDiffBytes)
	}

	files, _, err := gitdiff.Parse(strings.NewReader(in.Diff))
	if err != nil {
		return "", fmt.Errorf("file_patch: invalid diff: %w", err)
	}
	if len(files) == 0 {
		return "", errors.New("file_patch: diff contains no file changes")
	}
	if len(files) != 1 {
		return "", fmt.Errorf("file_patch: expected exactly one file in the diff, got %d", len(files))
	}
	f := files[0]
	if f.IsNew || f.IsDelete {
		return "", errors.New("file_patch: creating or deleting files is not supported")
	}
	if f.IsBinary {
		return "", errors.New("file_patch: binary diffs are not supported")
	}
	if len(f.TextFragments) == 0 {
		return "", errors.New("file_patch: diff contains no changes")
	}

	target := filepath.Clean(in.Path)
	src, err := os.Open(target)
	if err != nil {
		return "", fmt.Errorf("file_patch: %w", err)
	}
	defer src.Close() //nolint:errcheck

	var patched bytes.Buffer
	if err := gitdiff.Apply(&patched, src, f); err != nil {
		return "", fmt.Errorf("file_patch: apply failed: %w", err)
	}

	if err := os.WriteFile(target, patched.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("file_patch: %w", err)
	}

	return fmt.Sprintf("applied %d hunks to %s", len(f.TextFragments), in.Path), nil
}
