package fs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitignorePatterns(t *testing.T) {
	tests := []struct {
		name    string
		rules   string
		rel     string
		isDir   bool
		ignored bool
	}{
		{"basename file", "foo\n", "foo", false, true},
		{"basename nested", "foo\n", "a/foo", false, true},
		{"dir only matches dir", "build/\n", "build", true, true},
		{"dir only ignores file", "build/\n", "build", false, false},
		{"anchored root only", "/foo\n", "foo", false, true},
		{"anchored not nested", "/foo\n", "a/foo", false, false},
		{"middle slash anchored", "a/b\n", "a/b", false, true},
		{"middle slash not nested", "a/b\n", "x/a/b", false, false},
		{"star glob", "*.log\n", "a/x.log", false, true},
		{"star does not cross slash", "*.log\n", "a/b.log/x", false, false},
		{"question glob", "a?\n", "ab", false, true},
		{"question one char", "a?\n", "a", false, false},
		{"double star middle", "a/**/b\n", "a/b", false, true},
		{"double star middle nested", "a/**/b\n", "a/x/y/b", false, true},
		{"double star prefix", "**/foo\n", "x/y/foo", false, true},
		{"double star suffix", "a/**\n", "a/x/y", false, true},
		{"double star alone", "**\n", "anything", false, true},
		{"comment ignored", "# foo\n", "foo", false, false},
		{"blank ignored", "\n", "foo", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := parseGitignore([]byte(tt.rules), "base")
			assert.Equal(t, tt.ignored, isGitignored(rules, "base", tt.rel, tt.isDir))
		})
	}
}

func TestGitignoreNegation(t *testing.T) {
	rules := parseGitignore([]byte("*.log\n!important.log\n"), "base")
	assert.True(t, isGitignored(rules, "base", "debug.log", false))
	assert.False(t, isGitignored(rules, "base", "important.log", false))
}

func TestGitignoreLastMatchWins(t *testing.T) {
	rules := parseGitignore([]byte("foo\n!foo\n"), "base")
	assert.False(t, isGitignored(rules, "base", "foo", false))
}

func TestLoadGitignoreRulesBoundedByRepo(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "repo", ".git"), 0o755))                                   //nolint:gosec // test fixture
	require.NoError(t, os.MkdirAll(filepath.Join(root, "repo", "src"), 0o755))                                    //nolint:gosec // test fixture
	require.NoError(t, os.WriteFile(filepath.Join(root, "repo", ".gitignore"), []byte("node_modules/\n"), 0o644)) //nolint:gosec // test fixture
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secret.txt\n"), 0o644))            //nolint:gosec // test fixture

	rules := loadGitignoreRules(filepath.Join(root, "repo", "src"))
	require.Len(t, rules, 1)
	assert.True(t, isGitignored(rules, filepath.Join(root, "repo", "src"), "node_modules", true))
	assert.False(t, isGitignored(rules, filepath.Join(root, "repo", "src"), "secret.txt", false))
}

func TestLoadGitignoreRulesOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("foo\n"), 0o644)) //nolint:gosec // test fixture

	assert.Nil(t, loadGitignoreRules(dir))
}

func TestLoadGitignoreRulesNestedPrecedence(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "repo", ".git"), 0o755))                                                //nolint:gosec // test fixture
	require.NoError(t, os.MkdirAll(filepath.Join(root, "repo", "sub"), 0o755))                                                 //nolint:gosec // test fixture
	require.NoError(t, os.WriteFile(filepath.Join(root, "repo", ".gitignore"), []byte("*.log\n!keep.log\n/top.txt\n"), 0o644)) //nolint:gosec // test fixture
	require.NoError(t, os.WriteFile(filepath.Join(root, "repo", "sub", ".gitignore"), []byte("keep.log\n"), 0o644))            //nolint:gosec // test fixture

	rules := loadGitignoreRules(filepath.Join(root, "repo", "sub"))
	require.Len(t, rules, 4)
	assert.True(t, isGitignored(rules, filepath.Join(root, "repo", "sub"), "keep.log", false))
	assert.True(t, isGitignored(rules, filepath.Join(root, "repo", "sub"), "debug.log", false))
	assert.False(t, isGitignored(rules, filepath.Join(root, "repo", "sub"), "top.txt", false))
}
