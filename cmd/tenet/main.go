// Command tenetlint lints code against English rules.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenetlint/internal/buildinfo"
	"github.com/zoidsh/tenetlint/internal/report"
)

func newRootCmd() *cobra.Command {
	opts := &lintOptions{}
	root := &cobra.Command{
		Use:   "tenetlint [paths...]",
		Short: "Lint code against the rules you wrote in English",
		// Without this, cobra reads the first path as the name of a subcommand
		// it does not have.
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLint(cmd, args, opts)
		},
	}
	addLintFlags(root, opts)
	root.AddCommand(newVersionCmd(), newInitCmd(), newHookCmd(), newCheckCmd(), newRulesCmd(), newPresetsCmd(), newConfigCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the tenetlint version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), buildinfo.Version())
			return err
		},
	}
}

func execute(root *cobra.Command) int {
	err := root.Execute()
	if err == nil {
		return report.ExitOK
	}
	var exit *exitError
	if errors.As(err, &exit) {
		if exit.err != nil {
			_, _ = fmt.Fprintln(root.ErrOrStderr(), "tenetlint:", exit.err)
		}
		return exit.code
	}
	_, _ = fmt.Fprintln(root.ErrOrStderr(), "tenetlint:", err)
	return report.ExitError
}

func main() {
	os.Exit(execute(newRootCmd()))
}
