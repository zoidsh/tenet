package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenetlint/internal/baseline"
	"github.com/zoidsh/tenetlint/internal/cache"
	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/judge"
	"github.com/zoidsh/tenetlint/internal/report"
	"github.com/zoidsh/tenetlint/internal/source"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

// exitError carries the code the process should end with, so that a finding
// and a broken run are told apart by the caller of tenet, usually a hook.
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
	base          string
	commitMsg     string
	config        string
	format        string
	model         string
	noCache       bool
	verbose       bool
	quiet         bool
	baseline      string
	noBaseline    bool
	showBaselined bool
}

func addLintFlags(cmd *cobra.Command, o *lintOptions) {
	addRunFlags(cmd, o)
	f := cmd.Flags()
	f.StringVar(&o.commitMsg, "commit-msg", "", "lint the commit message in this file instead of any code")
	f.StringVar(&o.format, "format", "", "output format: text or json (default text on a terminal, json otherwise)")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "print the findings without the summary line")
	f.StringVar(&o.baseline, "baseline", "", "read this baseline instead of "+baseline.Name+" in the repository root")
	f.BoolVar(&o.noBaseline, "no-baseline", false, "report every finding, whatever the baseline accepts")
	f.BoolVar(&o.showBaselined, "show-baselined", false, "list the findings the baseline accepts as well, marked and still passing")
}

// addRunFlags are the flags that choose what is judged and how, which baseline
// takes too so that it accepts exactly what a lint would have found.
func addRunFlags(cmd *cobra.Command, o *lintOptions) {
	f := cmd.Flags()
	f.StringVar(&o.base, "base", "", "lint the working tree against this git ref instead of the staged changes")
	f.StringVar(&o.config, "config", "", "path to tenets.yml, searched for by default")
	f.StringVar(&o.model, "model", "", "jev model to ask, overriding the one in tenets.yml")
	f.BoolVar(&o.noCache, "no-cache", false, "ask the model again instead of reusing cached answers")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "report skipped files and what each window costs on stderr")
}

func (o *lintOptions) validate(out io.Writer, paths []string) error {
	if o.commitMsg != "" {
		switch {
		case len(paths) > 0:
			return fmt.Errorf("--commit-msg lints the message on its own, so it cannot be given paths as well")
		case o.base != "":
			return fmt.Errorf("--commit-msg lints the message on its own, so it cannot be combined with --base")
		}
	}
	if o.baseline != "" && o.noBaseline {
		return fmt.Errorf("--no-baseline ignores the baseline, so there is no file for --baseline to name")
	}
	if o.format == "" {
		o.format = report.DefaultFormat(out)
	}
	if o.format != report.FormatText && o.format != report.FormatJSON {
		return fmt.Errorf("--format must be %s or %s, got %q", report.FormatText, report.FormatJSON, o.format)
	}
	return nil
}

// run is what one judged pass over the selected code amounts to, before
// anything decides what to print or write about it. The findings' paths are
// still the repository-relative ones the tenets were matched against.
type run struct {
	dir     string
	set     *source.Set
	outcome judge.Outcome
}

// judgeRun collects what the options select and judges it, which is the part
// lint and baseline share.
func judgeRun(cmd *cobra.Command, paths []string, o *lintOptions) (*run, error) {
	ctx := cmd.Context()
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	cfg, err := openConfig(o.config)
	if err != nil {
		return nil, err
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
		return nil, missingKeyError()
	}

	ids := make([]string, 0, len(cfg.Tenets))
	for _, t := range cfg.Tenets {
		ids = append(ids, t.ID)
	}
	// A repository whose tenets say nothing about the commit message must not
	// pay for a call on every commit, so the message is not even read. The set
	// then has no root either: a run that read nothing has no paths to print,
	// and the directory it started in is not where they would be relative to.
	set := &source.Set{}
	if o.commitMsg == "" || applies(cfg, source.CommitMsgPath) {
		set, err = source.Collect(ctx, source.Options{Dir: dir, Base: o.base, Paths: paths, CommitMsg: o.commitMsg, Tenets: ids})
		if err != nil {
			return nil, err
		}
	}
	r := &run{dir: dir, set: set, outcome: judge.Outcome{Stats: judge.Stats{Files: len(set.Files)}}}
	windows := set.Windows()
	if len(windows) == 0 {
		return r, nil
	}
	outcome, err := lintWindows(ctx, cmd, o, cfg, windows, key, model)
	if err != nil {
		return nil, err
	}
	outcome.Stats.Files = len(set.Files)
	r.outcome = outcome
	return r, nil
}

func runLint(cmd *cobra.Command, paths []string, o *lintOptions) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := o.validate(out, paths); err != nil {
		return fail(err)
	}

	run, err := judgeRun(cmd, paths, o)
	if err != nil {
		return fail(err)
	}
	set, dir, near := run.set, run.dir, run.outcome.NearMisses

	accepted, err := o.accepted(set.Root)
	if err != nil {
		return fail(err)
	}
	findings, baselined := accepted.Split(run.outcome.Findings)

	r := report.Report{
		Findings:      findings,
		Baselined:     baselined,
		ShowBaselined: o.showBaselined,
		Stats:         run.outcome.Stats,
		Skipped:       set.Skipped,
		Quiet:         o.quiet,
	}
	if o.commitMsg != "" {
		r.Next = report.NextCommitMsg
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

// accepted is the baseline this run honours, nil when there is none. The
// default file being absent is how most repositories run, so it is no error,
// while a named one being absent is a typo worth stopping for.
func (o *lintOptions) accepted(root string) (*baseline.File, error) {
	if o.noBaseline {
		return nil, nil
	}
	if o.baseline != "" {
		return baseline.Load(o.baseline)
	}
	f, err := baseline.Load(filepath.Join(root, baseline.Name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return f, err
}

func applies(cfg *tenets.Config, path string) bool {
	for _, t := range cfg.Tenets {
		if t.Applies(path) {
			return true
		}
	}
	return false
}

// relocate rewrites the repository-relative paths the tenets are matched
// against into paths relative to where the lint was started, which is what a
// terminal needs to open the file it printed.
func relocate(r *report.Report, root, dir string) {
	for i := range r.Findings {
		r.Findings[i].File = displayPath(root, dir, r.Findings[i].File)
	}
	for i := range r.Baselined {
		r.Baselined[i].File = displayPath(root, dir, r.Baselined[i].File)
	}
	for i := range r.Skipped {
		r.Skipped[i].File = displayPath(root, dir, r.Skipped[i].File)
	}
}

func displayPath(root, dir, path string) string {
	return filepath.ToSlash(relativeTo(dir, filepath.Join(root, filepath.FromSlash(path))))
}

func relativeTo(dir, path string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return path
	}
	return rel
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
