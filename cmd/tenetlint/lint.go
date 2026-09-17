package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenetlint/internal/cache"
	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/judge"
	"github.com/zoidsh/tenetlint/internal/report"
	"github.com/zoidsh/tenetlint/internal/source"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

// exitError carries the code the process should end with, so that a finding
// and a broken run are told apart by the caller of tenetlint, usually a hook.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

func fail(err error) error { return &exitError{code: report.ExitError, err: err} }

// missingKeyError is shared with init, so that whichever command a newcomer
// runs first names the same key and the same way out.
func missingKeyError() error {
	return fmt.Errorf("%s is not set: export your TypeSafe API key to lint, or set %s=1 to commit without linting", jev.APIKeyEnv, SkipEnv)
}

type lintOptions struct {
	base    string
	config  string
	format  string
	model   string
	noCache bool
	verbose bool
	quiet   bool
}

func addLintFlags(cmd *cobra.Command, o *lintOptions) {
	f := cmd.Flags()
	f.StringVar(&o.base, "base", "", "lint the working tree against this git ref instead of the staged changes")
	f.StringVar(&o.config, "config", "", "path to tenets.yml, searched for by default")
	f.StringVar(&o.format, "format", "", "output format: text or json (default text on a terminal, json otherwise)")
	f.StringVar(&o.model, "model", "", "jev model to ask, overriding the one in tenets.yml")
	f.BoolVar(&o.noCache, "no-cache", false, "ask the model again instead of reusing cached answers")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "report skipped files and what each window costs on stderr")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "print the findings without the summary line")
}

func (o *lintOptions) validate(out io.Writer) error {
	if o.format == "" {
		o.format = report.DefaultFormat(out)
	}
	if o.format != report.FormatText && o.format != report.FormatJSON {
		return fmt.Errorf("--format must be %s or %s, got %q", report.FormatText, report.FormatJSON, o.format)
	}
	return nil
}

func runLint(cmd *cobra.Command, paths []string, o *lintOptions) error {
	ctx := cmd.Context()
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := o.validate(out); err != nil {
		return fail(err)
	}

	dir, err := os.Getwd()
	if err != nil {
		return fail(err)
	}

	cfg, err := openConfig(o.config)
	if err != nil {
		return fail(err)
	}

	model := o.model
	if model == "" {
		model = cfg.Model
	}
	if model == "" {
		model = jev.DefaultModel
	}
	cfg.SetModel(model)

	key := jev.KeyFromEnv()
	if key == "" {
		return fail(missingKeyError())
	}

	ids := make([]string, 0, len(cfg.Tenets))
	for _, t := range cfg.Tenets {
		ids = append(ids, t.ID)
	}
	set, err := source.Collect(ctx, source.Options{Dir: dir, Base: o.base, Paths: paths, Tenets: ids})
	if err != nil {
		return fail(err)
	}
	r := report.Report{
		Stats:   judge.Stats{Files: len(set.Files)},
		Skipped: set.Skipped,
		Quiet:   o.quiet,
	}
	var near []judge.NearMiss
	windows := set.Windows()
	if len(windows) > 0 {
		outcome, err := lintWindows(ctx, cmd, o, cfg, windows, key, model)
		if err != nil {
			return fail(err)
		}
		outcome.Stats.Files = len(set.Files)
		r.Findings, r.Stats, near = outcome.Findings, outcome.Stats, outcome.NearMisses
	}

	relocate(&r, set.Root, dir)
	if o.verbose {
		for _, s := range r.Skipped {
			_, _ = fmt.Fprintf(errOut, "skipped %s: %s\n", s.File, s.Reason)
		}
		for _, n := range near {
			_, _ = fmt.Fprintf(errOut, "near miss %s:%d: %s (p=%.2f, fails at %.2f)\n",
				displayPath(set.Root, dir, n.File), n.Line, n.Tenet, n.Probability, n.Fail)
		}
	}
	if err := r.Write(out, o.format, report.ColorEnabled(out)); err != nil {
		return fail(err)
	}
	if code := r.ExitCode(); code != report.ExitOK {
		return &exitError{code: code}
	}
	return nil
}

// relocate rewrites the repository-relative paths the tenets are matched
// against into paths relative to where the lint was started, which is what a
// terminal needs to open the file it printed.
func relocate(r *report.Report, root, dir string) {
	for i := range r.Findings {
		r.Findings[i].File = displayPath(root, dir, r.Findings[i].File)
	}
	for i := range r.Skipped {
		r.Skipped[i].File = displayPath(root, dir, r.Skipped[i].File)
	}
}

func displayPath(root, dir, path string) string {
	abs := filepath.Join(root, filepath.FromSlash(path))
	rel, err := filepath.Rel(dir, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

func lintWindows(ctx context.Context, cmd *cobra.Command, o *lintOptions, cfg *tenets.Config, windows []*source.Window, key, model string) (judge.Outcome, error) {
	j := &judge.Judge{Asker: jev.New(key, jev.WithModel(model)), Tenets: cfg.Tenets}
	if !o.noCache {
		c, err := cache.Open("")
		if err != nil {
			return judge.Outcome{}, err
		}
		j.Cache = c
	}
	if o.verbose {
		errOut := cmd.ErrOrStderr()
		// Windows are judged concurrently, so the log lines need a lock of
		// their own to arrive whole.
		var mu sync.Mutex
		j.Log = func(line string) {
			mu.Lock()
			defer mu.Unlock()
			_, _ = fmt.Fprintln(errOut, line)
		}
	}

	// The progress line is the only sign of life during a run that can take
	// tens of seconds, and it is taken back before anything is printed. A
	// verbose run has its own lines to write to stderr, which would be
	// printed over it and leave half of it behind.
	progress := report.NewProgress(cmd.ErrOrStderr(), !o.quiet && !o.verbose && o.format == report.FormatText)
	if progress.On() {
		progress.Start(len(windows), j.Cached(windows))
	}
	defer progress.Clear()
	return j.Run(ctx, windows)
}
