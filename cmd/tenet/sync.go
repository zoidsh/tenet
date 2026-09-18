package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenet/internal/configedit"
	"github.com/zoidsh/tenet/internal/importer"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/provider"
	"github.com/zoidsh/tenet/internal/report"
	"github.com/zoidsh/tenet/internal/tenets"
)

type syncOptions struct {
	draftOptions
}

func newSyncCmd(g *globalOptions) *cobra.Command {
	o := &syncOptions{draftOptions{g: g}}
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Bring an existing " + tenets.FileName + " back in step with your rule files",
		Long: "Re-read the rule files init drafted from and report what they have gained, reworded and dropped.\n\n" +
			"A new rule is appended as a draft and a reworded one has its source, and its sentence where nobody\n" +
			"has edited it, brought up to date. A rule the files no longer hold is reported and left where it is:\n" +
			"sync never deletes anything, because only you know whether a rule still stands.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runSync(cmd, o) },
	}
	addDraftFlags(cmd, &o.draftOptions,
		"print what would change without writing it",
		"path to "+tenets.FileName+", searched for by default")
	return cmd
}

func runSync(cmd *cobra.Command, o *syncOptions) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	if o.format == "" {
		o.format = report.DefaultFormat(out)
	}
	if o.format != report.FormatText && o.format != report.FormatJSON {
		return fail(fmt.Errorf("--format must be %s or %s, got %q", report.FormatText, report.FormatJSON, o.format))
	}

	dir, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	root, err := initRoot(ctx, dir)
	if err != nil {
		return fail(err)
	}
	path, err := o.configPath(dir)
	if err != nil {
		return fail(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	// The config is validated before it is edited, so that a sync never writes
	// its additions into a file that was already broken.
	if _, err := tenets.Parse(data); err != nil {
		return fail(fmt.Errorf("%s: %w", path, err))
	}
	file, err := configedit.Parse(data)
	if err != nil {
		return fail(fmt.Errorf("%s: %w", path, err))
	}

	paths, err := rulePaths(root, o.from)
	if err != nil {
		return fail(err)
	}
	if len(paths) == 0 {
		return fail(fmt.Errorf("found no rule files to read; name one with --from"))
	}
	candidates, read, err := splitRules(root, dir, paths)
	if err != nil {
		return fail(err)
	}

	p, key, err := keyForDraft(cmd, &o.draftOptions)
	if err != nil {
		return fail(err)
	}
	sorted, stats, err := sortCandidates(ctx, cmd, &o.draftOptions, p, key, candidates)
	if err != nil {
		return fail(err)
	}

	config, err := configHeld(file)
	if err != nil {
		return fail(err)
	}
	plan, err := o.syncPlan(ctx, cmd, p, key, sorted, read, config, &stats)
	if err != nil {
		return fail(err)
	}

	shown, err := shortPath(dir, path)
	if err != nil {
		return fail(err)
	}
	r := importer.SyncReport{Plan: plan, Candidates: sorted, Stats: stats, DryRun: o.dryRun}
	if !o.dryRun && edits(plan) {
		if err := apply(file, plan); err != nil {
			return fail(err)
		}
		edited, err := file.Bytes()
		if err != nil {
			return fail(err)
		}
		if _, err := tenets.Parse(edited); err != nil {
			return fail(fmt.Errorf("%s: the sync would not load: %w", path, err))
		}
		if err := configedit.Write(path, edited); err != nil {
			return fail(err)
		}
		r.Written = shown
	}
	if o.format == report.FormatJSON {
		return r.JSON(out)
	}
	return r.Text(out)
}

// configPath is the config to bring back in step: the one named, or the one
// the directory belongs to. A repository that has none is told to run init.
func (o *draftOptions) configPath(dir string) (string, error) {
	if o.config != "" {
		return o.config, nil
	}
	path, _, err := tenets.Find(dir)
	return path, err
}

// configHeld is everything in the config a rule file sentence stood behind,
// with the ids that are already spoken for. The built-in rule ids are among
// them, because a draft named after one would replace that rule.
func configHeld(file *configedit.File) (importer.Held, error) {
	taken := map[string]bool{}
	rules, err := tenets.BuiltinRules()
	if err != nil {
		return importer.Held{}, err
	}
	for _, r := range rules {
		taken[r.ID] = true
	}
	var entries []importer.Entry
	for _, t := range file.Tenets() {
		taken[t.ID] = true
		entries = append(entries, importer.Entry{Tenet: t.ID, Text: t.Tenet, Source: t.Source})
	}
	for _, c := range file.RuleComments() {
		entries = append(entries, importer.Entry{Rule: c.Rule, Source: c.Text})
	}
	return importer.Held{Entries: entries, Taken: taken}, nil
}

func (o *syncOptions) syncPlan(ctx context.Context, cmd *cobra.Command, p provider.Provider, key string, sorted []importer.Sorted, read []string, config importer.Held, stats *importer.Stats) (importer.Plan, error) {
	syncer := &importer.Syncer{
		Asker: p.Client(key, jev.DefaultModel, o.g.clientOptions(p)...),
		Model: jev.DefaultModel,
	}
	c, err := openCache(o.noCache)
	if err != nil {
		return importer.Plan{}, err
	}
	syncer.Cache = c
	if o.verbose {
		errOut := cmd.ErrOrStderr()
		syncer.Log = func(line string) { _, _ = fmt.Fprintln(errOut, line) }
	}
	return syncer.Sync(ctx, sorted, read, config, stats)
}

// edits reports whether the plan changes the file at all, so that a config
// already in step is not rewritten for nothing.
func edits(plan importer.Plan) bool {
	return len(plan.Added) > 0 || len(plan.Changed) > 0 || len(plan.Restated) > 0
}

// apply writes the plan into the parsed config. Stale entries are not in it:
// a rule a file stopped saying is a person's to delete.
func apply(file *configedit.File, plan importer.Plan) error {
	tenetsByID := map[string]*configedit.Tenet{}
	for _, t := range file.Tenets() {
		tenetsByID[t.ID] = t
	}
	comments := map[string][]*configedit.RuleComment{}
	for _, c := range file.RuleComments() {
		comments[c.Rule] = append(comments[c.Rule], c)
	}

	for _, r := range plan.Restated {
		restate(tenetsByID, comments, r.Entry, r.Source, "")
	}
	for _, c := range plan.Changed {
		text := ""
		if c.TextUpdated {
			text = c.New
		}
		restate(tenetsByID, comments, c.Entry, c.Source(), text)
	}

	drafts := make([]configedit.Draft, 0, len(plan.Added))
	for _, c := range plan.Added {
		drafts = append(drafts, configedit.Draft{
			ID:     c.ID,
			Tenet:  c.Text,
			Kind:   importer.DraftKind(c.Kind),
			Source: importer.SourceLine(c.File, c.Text),
		})
	}
	return file.Append(drafts...)
}

// restate points an entry at the sentence its rule file now holds, and rewords
// the tenet itself when text says the person had not touched it.
func restate(byID map[string]*configedit.Tenet, comments map[string][]*configedit.RuleComment, e importer.Entry, source, text string) {
	if e.Tenet != "" {
		t, ok := byID[e.Tenet]
		if !ok {
			return
		}
		t.SetSource(source)
		if text != "" {
			t.SetTenet(text)
		}
		return
	}
	for _, c := range comments[e.Rule] {
		if strings.TrimSpace(c.Text) == strings.TrimSpace(e.Source) {
			c.Set(source)
			return
		}
	}
}
