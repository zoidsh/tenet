package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Marker is how an uninstall tells our hook from one somebody else wrote.
const marker = "# tenetlint hook"

// SkipEnv lets someone commit without a key, or without the lint, at all.
const SkipEnv = "TENETLINT_SKIP"

// hooks are the git hooks tenetlint installs. args are shell words, not
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

// plan is one hook about to be written, with whatever is at its path now.
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
		Short: "Write the pre-commit and commit-msg hooks that run tenetlint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := hooksDir(cmd.Context())
			if err != nil {
				return fail(err)
			}
			binary, err := os.Executable()
			if err != nil {
				return fail(err)
			}
			// Every hook is read before any is written, so that a refusal over
			// one of them does not leave half of a pair installed.
			var plans []plan
			for _, h := range hooks {
				p := plan{name: h.name, path: filepath.Join(dir, h.name), script: hookScript(binary, h.args)}
				existing, err := os.ReadFile(p.path)
				switch {
				case err == nil && !strings.Contains(string(existing), marker) && !force:
					return fail(fmt.Errorf("%s already exists and was not written by tenetlint; pass --force to replace it", p.path))
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
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s the %s hook at %s\n", verb, p.name, p.path)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace a hook tenetlint did not write")
	return cmd
}

func newHookUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the tenetlint pre-commit and commit-msg hooks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := hooksDir(cmd.Context())
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
					_, _ = fmt.Fprintf(out, "left %s alone, tenetlint did not write it\n", path)
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

// hooksDir asks git where hooks live, which honours core.hooksPath.
func hooksDir(ctx context.Context) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}
