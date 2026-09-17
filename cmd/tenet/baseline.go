package main

import (
	"fmt"
	"path/filepath"
	"sort"
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
	scope, err := scopeOf(run, lint, paths)
	if err != nil {
		return fail(err)
	}
	entries := baseline.Entries(run.outcome.Findings)
	var dropped int
	if o.prune {
		accepted, err := baseline.Load(path)
		if err != nil {
			return fail(err)
		}
		if accepted.Scope.Mode == "" {
			return fail(fmt.Errorf("%s records no scope, so there is no telling what a prune would drop; write it again with tenetlint baseline", relativeTo(run.dir, path)))
		}
		if !scope.Covers(accepted.Scope) {
			return fail(fmt.Errorf("%s was written over %s and this run covers %s, which would drop what it never looked at; prune over the same scope or a wider one",
				relativeTo(run.dir, path), accepted.Scope, scope))
		}
		entries, dropped = accepted.Prune(entries)
	}
	if err := baseline.Save(path, scope, entries, time.Now()); err != nil {
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

// scopeOf is what this run looked at, in paths the baseline can be read
// against from anywhere in the repository.
func scopeOf(run *run, lint *lintOptions, paths []string) (baseline.Scope, error) {
	switch {
	case len(paths) > 0:
		rel := make([]string, 0, len(paths))
		for _, p := range paths {
			abs := p
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(run.dir, p)
			}
			r, err := filepath.Rel(run.set.Root, abs)
			if err != nil {
				return baseline.Scope{}, err
			}
			rel = append(rel, filepath.ToSlash(r))
		}
		sort.Strings(rel)
		return baseline.Scope{Mode: baseline.ModePaths, Paths: rel}, nil
	case lint.base != "":
		return baseline.Scope{Mode: baseline.ModeBase, Base: lint.base}, nil
	default:
		return baseline.Scope{Mode: baseline.ModeStaged}, nil
	}
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
