package fs

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type gitignoreRule struct {
	baseDir  string
	negated  bool
	dirOnly  bool
	anchored bool
	segs     []string
}

func parseGitignore(data []byte, baseDir string) []gitignoreRule {
	var rules []gitignoreRule
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := gitignoreRule{baseDir: baseDir}
		if strings.HasPrefix(line, "!") {
			r.negated = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if strings.HasPrefix(line, "/") {
			r.anchored = true
			line = line[1:]
		}
		if line == "" {
			continue
		}
		if strings.Contains(line, "/") {
			r.anchored = true
		}
		r.segs = strings.Split(line, "/")
		rules = append(rules, r)
	}
	return rules
}

func readGitignore(p, baseDir string) []gitignoreRule {
	f, err := os.Open(filepath.Clean(p))
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	return parseGitignore(data, baseDir)
}

func loadGitignoreRules(dir string) []gitignoreRule {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	stack := []gitignoreRule{}
	repoFound := false
	for d := abs; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			repoFound = true
		}
		stack = append(stack, readGitignore(filepath.Join(d, ".gitignore"), d)...)
		if repoFound || d == filepath.Dir(d) {
			break
		}
	}
	if !repoFound {
		return nil
	}
	rules := make([]gitignoreRule, 0, len(stack))
	for i := len(stack) - 1; i >= 0; i-- {
		rules = append(rules, stack[i])
	}
	return rules
}

func isGitignored(rules []gitignoreRule, dir, name string, isDir bool) bool {
	ignored := false
	for _, r := range rules {
		rel, err := filepath.Rel(r.baseDir, filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if r.match(filepath.ToSlash(rel), isDir) {
			ignored = !r.negated
		}
	}
	return ignored
}

func (r gitignoreRule) match(rel string, isDir bool) bool {
	if r.dirOnly && !isDir {
		return false
	}
	segs := strings.Split(rel, "/")
	if !r.anchored {
		for i := 0; i <= len(segs)-len(r.segs); i++ {
			if matchSegs(r.segs, segs[i:]) {
				return true
			}
		}
		return false
	}
	return matchSegs(r.segs, segs)
}

func matchSegs(pat, segs []string) bool {
	pi, si := 0, 0
	starPi, starSi := -1, 0
	for si < len(segs) {
		if pi < len(pat) {
			if pat[pi] == "**" {
				starPi, starSi = pi, si
				pi++
				continue
			}
			if matchSeg(pat[pi], segs[si]) {
				pi++
				si++
				continue
			}
		}
		if starPi >= 0 {
			pi = starPi + 1
			starSi++
			si = starSi
			continue
		}
		return false
	}
	for pi < len(pat) && pat[pi] == "**" {
		pi++
	}
	return pi == len(pat)
}

func matchSeg(pat, s string) bool {
	if !strings.ContainsAny(pat, "*?[") {
		return pat == s
	}
	ok, _ := path.Match(pat, s)
	return ok
}
