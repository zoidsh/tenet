// Package auth finds the API key a run judges with, and saves the one a
// person typed so that they need not type it again.
package auth

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/zoidsh/tenetlint/internal/provider"
)

// Sources a key can come from. Resolve tries the last three in this order,
// and SourceFlag is for the key a command was handed outright.
const (
	SourceFlag    = "flag"
	SourceEnv     = "env"
	SourceProject = "project"
	SourceGlobal  = "global"
)

// Scopes a saved key can live in.
const (
	ScopeProject = "project"
	ScopeGlobal  = "global"
)

// Where a saved key lives: Dir/FileName inside a repository, and
// tenetlint/FileName under the config home.
const (
	Dir      = ".tenetlint"
	FileName = "credentials"
	appDir   = "tenetlint"

	configHomeEnv = "XDG_CONFIG_HOME"
)

// MinKeyLength is what every key tenet has seen clears, and enough to tell a
// key from a path or a shell variable somebody pasted by mistake.
const MinKeyLength = 20

// ErrNoRepository is what a project scope has nothing to write to.
var ErrNoRepository = errors.New("the project scope keeps the key in a repository, and this directory is not in one")

// Resolve is the key a run uses for the provider, and where it came from.
// Both are empty when there is no key to be had.
func Resolve(p provider.Provider) (key, source string, err error) {
	if key := strings.TrimSpace(os.Getenv(p.Env)); key != "" {
		return key, SourceEnv, nil
	}
	project, _, err := projectFile()
	if err != nil {
		return "", "", err
	}
	if project != "" {
		key, err = KeyIn(project, p.Name)
		if err != nil {
			return "", "", err
		}
		if key != "" {
			return key, SourceProject, nil
		}
	}
	global, err := GlobalPath()
	if err != nil {
		return "", "", err
	}
	key, err = KeyIn(global, p.Name)
	if err != nil {
		return "", "", err
	}
	if key != "" {
		return key, SourceGlobal, nil
	}
	return "", "", nil
}

// Saved is what a save did, so that the command can say where the key went
// and what else it had to change.
type Saved struct {
	Path string

	// Gitignore is the file an entry was appended to so that the key is not
	// committed, empty when git ignored the credentials already.
	Gitignore string
}

// Save writes the key for one provider, globally or in the repository.
func Save(p provider.Provider, key string, global bool) (Saved, error) {
	if err := p.Available(); err != nil {
		return Saved{}, err
	}
	if err := Validate(key); err != nil {
		return Saved{}, err
	}
	if global {
		path, err := GlobalPath()
		if err != nil {
			return Saved{}, err
		}
		return Saved{Path: path}, writeKey(path, p.Name, key)
	}
	path, root, err := projectFile()
	if err != nil {
		return Saved{}, err
	}
	if root == "" {
		return Saved{}, ErrNoRepository
	}
	if err := writeKey(path, p.Name, key); err != nil {
		return Saved{}, err
	}
	gitignore, err := ignore(root, path)
	if err != nil {
		return Saved{}, err
	}
	return Saved{Path: path, Gitignore: gitignore}, nil
}

// Remove deletes one provider's key from one scope, and reports whether there
// was a key there to delete. A file left holding nothing is deleted with it.
func Remove(p provider.Provider, scope string) (path string, removed bool, err error) {
	path, err = ScopePath(scope)
	if err != nil {
		return "", false, err
	}
	entries, err := readEntries(path)
	if err != nil {
		return path, false, err
	}
	if _, ok := entries[p.Name]; !ok {
		return path, false, nil
	}
	delete(entries, p.Name)
	if len(entries) == 0 {
		return path, true, os.Remove(path)
	}
	return path, true, writeEntries(path, entries)
}

// Validate rejects what only a paid call would otherwise reject.
func Validate(key string) error {
	switch {
	case key == "":
		return errors.New("that is an empty key")
	case strings.ContainsFunc(key, unicode.IsSpace):
		return errors.New("an API key has no spaces in it")
	case len(key) < MinKeyLength:
		return fmt.Errorf("an API key is at least %d characters, and that one is %d", MinKeyLength, len(key))
	}
	return nil
}

// ScopePath is the credentials file a scope writes to.
func ScopePath(scope string) (string, error) {
	switch scope {
	case ScopeGlobal:
		return GlobalPath()
	case ScopeProject:
		path, root, err := projectFile()
		if err != nil {
			return "", err
		}
		if root == "" {
			return "", ErrNoRepository
		}
		return path, nil
	default:
		return "", fmt.Errorf("unknown scope %q", scope)
	}
}

// GlobalPath is the credentials file outside any repository. It is ~/.config
// on every platform rather than os.UserConfigDir's answer, so that the one
// path the documentation names is the path a Mac uses too.
func GlobalPath() (string, error) {
	base := os.Getenv(configHomeEnv)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, appDir, FileName), nil
}

// ProjectPath is the credentials file this directory's repository reads,
// empty outside a repository.
func ProjectPath() (string, error) {
	path, _, err := projectFile()
	return path, err
}

// KeyIn is one provider's key in one credentials file, empty when the file has
// no line for it and when there is no such file. The path is one a save could
// write, never the empty string a directory outside a repository has instead
// of a project file.
func KeyIn(path, name string) (string, error) {
	entries, err := readEntries(path)
	if err != nil {
		return "", err
	}
	return entries[name], nil
}

// projectFile is the credentials file a run reads: the nearest one at or above
// the working directory, and otherwise the one a save would write in the
// repository root. Both are empty outside a repository.
func projectFile() (path, root string, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	root = repoRoot(dir)
	if root == "" {
		return "", "", nil
	}
	for d := dir; ; d = filepath.Dir(d) {
		candidate := filepath.Join(d, Dir, FileName)
		if exists(candidate) {
			return candidate, root, nil
		}
		if d == root || filepath.Dir(d) == d {
			break
		}
	}
	return filepath.Join(root, Dir, FileName), root, nil
}

// repoRoot is the top of the working tree dir is in, empty when it is in
// none. A worktree and a submodule carry .git as a file rather than a
// directory, so what is looked for is the name.
func repoRoot(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if exists(filepath.Join(d, ".git")) {
			return d
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func readEntries(path string) (map[string]string, error) {
	entries := map[string]string{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) { // tenet:ignore no-fallback a file that is not there is what "no key has been saved" looks like, which is the answer every caller wants rather than a failure any of them could act on
		return entries, nil // tenet:ignore no-fallback the same absence as the line above
	}
	if err != nil {
		return nil, err
	}
	for n, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, key, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: a line holds a provider and its key, as provider=key", path, n+1)
		}
		entries[strings.TrimSpace(name)] = strings.TrimSpace(key)
	}
	return entries, nil
}

func writeKey(path, name, key string) error {
	entries, err := readEntries(path)
	if err != nil {
		return err
	}
	entries[name] = key
	return writeEntries(path, entries)
}

// writeEntries rewrites the whole file, keeping every provider it does not
// know about: a newer tenet may have written one, and losing that key would
// be a worse trade than carrying a line nothing here reads. Comments do not
// survive a save.
func writeEntries(path string, entries map[string]string) error {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s=%s\n", name, entries[name])
	}
	return writeFile(path, b.String())
}

// writeFile replaces the file in one step, so that an interrupted save leaves
// the key that was there rather than half of a new one.
func writeFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, FileName+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	// The mode is set rather than left to the temp file's own, because the
	// umask reaches the one and not the other.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ignore keeps the credentials out of a commit, and says nothing when git
// ignores them already, which is what makes a second save change no file.
func ignore(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	ignored, err := checkIgnore(root, rel)
	if err != nil || ignored {
		return "", err
	}
	gitignore := filepath.Join(root, ".gitignore")
	if err := appendLine(gitignore, rel); err != nil {
		return "", err
	}
	return gitignore, nil
}

// checkIgnore asks git rather than reading .gitignore, because what ignores a
// path may be a pattern in any of the files git consults, including the
// user's own global one. --no-index answers about the pattern rules alone,
// which is the question, whether or not the file is tracked.
func checkIgnore(root, rel string) (bool, error) {
	err := exec.Command("git", "-C", root, "check-ignore", "-q", "--no-index", rel).Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git check-ignore %s: %w", rel, err)
}

func appendLine(path, line string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var b strings.Builder
	b.Write(data)
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		b.WriteString("\n")
	}
	b.WriteString(line + "\n")
	// A .gitignore this created is read by everyone working in the
	// repository, unlike the credentials beside it.
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
