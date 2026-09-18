// Package receipt remembers that one staged tree was judged clean, so that a
// commit is not paid for twice: an agent's commit is linted by the plugin's
// PreToolUse hook and then again by the repository's pre-commit hook, and a
// person who runs tenet and then commits pays the same way.
//
// tenet:ignore-file no-fallback a missing or unreadable receipt is the contract
// rather than a degraded read: it only means the run judges what it would have
// judged anyway, and nothing a receipt says can fail a commit.
package receipt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zoidsh/tenet/internal/buildinfo"
)

// gitPath is asked of git rather than joined onto the git dir, so that a linked
// worktree, whose index and staged tree are its own, gets a receipt of its own.
const gitPath = "tenet-receipt"

// Example is one tenet's examples_from file, named relative to the config.
type Example struct {
	Name string
	Data []byte
}

// Inputs are everything that decides what a staged run finds. The baseline is
// carried as bytes and presence alone, because a baseline read from elsewhere
// and one read from beside the config accept exactly the same findings, and
// --no-baseline is a run with no baseline at all.
type Inputs struct {
	Head     string
	Tree     string
	Config   []byte
	Examples []Example
	Model    string

	Baseline    []byte
	HasBaseline bool
}

// Key identifies the inputs. The version of the binary is part of it because
// the built-in rules ship inside the binary, so an upgrade can change the
// verdict on a tree no file of the repository's touched.
func Key(in Inputs) string {
	h := sha256.New()
	field(h, "version", []byte(version()))
	field(h, "head", []byte(in.Head))
	field(h, "tree", []byte(in.Tree))
	field(h, "model", []byte(in.Model))
	field(h, "config", in.Config)
	for _, e := range in.Examples {
		field(h, "examples:"+e.Name, e.Data)
	}
	if in.HasBaseline {
		field(h, "baseline", in.Baseline)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// version tells one build of tenet from another. Every unreleased build calls
// itself dev, and a developer editing a built-in rule rebuilds within the one
// version, so the binary's own size and time stand in for the number it does
// not have.
func version() string {
	v := buildinfo.Version()
	if v != buildinfo.Dev {
		return v
	}
	path, err := os.Executable()
	if err != nil {
		return v
	}
	info, err := os.Stat(path)
	if err != nil {
		return v
	}
	return fmt.Sprintf("%s %d %d", v, info.Size(), info.ModTime().UnixNano())
}

// field writes one input, length-prefixed, so that no two sets of inputs can
// hash the same by running into each other.
func field(h hash.Hash, name string, data []byte) {
	_, _ = fmt.Fprintf(h, "%s %d\n", name, len(data))
	_, _ = h.Write(data)
	_, _ = h.Write([]byte{'\n'})
}

// Path is where this repository, or this linked worktree, keeps its receipt.
func Path(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-path", gitPath).Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

// Head is the commit the staged changes are a diff against, empty on a branch
// with no commit on it yet, which is a staged run like any other.
func Head(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--verify", "--quiet", "HEAD").Output()
	if err != nil {
		// --quiet says nothing and exits 1 when there is no HEAD to name, which
		// is the branch before its first commit rather than anything wrong.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(out) == 0 {
			return "", nil
		}
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Tree is the hash of the index. It says what the staged run reads, since every
// file's content comes out of the index, and with Head it says which lines of
// that content the run reports on, since the staged changes are a diff of the
// two. An index that cannot be written as a tree, an unmerged one above all,
// has no receipt.
func Tree(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "write-tree").Output()
	if err != nil {
		return "", fmt.Errorf("git write-tree: %w", err)
	}
	tree := strings.TrimSpace(string(out))
	if tree == "" {
		return "", fmt.Errorf("git write-tree wrote no tree")
	}
	return tree, nil
}

// Load reads the key a receipt holds, empty when there is none to read.
func Load(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Save records the key. The write goes to a temporary file first, so that a
// run killed mid-write, or two runs writing at once, cannot leave a receipt
// behind that is neither key.
func Save(path, key string) error {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	remove := func(err error) error {
		_ = os.Remove(temp.Name())
		return err
	}
	if _, err := temp.WriteString(key + "\n"); err != nil {
		_ = temp.Close()
		return remove(err)
	}
	if err := temp.Close(); err != nil {
		return remove(err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return remove(err)
	}
	return nil
}
