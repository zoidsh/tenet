package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenetlint/internal/cache"
	"github.com/zoidsh/tenetlint/internal/check"
	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/report"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

type checkOptions struct {
	config      string
	format      string
	noCache     bool
	verbose     bool
	minExamples int
}

func newCheckCmd() *cobra.Command {
	o := &checkOptions{}
	cmd := &cobra.Command{
		Use:   "check [tenet-id...]",
		Short: "Measure how well a tenet's wording separates its labelled examples",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return runCheck(cmd, args, o) },
	}
	f := cmd.Flags()
	f.StringVar(&o.config, "config", "", "path to tenets.yml, searched for by default")
	f.StringVar(&o.format, "format", report.FormatText, "output format: text or json")
	f.BoolVar(&o.noCache, "no-cache", false, "ask the model again instead of reusing cached answers")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "report every call on stderr")
	f.IntVar(&o.minExamples, "min-examples", check.DefaultMinExamples, "report no numbers for a tenet with fewer examples than this")
	return cmd
}

// runCheck never fails the process over what it measures: a blurry tenet is
// something to go and edit, not a broken run.
func runCheck(cmd *cobra.Command, ids []string, o *checkOptions) error {
	if o.format != report.FormatText && o.format != report.FormatJSON {
		return fail(fmt.Errorf("--format must be %s or %s, got %q", report.FormatText, report.FormatJSON, o.format))
	}
	out := cmd.OutOrStdout()

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
		_, err := fmt.Fprintf(out, "no tenet in %s has examples; add examples to one to check it\n", configPath)
		return err
	}

	key := jev.KeyFromEnv()
	if key == "" {
		return fail(missingKeyError())
	}
	results, stats, err := checkExamples(cmd, o, key, model, selected)
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

func checkExamples(cmd *cobra.Command, o *checkOptions, key, model string, ts []*tenets.Tenet) ([]check.Result, check.Stats, error) {
	c := &check.Checker{
		Asker:       jev.New(key, jev.WithModel(model)),
		MinExamples: o.minExamples,
	}
	if !o.noCache {
		opened, err := cache.Open("")
		if err != nil {
			return nil, check.Stats{}, err
		}
		c.Cache = opened
	}
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
