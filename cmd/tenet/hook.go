package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenet/internal/source"
)

// Marker is how an uninstall tells our hook from one somebody else wrote. It
// keeps the old name of the command, because changing it would orphan every
// hook already on a machine.
const marker = "# tenet hook"

// SkipEnv lets someone commit without a key, or without the lint, at all.
const SkipEnv = "TENET_SKIP"

// hooks are the git hooks tenet installs. args are shell words, not
// arguments to quote: "$1" is the message file git passes a commit-msg hook,
// and quoting it here would make it a literal.
var hooks = []struct {
	name string
	args string
}{
	{name: "pre-commit"},
	{name: "commit-msg", args: ` --commit-msg "$1"`},
}

// hookScript names the binary by its absolute path, because a hook runs with
// whatever PATH the committing program happens to have, which for a GUI git
// client is rarely the shell's.
func hookScript(binary, args string) string {
	return "#!/bin/sh\n" +
		marker + "\n" +
		"[ -n \"$" + SkipEnv + "\" ] && exit 0\n" +
		"exec " + shellQuote(binary) + args + "\n"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Manage the pre-commit and commit-msg hooks",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newHookInstallCmd(), newHookUninstallCmd())
	return cmd
}

type plan struct {
	name     string
	path     string
	script   string
	existing []byte
}

func newHookInstallCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the pre-commit and commit-msg hooks that run tenet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, root, err := hookPaths(cmd.Context())
			if err != nil {
				return fail(err)
			}
			binary, err := os.Executable()
			if err != nil {
				return fail(err)
			}
			if !force {
				reason, err := managedHooks(cmd.Context(), dir, root)
				if err != nil {
					return fail(err)
				}
				if reason != "" {
					return fail(fmt.Errorf(`this repository already manages its git hooks (%s); add tenet to that setup instead, as a pre-commit command "tenet" and a commit-msg command "tenet --commit-msg <file>", or pass --force to write tenet's own hooks`, reason))
				}
			}
			// Every hook is read before any is written, so that a refusal over
			// one of them does not leave half of a pair installed.
			var plans []plan
			for _, h := range hooks {
				p := plan{name: h.name, path: filepath.Join(dir, h.name), script: hookScript(binary, h.args)}
				existing, err := os.ReadFile(p.path)
				switch {
				case err == nil && !strings.Contains(string(existing), marker) && !force:
					return fail(fmt.Errorf("%s already exists and was not written by tenet; pass --force to replace it", p.path))
				case err != nil && !os.IsNotExist(err):
					return fail(err)
				}
				p.existing = existing
				plans = append(plans, p)
			}
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return fail(err)
			}
			for _, p := range plans {
				if err := os.WriteFile(p.path, []byte(p.script), 0o700); err != nil {
					return fail(err)
				}
				verb := "installed"
				if p.existing != nil {
					verb = "replaced"
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", verb, inRepo(root, p.path))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace a hook tenet did not write")
	return cmd
}

func newHookUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the tenet pre-commit and commit-msg hooks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, _, err := hookPaths(cmd.Context())
			if err != nil {
				return fail(err)
			}
			out := cmd.OutOrStdout()
			for _, h := range hooks {
				path := filepath.Join(dir, h.name)
				existing, err := os.ReadFile(path)
				if os.IsNotExist(err) {
					_, _ = fmt.Fprintf(out, "no %s hook at %s\n", h.name, path)
					continue
				}
				if err != nil {
					return fail(err)
				}
				if !strings.Contains(string(existing), marker) {
					_, _ = fmt.Fprintf(out, "left %s alone, tenet did not write it\n", path)
					continue
				}
				if err := os.Remove(path); err != nil {
					return fail(err)
				}
				_, _ = fmt.Fprintf(out, "removed the %s hook at %s\n", h.name, path)
			}
			return nil
		},
	}
}

// managedHooks says, in a few words for the refusal, why this repository
// looks like it already runs its git hooks through something else, and is
// empty when it does not. Hook managers differ too much to name, so the only
// signals are the ones every one of them leaves: the hooks directory moved,
// or a hook in it that tenet did not write. Go reports no executable bit on
// Windows, where core.hooksPath is therefore the only signal left.
func managedHooks(ctx context.Context, hooks, root string) (string, error) {
	if path, err := exec.CommandContext(ctx, "git", "-C", root, "config", "--get", "core.hooksPath").Output(); err == nil && strings.TrimSpace(string(path)) != "" {
		return "core.hooksPath is set", nil
	}
	entries, err := os.ReadDir(hooks)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".sample") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		if info.Mode()&0o111 == 0 {
			continue
		}
		path := filepath.Join(hooks, entry.Name())
		script, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if !strings.Contains(string(script), marker) {
			return inRepo(root, path) + " exists", nil
		}
	}
	return "", nil
}

// inRepo names a hook the way the repository does. A hooks directory outside
// the repository, which core.hooksPath allows, keeps its absolute path: a
// trail of ".." is no easier to read than the path itself.
func inRepo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}

// hookPaths is where the hooks live and the repository their paths are
// printed relative to, both read from the directory tenet was run in.
func hookPaths(ctx context.Context) (hooks, root string, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	if hooks, err = hooksDir(ctx, dir); err != nil {
		return "", "", err
	}
	root, err = source.RepoRoot(ctx, dir)
	return hooks, root, err
}

// hooksDir asks git where hooks live, which honours core.hooksPath.
func hooksDir(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}
