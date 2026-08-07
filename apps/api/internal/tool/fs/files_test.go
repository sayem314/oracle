package fs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustArgs(s string) json.RawMessage { return json.RawMessage(s) }

func TestFileToolsRegistered(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range New() {
		names[tl.Definition.Name] = true
	}
	for _, want := range []string{"file_read", "file_write", "file_list", "file_delete", "file_patch"} {
		assert.True(t, names[want], "%s should be part of the fs group", want)
	}
}

func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.txt")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644)) //nolint:gosec // test fixture
	return p
}

func writeTestDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)) //nolint:gosec // test fixture
	}
	return dir
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
	assert.Equal(t, "1: hello world\n\n(End of file - total 1 lines)", got)
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

func TestFileReadByteCap(t *testing.T) {
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = strings.Repeat("a", 3000)
	}
	p := writeTestFile(t, strings.Join(lines, "\n"))

	got, err := fileReadTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`"}`))
	require.NoError(t, err)
	assert.Contains(t, got, "1: "+strings.Repeat("a", 2000)+"... (line truncated to 2000 chars)")
	assert.Contains(t, got, "(Output capped at 50 KB. Showing lines 1-25. Use offset=26 to continue.)")
}

func TestFileReadWindowOffset(t *testing.T) {
	p := writeTestFile(t, "l1\nl2\nl3\nl4\nl5")

	got, err := fileReadTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","offset":3}`))
	require.NoError(t, err)
	assert.Equal(t, "3: l3\n4: l4\n5: l5\n\n(End of file - total 5 lines)", got)
}

func TestFileReadWindowLimit(t *testing.T) {
	p := writeTestFile(t, "l1\nl2\nl3\nl4\nl5")

	got, err := fileReadTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","offset":2,"limit":2}`))
	require.NoError(t, err)
	assert.Equal(t, "2: l2\n3: l3\n\n(Showing lines 2-3 of 5. Use offset=4 to continue.)", got)
}

func TestFileReadOffsetOutOfRange(t *testing.T) {
	p := writeTestFile(t, "l1\nl2\nl3")

	_, err := fileReadTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","offset":10}`))
	require.ErrorContains(t, err, "file_read: Offset 10 is out of range for this file (3 lines)")
}

func TestFileReadOffsetZeroCoerces(t *testing.T) {
	p := writeTestFile(t, "l1\nl2\nl3")

	got, err := fileReadTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","offset":0}`))
	require.NoError(t, err)
	assert.Contains(t, got, "1: l1")
}

func TestFileReadLineTruncation(t *testing.T) {
	p := writeTestFile(t, strings.Repeat("a", 2500))

	got, err := fileReadTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`"}`))
	require.NoError(t, err)
	assert.Equal(t, "1: "+strings.Repeat("a", 2000)+"... (line truncated to 2000 chars)"+"\n\n(End of file - total 1 lines)", got)
}

func TestFileReadEmpty(t *testing.T) {
	p := writeTestFile(t, "")

	got, err := fileReadTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`"}`))
	require.NoError(t, err)
	assert.Equal(t, "\n\n(End of file - total 0 lines)", got)
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

func TestFileListWindow(t *testing.T) {
	p := writeTestDir(t, "a.txt", "b.txt", "c.txt", "d.txt", "e.txt")

	got, err := fileListTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","offset":2,"limit":2,"ignore":false}`))
	require.NoError(t, err)
	assert.Equal(t, "b.txt\nc.txt\n\n(Showing entries 2-3 of 5. Use offset=4 to continue.)", got)
}

func TestFileListCompleteMessage(t *testing.T) {
	p := writeTestDir(t, "a.txt", "b.txt")

	got, err := fileListTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","ignore":false}`))
	require.NoError(t, err)
	assert.Equal(t, "a.txt\nb.txt\n\n(2 entries)", got)
}

func TestFileListOffsetOutOfRange(t *testing.T) {
	p := writeTestDir(t, "a.txt", "b.txt")

	_, err := fileListTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","offset":5,"ignore":false}`))
	require.ErrorContains(t, err, "file_list: Offset 5 is out of range for this directory (2 entries)")
}

func TestFileListHidesDotfilesAndGitByDefault(t *testing.T) {
	p := writeTestDir(t, "a.txt", "b.txt", ".env")
	require.NoError(t, os.MkdirAll(filepath.Join(p, ".git"), 0o755)) //nolint:gosec // test fixture

	got, err := fileListTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","ignore":false}`))
	require.NoError(t, err)
	assert.Equal(t, "a.txt\nb.txt\n\n(2 entries)", got)
}

func TestFileListHiddenFlag(t *testing.T) {
	p := writeTestDir(t, "a.txt", ".env")
	require.NoError(t, os.MkdirAll(filepath.Join(p, ".git"), 0o755)) //nolint:gosec // test fixture

	got, err := fileListTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","hidden":true,"ignore":false}`))
	require.NoError(t, err)
	assert.Contains(t, got, ".env")
	assert.NotContains(t, got, ".git")
}

func TestFileListGitignoreAppliedByDefault(t *testing.T) {
	p := writeTestDir(t, "app.go")
	require.NoError(t, os.MkdirAll(filepath.Join(p, ".git"), 0o755))                                   //nolint:gosec // test fixture
	require.NoError(t, os.MkdirAll(filepath.Join(p, "node_modules"), 0o755))                           //nolint:gosec // test fixture
	require.NoError(t, os.WriteFile(filepath.Join(p, ".gitignore"), []byte("node_modules/\n"), 0o644)) //nolint:gosec // test fixture

	got, err := fileListTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`"}`))
	require.NoError(t, err)
	assert.Contains(t, got, "app.go")
	assert.NotContains(t, got, "node_modules")
}

func TestFileListIgnoreDisabled(t *testing.T) {
	p := writeTestDir(t, "app.go", "node_modules")
	require.NoError(t, os.MkdirAll(filepath.Join(p, ".git"), 0o755))                                   //nolint:gosec // test fixture
	require.NoError(t, os.WriteFile(filepath.Join(p, ".gitignore"), []byte("node_modules/\n"), 0o644)) //nolint:gosec // test fixture

	got, err := fileListTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","ignore":false}`))
	require.NoError(t, err)
	assert.Contains(t, got, "node_modules")
}

func TestFileListFilterThenWindow(t *testing.T) {
	p := writeTestDir(t, "a.txt", "b.txt", "c.txt", "d.txt", "e.txt", "f.txt")
	require.NoError(t, os.MkdirAll(filepath.Join(p, ".git"), 0o755))                                  //nolint:gosec // test fixture
	require.NoError(t, os.WriteFile(filepath.Join(p, ".gitignore"), []byte("c.txt\nf.txt\n"), 0o644)) //nolint:gosec // test fixture

	got, err := fileListTool().Execute(context.Background(), mustArgs(`{"path":"`+p+`","offset":2,"limit":2}`))
	require.NoError(t, err)
	assert.Equal(t, "b.txt\nd.txt\n\n(Showing entries 2-3 of 4. Use offset=4 to continue.)", got)
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
