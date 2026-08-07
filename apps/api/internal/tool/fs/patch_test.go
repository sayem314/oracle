package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilePatchToolRegistered(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range New() {
		names[tl.Definition.Name] = true
	}
	assert.True(t, names["file_patch"], "file_patch should be part of the fs group")
}

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "notes.txt")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644)) //nolint:gosec // test fixture
	return p
}

func TestFilePatchSingleHunk(t *testing.T) {
	p := writeFixture(t, "line one\nline two\nold line\nline four\n")

	out, err := filePatchTool().Execute(context.Background(), mustArgs(`{
		"path":"`+p+`",
		"diff":"--- a/notes.txt\n+++ b/notes.txt\n@@ -1,4 +1,5 @@\n line one\n line two\n-old line\n+new line\n+extra line\n line four\n"
	}`))
	require.NoError(t, err)
	assert.Contains(t, out, "applied 1 hunks")

	data, _ := os.ReadFile(p) //nolint:gosec // test fixture
	assert.Equal(t, "line one\nline two\nnew line\nextra line\nline four\n", string(data))
}

func TestFilePatchMultipleHunks(t *testing.T) {
	p := writeFixture(t, "alpha\nbeta\ngamma\ndelta\nepsilon\n")

	out, err := filePatchTool().Execute(context.Background(), mustArgs(`{
		"path":"`+p+`",
		"diff":"--- a/notes.txt\n+++ b/notes.txt\n@@ -1,2 +1,2 @@\n-alpha\n+ALPHA\n beta\n@@ -4,2 +4,2 @@\n delta\n-epsilon\n+EPSILON\n"
	}`))
	require.NoError(t, err)
	assert.Contains(t, out, "applied 2 hunks")

	data, _ := os.ReadFile(p) //nolint:gosec // test fixture
	assert.Equal(t, "ALPHA\nbeta\ngamma\ndelta\nEPSILON\n", string(data))
}

func TestFilePatchContextMismatch(t *testing.T) {
	p := writeFixture(t, "line one\nline two\nline three\n")

	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{
		"path":"`+p+`",
		"diff":"--- a/notes.txt\n+++ b/notes.txt\n@@ -1,3 +1,3 @@\n line one\n-drifted line\n+new line\n line three\n"
	}`))
	require.ErrorContains(t, err, "apply failed")

	data, _ := os.ReadFile(p) //nolint:gosec // test fixture
	assert.Equal(t, "line one\nline two\nline three\n", string(data))
}

func TestFilePatchRejectsCreate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new.txt")

	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{
		"path":"`+p+`",
		"diff":"--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+content\n"
	}`))
	require.ErrorContains(t, err, "creating or deleting files is not supported")
}

func TestFilePatchRejectsDelete(t *testing.T) {
	p := writeFixture(t, "content\n")

	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{
		"path":"`+p+`",
		"diff":"--- a/notes.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-content\n"
	}`))
	require.ErrorContains(t, err, "creating or deleting files is not supported")
}

func TestFilePatchRejectsMultipleFiles(t *testing.T) {
	p := writeFixture(t, "content\n")

	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{
		"path":"`+p+`",
		"diff":"--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-content\n+CONTENT\n--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-content\n+CONTENT\n"
	}`))
	require.ErrorContains(t, err, "exactly one file")
}

func TestFilePatchRejectsInvalidDiff(t *testing.T) {
	p := writeFixture(t, "content\n")

	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{
		"path":"`+p+`",
		"diff":"not a diff at all"
	}`))
	require.ErrorContains(t, err, "no file changes")
}

func TestFilePatchRejectsEmptyDiff(t *testing.T) {
	p := writeFixture(t, "content\n")

	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","diff":""}`))
	require.ErrorContains(t, err, "diff is required")
}

func TestFilePatchRejectsLargeDiff(t *testing.T) {
	p := writeFixture(t, "content\n")
	huge := strings.Repeat("x", maxDiffBytes+1)

	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","diff":"`+huge+`"}`))
	require.ErrorContains(t, err, "exceeds")
}

func TestFilePatchMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.txt")

	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{
		"path":"`+p+`",
		"diff":"--- a/missing.txt\n+++ b/missing.txt\n@@ -1 +1 @@\n-content\n+CONTENT\n"
	}`))
	require.ErrorContains(t, err, "file_patch")
}

func TestFilePatchRequiresPath(t *testing.T) {
	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{"path":"","diff":"x"}`))
	require.ErrorContains(t, err, "path is required")
}

func TestFilePatchCwd(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(p, []byte("old line\n"), 0o644)) //nolint:gosec // test fixture

	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{
		"path":"notes.txt",
		"cwd":"`+dir+`",
		"diff":"--- a/notes.txt\n+++ b/notes.txt\n@@ -1 +1 @@\n-old line\n+new line\n"
	}`))
	require.NoError(t, err)

	data, _ := os.ReadFile(p) //nolint:gosec // test fixture
	assert.Equal(t, "new line\n", string(data))
}

func TestFilePatchNoChanges(t *testing.T) {
	p := writeFixture(t, "content\n")

	_, err := filePatchTool().Execute(context.Background(), mustArgs(`{
		"path":"`+p+`",
		"diff":"--- a/notes.txt\n+++ b/notes.txt\n"
	}`))
	require.ErrorContains(t, err, "no file changes")
}
