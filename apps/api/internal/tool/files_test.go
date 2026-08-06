package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileToolsRegistered(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range NewBuiltin() {
		names[tl.Definition.Name] = true
	}
	for _, want := range []string{"file_read", "file_write", "file_list", "file_delete"} {
		assert.True(t, names[want], "%s should be part of NewBuiltin", want)
	}
}

func TestFileWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "nested", "note.txt")

	out, err := fileWriteTool().Execute(context.Background(), mustArgs(
		`{"path":"`+p+`","content":"hello world"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "wrote")

	got, err := fileReadTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`"}`))
	require.NoError(t, err)
	assert.Equal(t, "hello world", got)
}

func TestFileWriteOverwrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(p, []byte("old"), 0o644)) //nolint:gosec // test fixture

	_, err := fileWriteTool().Execute(context.Background(), mustArgs(
		`{"path":"`+p+`","content":"new"}`))
	require.NoError(t, err)

	data, _ := os.ReadFile(p) //nolint:gosec // test fixture
	assert.Equal(t, "new", string(data))
}

func TestFileReadMissing(t *testing.T) {
	_, err := fileReadTool().Execute(context.Background(),
		mustArgs(`{"path":"/nope/missing.txt"}`))
	require.ErrorContains(t, err, "file_read")
}

func TestFileReadTrimsLarge(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.txt")
	require.NoError(t, os.WriteFile(p, []byte(strings.Repeat("a", 600*1024)), 0o644)) //nolint:gosec // test fixture

	got, err := fileReadTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`"}`))
	require.NoError(t, err)
	assert.Contains(t, got, "[truncated: file exceeds 512 KiB]")
}

func TestFileList(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir"), 0o755))              //nolint:gosec // test fixture
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)) //nolint:gosec // test fixture

	got, err := fileListTool().Execute(context.Background(), mustArgs(`{"path":"`+dir+`"}`))
	require.NoError(t, err)
	assert.Contains(t, got, "a.txt")
	assert.Contains(t, got, "subdir/")
}

func TestFileDelete(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "del.txt")
	require.NoError(t, os.WriteFile(p, []byte("x"), 0o644)) //nolint:gosec // test fixture

	out, err := fileDeleteTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "deleted")
	_, err = os.Stat(p)
	assert.True(t, os.IsNotExist(err))
}

func TestFileDeleteMissing(t *testing.T) {
	_, err := fileDeleteTool().Execute(context.Background(),
		mustArgs(`{"path":"/nope/missing.txt"}`))
	require.ErrorContains(t, err, "file_delete")
}

func TestFileToolsRequirePath(t *testing.T) {
	_, err := fileReadTool().Execute(context.Background(), mustArgs(`{"path":""}`))
	require.ErrorContains(t, err, "path is required")

	_, err = fileWriteTool().Execute(context.Background(), mustArgs(`{"path":"","content":"x"}`))
	require.ErrorContains(t, err, "path is required")

	_, err = fileListTool().Execute(context.Background(), mustArgs(`{"path":""}`))
	require.ErrorContains(t, err, "path is required")

	_, err = fileDeleteTool().Execute(context.Background(), mustArgs(`{"path":""}`))
	require.ErrorContains(t, err, "path is required")
}
