package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenetlint/internal/baseline"
)

type baselineOptions struct {
	output string
}

func newBaselineCmd() *cobra.Command {
	lint := &lintOptions{}
	o := &baselineOptions{}
	cmd := &cobra.Command{
		Use:   "baseline [paths...]",
		Short: "Accept the findings the code has today, so that later runs block only new ones",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBaseline(cmd, args, lint, o)
		},
	}
	addRunFlags(cmd, lint)
	cmd.Flags().StringVar(&o.output, "output", "", "write the baseline here instead of "+baseline.Name+" in the repository root")
	return cmd
}

func runBaseline(cmd *cobra.Command, paths []string, lint *lintOptions, o *baselineOptions) error {
	run, err := judgeRun(cmd, paths, lint)
	if err != nil {
		return fail(err)
	}
	path := o.baselinePath(run)
	entries := baseline.Entries(run.outcome.Findings)
	if err := baseline.Save(path, entries, time.Now()); err != nil {
		return fail(err)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s to %s\n", countFindings(len(entries)), relativeTo(run.dir, path))
	return err
}

func (o *baselineOptions) baselinePath(run *run) string {
	if o.output != "" {
		return o.output
	}
	return filepath.Join(run.set.Root, baseline.Name)
}

func countFindings(n int) string {
	if n == 1 {
		return "1 finding"
	}
	return fmt.Sprintf("%d findings", n)
}
