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

// hookScript names the binary by its absolute path, because a hook runs with
// whatever PATH the committing program happens to have, which for a GUI git
// client is rarely the shell's.
func hookScript(binary string) string {
	return "#!/bin/sh\n" +
		marker + "\n" +
		"[ -n \"$" + SkipEnv + "\" ] && exit 0\n" +
		"exec " + shellQuote(binary) + "\n"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Manage the pre-commit hook",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newHookInstallCmd(), newHookUninstallCmd())
	return cmd
}

func newHookInstallCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write a pre-commit hook that runs tenetlint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := hookPath(cmd.Context())
			if err != nil {
				return fail(err)
			}
			existing, err := os.ReadFile(path)
			switch {
			case err == nil && !strings.Contains(string(existing), marker) && !force:
				return fail(fmt.Errorf("%s already exists and was not written by tenetlint; pass --force to replace it", path))
			case err != nil && !os.IsNotExist(err):
				return fail(err)
			}
			binary, err := os.Executable()
			if err != nil {
				return fail(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				return fail(err)
			}
			if err := os.WriteFile(path, []byte(hookScript(binary)), 0o700); err != nil {
				return fail(err)
			}
			verb := "installed"
			if existing != nil {
				verb = "replaced"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s the pre-commit hook at %s\n", verb, path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace a pre-commit hook tenetlint did not write")
	return cmd
}

func newHookUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the tenetlint pre-commit hook",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := hookPath(cmd.Context())
			if err != nil {
				return fail(err)
			}
			out := cmd.OutOrStdout()
			existing, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				_, _ = fmt.Fprintf(out, "no pre-commit hook at %s\n", path)
				return nil
			}
			if err != nil {
				return fail(err)
			}
			if !strings.Contains(string(existing), marker) {
				_, _ = fmt.Fprintf(out, "left %s alone, tenetlint did not write it\n", path)
				return nil
			}
			if err := os.Remove(path); err != nil {
				return fail(err)
			}
			_, _ = fmt.Fprintf(out, "removed the pre-commit hook at %s\n", path)
			return nil
		},
	}
}

// hookPath asks git where hooks live, which honours core.hooksPath.
func hookPath(ctx context.Context) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return filepath.Join(strings.TrimSpace(string(out)), "pre-commit"), nil
}
