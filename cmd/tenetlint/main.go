// Command tenetlint lints code against English rules.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenetlint/internal/buildinfo"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tenetlint",
		Short:         "Lint code against the rules you wrote in English",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.AddCommand(newVersionCmd())
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

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "tenetlint:", err)
		os.Exit(1)
	}
}
