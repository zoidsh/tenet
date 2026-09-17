package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenet/internal/baseline"
)

type baselineOptions struct {
	output string
	prune  bool
}

func newBaselineCmd(g *globalOptions) *cobra.Command {
	lint := &lintOptions{g: g}
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
	run, err := collectRun(cmd, paths, lint)
	if err != nil {
		return fail(err)
	}
	path := o.baselinePath(run)
	scope, err := scopeOf(run, lint, paths)
	if err != nil {
		return fail(err)
	}
	// Whether a prune is allowed at all is settled before the model is asked
	// anything, so that a refused one costs nothing.
	var accepted *baseline.File
	if o.prune {
		if accepted, err = o.loadForPrune(run, path, scope); err != nil {
			return fail(err)
		}
	}
	if err := run.judge(cmd, lint); err != nil {
		return fail(err)
	}
	entries := baseline.Entries(run.outcome.Findings)
	var dropped int
	if o.prune {
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

// loadForPrune is the baseline a prune is about to rewrite, refused unless
// this run looked at everything the baseline was written over.
func (o *baselineOptions) loadForPrune(run *run, path string, scope baseline.Scope) (*baseline.File, error) {
	accepted, err := baseline.Load(path)
	if err != nil {
		return nil, err
	}
	if accepted.Scope.Mode == "" {
		return nil, fmt.Errorf("%s records no scope, so there is no telling what a prune would drop; write it again with tenet baseline", relativeTo(run.dir, path))
	}
	if !scope.Covers(accepted.Scope) {
		return nil, fmt.Errorf("%s was written over %s and this run covers %s, which would drop what it never looked at; prune over the same scope or a wider one",
			relativeTo(run.dir, path), accepted.Scope, scope)
	}
	return accepted, nil
}

// scopeOf is what this run looked at, in paths the baseline can be read
// against from anywhere in the repository. The order of the cases is
// source.Collect's own precedence, where paths win over a base ref and the
// staged changes are what is left, so that the scope recorded is the mode that
// actually ran rather than the flags that were typed..
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
