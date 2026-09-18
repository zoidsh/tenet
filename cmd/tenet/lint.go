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

	"github.com/zoidsh/tenet/internal/auth"
	"github.com/zoidsh/tenet/internal/baseline"
	"github.com/zoidsh/tenet/internal/cache"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/judge"
	"github.com/zoidsh/tenet/internal/provider"
	"github.com/zoidsh/tenet/internal/receipt"
	"github.com/zoidsh/tenet/internal/report"
	"github.com/zoidsh/tenet/internal/source"
	"github.com/zoidsh/tenet/internal/tenets"
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

func fail(err error) error {
	if errors.Is(err, tenets.ErrNotFound) {
		return &exitError{code: report.ExitNoConfig, err: err}
	}
	return &exitError{code: report.ExitError, err: err}
}

// openCache is the cache every command that asks the model writes to.
// --no-cache is a run that wants fresh answers, not one that leaves the next
// run as cold as itself, so it bypasses the reads alone.
func openCache(noCache bool) (*cache.Cache, error) {
	c, err := cache.Open("")
	if err != nil {
		return nil, err
	}
	if noCache {
		c.WriteOnly()
	}
	return c, nil
}

// missingKeyError is shared with init, so that whichever command a newcomer
// runs first names the same key and the same way out.
func missingKeyError(p provider.Provider) error {
	return fmt.Errorf("no %s API key: run tenet auth, or set %s; set %s=1 to commit without linting", p.Label, p.Env, SkipEnv)
}

// keyFor is the key this config's provider is asked with.
func keyFor(g *globalOptions, cfg *tenets.Config) (provider.Provider, string, error) {
	p, err := provider.Lookup(cfg.Provider)
	if err != nil {
		return p, "", err
	}
	if err := p.Available(); err != nil {
		return p, "", err
	}
	key, _, err := resolveKey(g, p)
	if err != nil {
		return p, "", err
	}
	if key == "" {
		return p, "", missingKeyError(p)
	}
	return p, key, nil
}

// resolveKey is auth.Resolve with the flag in front of it, which is the one
// source a person names for a single run.
func resolveKey(g *globalOptions, p provider.Provider) (key, source string, err error) {
	if key := g.key(p); key != "" {
		return key, auth.SourceFlag, nil
	}
	return auth.Resolve(p)
}

type lintOptions struct {
	g             *globalOptions
	base          string
	commitMsg     string
	prText        string
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
	f.StringVar(&o.prText, "pr-text", "", "lint the pull request title and description in this file instead of any code")
	f.StringVar(&o.format, "format", "", "output format: text, json or github (default text on a terminal, json otherwise)")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "print the findings without the summary line")
	f.StringVar(&o.baseline, "baseline", "", "read this baseline instead of "+baseline.Name+" beside the config")
	f.BoolVar(&o.noBaseline, "no-baseline", false, "report every finding, whatever the baseline accepts")
	f.BoolVar(&o.showBaselined, "show-baselined", false, "list the findings the baseline accepts as well, marked and still passing")
}

// addRunFlags are the flags that choose what is judged and how, which baseline
// takes too so that it accepts exactly what a lint would have found.
func addRunFlags(cmd *cobra.Command, o *lintOptions) {
	f := cmd.Flags()
	f.StringVar(&o.base, "base", "", "lint the working tree against this git ref instead of the staged changes")
	f.StringVar(&o.config, "config", "", "path to "+tenets.FileName+", searched for by default")
	f.StringVar(&o.model, "model", "", "jev model to ask, overriding the one in "+tenets.FileName)
	f.BoolVar(&o.noCache, "no-cache", false, "ask the model again instead of reusing cached answers, which are still written")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "report skipped files and what each window costs on stderr")
}

func (o *lintOptions) validate(out io.Writer, paths []string) error {
	if o.commitMsg != "" {
		switch {
		case len(paths) > 0:
			return fmt.Errorf("--commit-msg lints the message on its own, so it cannot be given paths as well")
		case o.base != "":
			return fmt.Errorf("--commit-msg lints the message on its own, so it cannot be combined with --base")
		case o.prText != "":
			return fmt.Errorf("--commit-msg lints the message on its own, so it cannot be combined with --pr-text")
		}
	}
	if o.prText != "" {
		switch {
		case len(paths) > 0:
			return fmt.Errorf("--pr-text lints the pull request text on its own, so it cannot be given paths as well")
		case o.base != "":
			return fmt.Errorf("--pr-text lints the pull request text on its own, so it cannot be combined with --base")
		}
	}
	if o.baseline != "" && o.noBaseline {
		return fmt.Errorf("--no-baseline ignores the baseline, so there is no file for --baseline to name")
	}
	if o.format == "" {
		o.format = report.DefaultFormat(out)
	}
	switch o.format {
	case report.FormatText, report.FormatJSON, report.FormatGitHub:
	default:
		return fmt.Errorf("--format must be %s, %s or %s, got %q",
			report.FormatText, report.FormatJSON, report.FormatGitHub, o.format)
	}
	return nil
}

// run is what one pass over the selected code amounts to, before anything
// decides what to print or write about it. The findings' paths are still the
// repository-relative ones the tenets were matched against.
type run struct {
	dir string
	cfg *tenets.Config

	// configDir is where tenet's own files for this run live, so that a
	// baseline sits beside the config it was accepted against.
	configDir string

	model    string
	provider provider.Provider
	key      string
	set      *source.Set
	outcome  judge.Outcome
}

// collectRun reads the config and gathers what the options select, without
// asking the model anything. It is a step of its own because what a run is
// allowed to do with its findings, such as pruning a baseline, is settled
// against the files it covers, and settling that after the calls would have
// paid for them.
func collectRun(cmd *cobra.Command, paths []string, o *lintOptions) (*run, error) {
	r, err := openRun(o)
	if err != nil {
		return nil, err
	}
	if err := r.collect(cmd, paths, o); err != nil {
		return nil, err
	}
	return r, nil
}

// openRun settles the config, where tenet's files live and which model will be
// asked. It is what a receipt is judged against, and it is separate from the
// collecting because a run a receipt excuses asks the model nothing and so
// needs no API key.
func openRun(o *lintOptions) (*run, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	cfg, err := openConfig(o.config)
	if err != nil {
		return nil, err
	}
	configDir, err := filepath.Abs(filepath.Dir(cfg.Path))
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

	return &run{dir: dir, cfg: cfg, configDir: configDir, model: model, set: &source.Set{}}, nil
}

func (r *run) collect(cmd *cobra.Command, paths []string, o *lintOptions) error {
	ctx := cmd.Context()
	p, key, err := keyFor(o.g, r.cfg)
	if err != nil {
		return err
	}

	cfg := r.cfg
	ids := make([]string, 0, len(cfg.Tenets))
	for _, t := range cfg.Tenets {
		ids = append(ids, t.ID)
	}
	// A repository whose tenets say nothing about the commit message or the
	// pull request text must not pay for a call on every commit or push, so
	// neither is even read. The set then has no root either: a run that read
	// nothing has no paths to print, and the directory it started in is not
	// where they would be relative to.
	set := &source.Set{}
	if text := textPath(o); text == "" || applies(cfg, text) {
		set, err = source.Collect(ctx, source.Options{Dir: r.dir, Base: o.base, Paths: paths, CommitMsg: o.commitMsg, PRText: o.prText, Tenets: ids})
		if err != nil {
			return err
		}
	}
	r.provider = p
	r.key = key
	r.set = set
	r.outcome = judge.Outcome{Stats: judge.Stats{Files: len(set.Files)}}
	return nil
}

// judge asks the model about everything collected.
func (r *run) judge(cmd *cobra.Command, o *lintOptions) error {
	windows := r.set.Windows()
	if len(windows) == 0 {
		return nil
	}
	outcome, err := lintWindows(cmd.Context(), cmd, o, r.cfg, windows, r.provider, r.key, r.model)
	if err != nil {
		return err
	}
	outcome.Stats.Files = len(r.set.Files)
	r.outcome = outcome
	return nil
}

func runLint(cmd *cobra.Command, paths []string, o *lintOptions) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := o.validate(out, paths); err != nil {
		return fail(err)
	}

	run, err := openRun(o)
	if err != nil {
		return fail(err)
	}
	// The baseline is read before the calls are made, so that a run pointed at
	// a file that is not there says so instead of billing for the answer first.
	accepted, err := o.accepted(run.configDir)
	if err != nil {
		return fail(err)
	}
	receiptPath, receiptKey := receiptFor(cmd, run, paths, o)
	if receiptKey != "" && receipt.Load(receiptPath) == receiptKey {
		return reportExcused(cmd, o)
	}

	if err := run.collect(cmd, paths, o); err != nil {
		return fail(err)
	}
	if err := run.judge(cmd, o); err != nil {
		return fail(err)
	}
	set, dir, near := run.set, run.dir, run.outcome.NearMisses
	findings, baselined := accepted.Split(run.outcome.Findings)

	r := report.Report{
		Findings:      findings,
		Baselined:     baselined,
		ShowBaselined: o.showBaselined,
		Stats:         run.outcome.Stats,
		Skipped:       set.Skipped,
		Quiet:         o.quiet,
	}
	switch {
	case o.commitMsg != "":
		r.Next = report.NextCommitMsg
	case o.prText != "":
		r.Next = report.NextPR
	case len(paths) == 0 && o.base == "":
		r.Next = report.NextStaged
	}

	// GitHub resolves an annotation's path against the checkout root rather
	// than the directory the step ran in, so that report keeps the paths the
	// tenets were matched against.
	if o.format != report.FormatGitHub {
		relocate(&r, set.Root, dir)
	}
	if o.verbose {
		for _, s := range r.Skipped {
			_, _ = fmt.Fprintf(errOut, "skipped %s: %s\n", s.File, s.Reason)
		}
		for _, n := range near {
			_, _ = fmt.Fprintf(errOut, "near miss %s:%d: %s (p=%.2f, fails at %.2f)\n",
				displayPath(set.Root, dir, n.File), n.Line, n.Tenet, n.Probability, n.Fail)
		}
	}
	color, err := report.Colored(out, o.g.color)
	if err != nil {
		return fail(err)
	}
	if err := r.Write(out, o.format, color); err != nil {
		return fail(err)
	}
	if code := r.ExitCode(); code != report.ExitOK {
		return &exitError{code: code}
	}
	if receiptKey != "" {
		saveReceipt(cmd, o, receiptPath, receiptKey)
	}
	return nil
}

// reportExcused reports the run a receipt excused, which found nothing because
// this staged tree was judged clean already.
func reportExcused(cmd *cobra.Command, o *lintOptions) error {
	out := cmd.OutOrStdout()
	color, err := report.Colored(out, o.g.color)
	if err != nil {
		return fail(err)
	}
	r := report.Report{Quiet: o.quiet, Receipt: true, ShowBaselined: o.showBaselined}
	if err := r.Write(out, o.format, color); err != nil {
		return fail(err)
	}
	return nil
}

// receiptFor is the receipt this run may be excused by and would leave behind,
// empty when there is none to be had. Only the default staged run has one: the
// index is what it reads every file's content out of, so the tree the index
// writes says exactly what would be judged. --no-cache asks for fresh answers,
// which is also a run that wants no receipt, neither read nor written.
//
// Whatever goes wrong here leaves the run without a receipt rather than
// stopping it, because a receipt only ever saves a call, and a commit must
// never fail over one.
func receiptFor(cmd *cobra.Command, r *run, paths []string, o *lintOptions) (path, key string) {
	if len(paths) > 0 || o.base != "" || o.commitMsg != "" || o.prText != "" || o.noCache {
		return "", ""
	}
	ctx := cmd.Context()
	path, err := receipt.Path(ctx, r.dir)
	if err != nil {
		logReceipt(cmd, o, err)
		return "", ""
	}
	in, err := receiptInputs(ctx, r, o)
	if err != nil {
		logReceipt(cmd, o, err)
		return "", ""
	}
	return path, receipt.Key(in)
}

func receiptInputs(ctx context.Context, r *run, o *lintOptions) (receipt.Inputs, error) {
	head, err := receipt.Head(ctx, r.dir)
	if err != nil {
		return receipt.Inputs{}, err
	}
	tree, err := receipt.Tree(ctx, r.dir)
	if err != nil {
		return receipt.Inputs{}, err
	}
	config, err := os.ReadFile(r.cfg.Path)
	if err != nil {
		return receipt.Inputs{}, err
	}
	in := receipt.Inputs{Head: head, Tree: tree, Config: config, Model: r.model}
	for _, t := range r.cfg.Tenets {
		if t.ExamplesFrom == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(r.cfg.Path), filepath.FromSlash(t.ExamplesFrom)))
		if err != nil {
			return receipt.Inputs{}, err
		}
		in.Examples = append(in.Examples, receipt.Example{Name: t.ExamplesFrom, Data: data})
	}
	if path := o.baselinePath(r.configDir); path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			in.Baseline, in.HasBaseline = data, true
		case !errors.Is(err, fs.ErrNotExist):
			return receipt.Inputs{}, err
		}
	}
	return in, nil
}

func saveReceipt(cmd *cobra.Command, o *lintOptions, path, key string) {
	if err := receipt.Save(path, key); err != nil {
		logReceipt(cmd, o, err)
	}
}

func logReceipt(cmd *cobra.Command, o *lintOptions, err error) {
	if o.verbose {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "no receipt: %s\n", err)
	}
}

// accepted is the baseline this run honours, nil when there is none. The
// default file being absent is how most repositories run, so it is no error,
// while a named one being absent is a typo worth stopping for.
func (o *lintOptions) accepted(configDir string) (*baseline.File, error) {
	path := o.baselinePath(configDir)
	if path == "" {
		return nil, nil
	}
	f, err := baseline.Load(path)
	if o.baseline == "" && errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return f, err
}

// baselinePath is the file the run honours, empty when it honours none.
func (o *lintOptions) baselinePath(configDir string) string {
	switch {
	case o.noBaseline:
		return ""
	case o.baseline != "":
		return o.baseline
	}
	return filepath.Join(configDir, baseline.Name)
}

func textPath(o *lintOptions) string {
	switch {
	case o.commitMsg != "":
		return source.CommitMsgPath
	case o.prText != "":
		return source.PRTextPath
	}
	return ""
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

func lintWindows(ctx context.Context, cmd *cobra.Command, o *lintOptions, cfg *tenets.Config, windows []*source.Window, p provider.Provider, key, model string) (judge.Outcome, error) {
	j := &judge.Judge{Asker: p.Client(key, model, o.g.clientOptions(p)...), Tenets: cfg.Tenets}
	c, err := openCache(o.noCache)
	if err != nil {
		return judge.Outcome{}, err
	}
	j.Cache = c
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
