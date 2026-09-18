package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenet/internal/check"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/provider"
	"github.com/zoidsh/tenet/internal/report"
	"github.com/zoidsh/tenet/internal/tenets"
)

type checkOptions struct {
	g           *globalOptions
	config      string
	builtin     bool
	format      string
	noCache     bool
	verbose     bool
	minExamples int
	runs        int
}

// builtinConfigPath is what the messages name when the run measures the rules
// in the binary rather than a file somebody wrote.
const builtinConfigPath = "the built-in rules"

// configForCheck is what the run measures: the tenets this repository resolves
// to, or every rule that ships in the binary, which is how the corpus itself
// is kept honest.
func configForCheck(o *checkOptions) (*tenets.Config, error) {
	if !o.builtin {
		return openConfig(o.config)
	}
	if o.config != "" {
		return nil, errors.New("--builtin measures the rules in the binary, so --config has nothing to say")
	}
	rules, err := tenets.BuiltinRules()
	if err != nil {
		return nil, err
	}
	cfg := &tenets.Config{Version: 1, Path: builtinConfigPath}
	for _, rule := range rules {
		cfg.Tenets = append(cfg.Tenets, rule.Tenet)
	}
	return cfg, nil
}

func newCheckCmd(g *globalOptions) *cobra.Command {
	o := &checkOptions{g: g}
	cmd := &cobra.Command{
		Use:   "check [tenet-id...]",
		Short: "Measure how well a tenet's wording separates its labelled examples",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return runCheck(cmd, args, o) },
	}
	f := cmd.Flags()
	f.StringVar(&o.config, "config", "", "path to "+tenets.FileName+", searched for by default")
	f.BoolVar(&o.builtin, "builtin", false, "measure every rule that ships in the binary, whatever the config turns on")
	f.StringVar(&o.format, "format", "", "output format: text or json (default text on a terminal, json otherwise)")
	f.BoolVar(&o.noCache, "no-cache", false, "ask the model again instead of reusing cached answers, which are still written")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "report every call on stderr")
	f.IntVar(&o.minExamples, "min-examples", check.DefaultMinExamples, "report no numbers for a tenet with fewer examples than this")
	f.IntVar(&o.runs, "runs", 1, "judge every example this many times and report how far the answers moved")
	return cmd
}

// runCheck never fails the process over what it measures: a blurry tenet is
// something to go and edit, not a broken run.
func runCheck(cmd *cobra.Command, ids []string, o *checkOptions) error {
	out := cmd.OutOrStdout()
	if o.format == "" {
		o.format = report.DefaultFormat(out)
	}
	if o.format != report.FormatText && o.format != report.FormatJSON {
		return fail(fmt.Errorf("--format must be %s or %s, got %q", report.FormatText, report.FormatJSON, o.format))
	}

	if o.runs < 1 {
		return fail(fmt.Errorf("--runs is how many times each example is judged, so it is at least 1, got %d", o.runs))
	}

	cfg, err := configForCheck(o)
	if err != nil {
		return fail(err)
	}
	model := cfg.Model
	if model == "" {
		model = jev.DefaultModel
	}
	cfg.SetModel(model)

	selected, err := selectTenets(cfg, ids)
	if err != nil {
		return fail(err)
	}
	if len(selected) == 0 {
		_, err := fmt.Fprintf(out, "no tenet in %s has examples; add examples to one to check it\n", cfg.Path)
		return err
	}

	p, key, err := keyFor(o.g, cfg)
	if err != nil {
		return fail(err)
	}
	results, stats, err := checkExamples(cmd, o, p, key, model, selected)
	if err != nil {
		return fail(err)
	}
	r := check.Report{Results: results, Stats: stats, MinExamples: o.minExamples}
	if err := r.Write(out, o.format); err != nil {
		return fail(err)
	}
	return nil
}

// selectTenets is what the ids name, or every tenet that has examples when
// they name nothing.
func selectTenets(cfg *tenets.Config, ids []string) ([]*tenets.Tenet, error) {
	if len(ids) == 0 {
		var withExamples []*tenets.Tenet
		for _, t := range cfg.Tenets {
			if len(t.Examples) > 0 {
				withExamples = append(withExamples, t)
			}
		}
		return withExamples, nil
	}
	byID := make(map[string]*tenets.Tenet, len(cfg.Tenets))
	known := make([]string, 0, len(cfg.Tenets))
	for _, t := range cfg.Tenets {
		byID[t.ID] = t
		known = append(known, t.ID)
	}
	selected := make([]*tenets.Tenet, 0, len(ids))
	for _, id := range ids {
		t, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("no tenet %q in %s; it has %s", id, cfg.Path, strings.Join(known, ", "))
		}
		selected = append(selected, t)
	}
	return selected, nil
}

func checkExamples(cmd *cobra.Command, o *checkOptions, p provider.Provider, key, model string, ts []*tenets.Tenet) ([]check.Result, check.Stats, error) {
	c := &check.Checker{
		Asker:       p.Client(key, model, o.g.clientOptions(p)...),
		MinExamples: o.minExamples,
		Runs:        o.runs,
	}
	opened, err := openCache(o.noCache)
	if err != nil {
		return nil, check.Stats{}, err
	}
	c.Cache = opened
	if o.verbose {
		errOut := cmd.ErrOrStderr()
		// Examples are judged concurrently, so the log lines need a lock of
		// their own to arrive whole.
		var mu sync.Mutex
		c.Log = func(line string) {
			mu.Lock()
			defer mu.Unlock()
			_, _ = fmt.Fprintln(errOut, line)
		}
	}
	return c.Run(cmd.Context(), ts)
}
