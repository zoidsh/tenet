package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ErrNotARepository is returned when a mode that needs git is asked for
// outside a working tree.
var ErrNotARepository = errors.New("not inside a git repository")

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// RepoRoot is the top of the working tree that contains dir.
func RepoRoot(ctx context.Context, dir string) (string, error) {
	out, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		// The sentinel is what the modes switch on, but git's own words stay:
		// rev-parse also fails when git is missing or the repository is
		// broken, and "not a repository" would be a lie about those.
		return "", fmt.Errorf("%w: %w", ErrNotARepository, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func splitZ(out []byte) []string {
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

func changedPaths(ctx context.Context, root string, diffArgs []string) ([]string, error) {
	args := append([]string{"diff", "--name-only", "--diff-filter=ACMR", "-M", "-z"}, diffArgs...)
	out, err := git(ctx, root, args...)
	if err != nil {
		return nil, err
	}
	return splitZ(out), nil
}

var hunkPattern = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// changedLines are the lines a diff added or rewrote, per file, which are the
// only lines a finding may be reported on in a diff mode. The whole diff is
// read in one go and attributed by its file headers, because naming a path on
// the command line turns rename detection off and makes a renamed file look
// entirely new.
func changedLines(ctx context.Context, root string, diffArgs []string) (map[string]map[int]bool, error) {
	args := append([]string{
		"-c", "core.quotePath=false",
		"diff", "-U0", "-M", "--src-prefix=a/", "--dst-prefix=b/",
	}, diffArgs...)
	out, err := git(ctx, root, args...)
	if err != nil {
		return nil, err
	}

	byPath := map[string]map[int]bool{}
	var current map[int]bool
	for _, line := range strings.Split(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "+++ "); ok {
			path, ok := diffPath(after)
			if !ok {
				current = nil
				continue
			}
			current = map[int]bool{}
			byPath[path] = current
			continue
		}
		m := hunkPattern.FindStringSubmatch(line)
		if m == nil || current == nil {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		count := 1
		if m[2] != "" {
			count, _ = strconv.Atoi(m[2])
		}
		for i := 0; i < count; i++ {
			current[start+i] = true
		}
	}
	return byPath, nil
}

// diffPath reads the file header of a diff, which git quotes when the path
// holds anything awkward.
func diffPath(token string) (string, bool) {
	if strings.HasPrefix(token, `"`) {
		unquoted, err := strconv.Unquote(token)
		if err != nil {
			return "", false
		}
		token = unquoted
	}
	return strings.CutPrefix(token, "b/")
}

// ignored asks git which of these repository-relative paths are ignored. A
// check-ignore that matches nothing exits 1, which is not a failure.
func ignored(ctx context.Context, root string, paths []string) (map[string]bool, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "--stdin", "-z")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return nil, fmt.Errorf("git check-ignore: %s", strings.TrimSpace(stderr.String()))
	}
	out := map[string]bool{}
	for _, p := range splitZ(stdout.Bytes()) {
		out[p] = true
	}
	return out, nil
}
