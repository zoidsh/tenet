// Package source decides what tenet looks at: which files, which of their
// lines may carry a finding, and how they are cut into windows.
package source

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skip limits, chosen so that a call is never spent on something no rule can
// be judged against.
const (
	MaxFileBytes = 1 << 20
	sniffBytes   = 8 << 10
)

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
}

// Files whose contents must never leave the machine, whatever the tenets say.
var secretPatterns = []string{".env", ".env.*", "*.pem", "*.key", "id_rsa*", "*.p12", "*.pfx"}

// ConfigName and BaselineName are tenet's own files. They live here
// rather than in the packages that own them because those packages import
// this one, and the skip below has to name them.
const (
	ConfigName   = "tenets.yml"
	BaselineName = ".tenet-baseline.json"
)

// tenet's own files are never judged: both quote the tenets back, so a
// rule about narrating comments reads its own sentence in them as a
// violation.
var skipFiles = map[string]bool{ConfigName: true, BaselineName: true}

// Skip is a file that was not linted, and why.
type Skip struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// Options select what to collect. CommitMsg and PRText win over Paths, which
// win over Base, and with none of them the staged changes are linted.
type Options struct {
	Dir   string
	Base  string
	Paths []string

	// CommitMsg is the path of a commit message file to lint on its own, as
	// git hands it to a commit-msg hook.
	CommitMsg string

	// PRText is the path of a file holding a pull request's title and
	// description, to lint on its own.
	PRText string

	// Tenets are the ids an ignore directive may name.
	Tenets []string
}

// Set is everything one run looks at.
type Set struct {
	Root    string
	Files   []*File
	Skipped []Skip
}

// Windows are every window worth a call, in file and line order.
func (s *Set) Windows() []*Window {
	var out []*Window
	for _, f := range s.Files {
		for _, w := range f.Windows() {
			if w.HasReportable() {
				out = append(out, w)
			}
		}
	}
	return out
}

// Collect gathers the files to lint.
func Collect(ctx context.Context, opts Options) (*Set, error) {
	dir := opts.Dir
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root, repoErr := RepoRoot(ctx, abs)
	known := make(map[string]bool, len(opts.Tenets))
	for _, id := range opts.Tenets {
		known[id] = true
	}

	switch {
	case opts.CommitMsg != "":
		return collectCommitMsg(abs, opts.CommitMsg, known)
	case opts.PRText != "":
		return collectPRText(abs, opts.PRText, known)
	case len(opts.Paths) > 0:
		return collectPaths(ctx, abs, root, opts.Paths, known)
	case opts.Base != "":
		if repoErr != nil {
			return nil, repoErr
		}
		if err := checkRef(ctx, root, opts.Base); err != nil {
			return nil, err
		}
		return collectDiff(ctx, root, []string{opts.Base}, false, known)
	default:
		if repoErr != nil {
			return nil, repoErr
		}
		return collectDiff(ctx, root, []string{"--cached"}, true, known)
	}
}

// collectDiff lints a diff: the staged changes when fromIndex, otherwise the
// working tree against a base ref.
func collectDiff(ctx context.Context, root string, diffArgs []string, fromIndex bool, known map[string]bool) (*Set, error) {
	paths, err := changedPaths(ctx, root, diffArgs)
	if err != nil {
		return nil, err
	}
	byPath, err := changedLines(ctx, root, diffArgs)
	if err != nil {
		return nil, err
	}
	set := &Set{Root: root}
	for _, path := range paths {
		if reason := skipByName(path); reason != "" {
			set.Skipped = append(set.Skipped, Skip{File: path, Reason: reason})
			continue
		}
		var content []byte
		if fromIndex {
			content, err = git(ctx, root, "show", ":"+path)
		} else {
			content, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		}
		if err != nil {
			return nil, err
		}
		// A nil map would mean every line is reportable, which is what the
		// path modes want; here a file the diff gave no hunks for, such as a
		// pure rename, has nothing to report on at all.
		lines := byPath[path]
		if lines == nil {
			lines = map[int]bool{}
		}
		if err := set.add(path, content, lines, known); err != nil {
			return nil, err
		}
	}
	return set, nil
}

func collectPaths(ctx context.Context, dir, root string, paths []string, known map[string]bool) (*Set, error) {
	base := root
	if base == "" {
		base = dir
	}
	set := &Set{Root: base}

	var candidates []string
	var skipped []Skip
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, p)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			rel, err := relative(base, abs)
			if err != nil {
				return nil, err
			}
			candidates = append(candidates, rel)
			continue
		}
		err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDirs[d.Name()] {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			rel, err := relative(base, path)
			if err != nil {
				return err
			}
			candidates = append(candidates, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(candidates)

	if root != "" {
		skippedByGit, err := ignored(ctx, root, candidates)
		if err != nil {
			return nil, err
		}
		kept := candidates[:0]
		for _, c := range candidates {
			if skippedByGit[c] {
				skipped = append(skipped, Skip{File: c, Reason: "ignored by git"})
				continue
			}
			kept = append(kept, c)
		}
		candidates = kept
	}

	set.Skipped = skipped
	for _, rel := range candidates {
		if reason := skipByName(rel); reason != "" {
			set.Skipped = append(set.Skipped, Skip{File: rel, Reason: reason})
			continue
		}
		content, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if err := set.add(rel, content, nil, known); err != nil {
			return nil, err
		}
	}
	return set, nil
}

func relative(base, path string) (string, error) {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// add records a file unless its contents rule it out.
func (s *Set) add(path string, content []byte, reportable map[int]bool, known map[string]bool) error {
	if reason := skipByContent(content); reason != "" {
		s.Skipped = append(s.Skipped, Skip{File: path, Reason: reason})
		return nil
	}
	if reportable != nil && len(reportable) == 0 {
		return nil
	}
	file, err := NewFile(path, content, reportable, known)
	if err != nil {
		return err
	}
	s.Files = append(s.Files, file)
	return nil
}

// NewFile prepares one file's contents for the model: the directives are cut
// out and recorded, and reportable, when it is not nil, limits findings to the
// lines a diff touched. known are the tenet ids a directive is allowed to
// name.
func NewFile(path string, content []byte, reportable map[int]bool, known map[string]bool) (*File, error) {
	return newFile(path, splitLines(content), reportable, known)
}

func newFile(path string, lines []string, reportable map[int]bool, known map[string]bool) (*File, error) {
	lines, sup, err := stripDirectives(path, lines, known)
	if err != nil {
		return nil, err
	}
	return &File{Path: path, Lines: lines, reportable: reportable, Sup: sup}, nil
}

func skipByName(path string) string {
	name := filepath.Base(filepath.FromSlash(path))
	for _, dir := range strings.Split(path, "/") {
		if skipDirs[dir] {
			return "in " + dir
		}
	}
	if skipFiles[name] {
		return "tenet's own file"
	}
	for _, pattern := range secretPatterns {
		if ok, _ := filepath.Match(pattern, name); ok {
			return "may hold a secret"
		}
	}
	return ""
}

func skipByContent(content []byte) string {
	if len(content) > MaxFileBytes {
		return "larger than 1MB"
	}
	head := content
	if len(head) > sniffBytes {
		head = head[:sniffBytes]
	}
	for _, b := range head {
		if b == 0 {
			return "binary"
		}
	}
	return ""
}

func splitLines(content []byte) []string {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
