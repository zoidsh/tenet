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
	prune  bool
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
	f := cmd.Flags()
	f.StringVar(&o.output, "output", "", "write the baseline here instead of "+baseline.Name+" in the repository root")
	f.BoolVar(&o.prune, "prune", false, "keep only the entries this run still finds, instead of writing the run out whole")
	return cmd
}

func runBaseline(cmd *cobra.Command, paths []string, lint *lintOptions, o *baselineOptions) error {
	run, err := judgeRun(cmd, paths, lint)
	if err != nil {
		return fail(err)
	}
	path := o.baselinePath(run)
	entries := baseline.Entries(run.outcome.Findings)
	var dropped int
	if o.prune {
		accepted, err := baseline.Load(path)
		if err != nil {
			return fail(err)
		}
		entries, dropped = accepted.Prune(entries)
	}
	if err := baseline.Save(path, entries, time.Now()); err != nil {
		return fail(err)
	}
	out, name := cmd.OutOrStdout(), relativeTo(run.dir, path)
	if o.prune {
		_, err = fmt.Fprintf(out, "dropped %s, %s left in %s\n", countFindings(dropped), countFindings(len(entries)), name)
		return err
	}
	_, err = fmt.Fprintf(out, "wrote %s to %s\n", countFindings(len(entries)), name)
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
