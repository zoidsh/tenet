package main

import (
	"context"
	"fmt"
	"os"
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

// newAsker builds the client a lint run asks. The end-to-end test replaces it
// with one pointed at a local server.
var newAsker = func(key, model string) judge.Asker {
	return jev.New(key, jev.WithModel(model))
}

type lintOptions struct {
	base    string
	config  string
	format  string
	failOn  string
	model   string
	noCache bool
	verbose bool
	quiet   bool
}

func addLintFlags(cmd *cobra.Command, o *lintOptions) {
	f := cmd.Flags()
	f.StringVar(&o.base, "base", "", "lint the working tree against this git ref instead of the staged changes")
	f.StringVar(&o.config, "config", "", "path to tenets.yml, searched for by default")
	f.StringVar(&o.format, "format", report.FormatText, "output format: text or json")
	f.StringVar(&o.failOn, "fail-on", string(tenets.SeverityWarn), "exit 1 on a finding at this severity or above: error, warn, info or never")
	f.StringVar(&o.model, "model", "", "jev model to ask, overriding the one in tenets.yml")
	f.BoolVar(&o.noCache, "no-cache", false, "ask the model again instead of reusing cached answers")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "report skipped files and every call on stderr")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "print the findings without the summary line")
}

func (o *lintOptions) validate() error {
	if o.format != report.FormatText && o.format != report.FormatJSON {
		return fmt.Errorf("--format must be %s or %s, got %q", report.FormatText, report.FormatJSON, o.format)
	}
	if o.failOn != report.FailNever {
		if _, err := tenets.ParseSeverity(o.failOn); err != nil {
			return fmt.Errorf("--fail-on must be error, warn, info or never, got %q", o.failOn)
		}
	}
	return nil
}

func runLint(cmd *cobra.Command, paths []string, o *lintOptions) error {
	if err := o.validate(); err != nil {
		return fail(err)
	}
	ctx := cmd.Context()
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	dir, err := os.Getwd()
	if err != nil {
		return fail(err)
	}

	configPath := o.config
	if configPath == "" {
		found, _, err := tenets.Find(dir)
		if err != nil {
			return fail(err)
		}
		configPath = found
	}
	cfg, err := tenets.Load(configPath)
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
		return fail(fmt.Errorf("%s is not set: export your TypeSafe API key to lint", jev.APIKeyEnv))
	}

	set, err := source.Collect(ctx, source.Options{Dir: dir, Base: o.base, Paths: paths})
	if err != nil {
		return fail(err)
	}
	if o.verbose {
		for _, s := range set.Skipped {
			_, _ = fmt.Fprintf(errOut, "skipped %s: %s\n", s.File, s.Reason)
		}
	}

	r := report.Report{
		Stats:   judge.Stats{Files: len(set.Files)},
		Skipped: set.Skipped,
		Quiet:   o.quiet,
	}
	windows := set.Windows()
	if len(windows) > 0 {
		findings, stats, err := lintWindows(ctx, cmd, o, cfg, windows, key, model)
		if err != nil {
			return fail(err)
		}
		stats.Files = len(set.Files)
		r.Findings, r.Stats = findings, stats
	}

	if err := r.Write(out, o.format, report.ColorEnabled(out)); err != nil {
		return fail(err)
	}
	if code := r.ExitCode(o.failOn); code != report.ExitOK {
		return &exitError{code: code}
	}
	return nil
}

func lintWindows(ctx context.Context, cmd *cobra.Command, o *lintOptions, cfg *tenets.Config, windows []*source.Window, key, model string) ([]judge.Finding, judge.Stats, error) {
	j := &judge.Judge{Asker: newAsker(key, model), Tenets: cfg.Tenets}
	if !o.noCache {
		c, err := cache.Open("")
		if err != nil {
			return nil, judge.Stats{}, err
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
	return j.Run(ctx, windows)
}
