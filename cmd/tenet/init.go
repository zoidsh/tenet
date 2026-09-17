package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenet/internal/importer"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/provider"
	"github.com/zoidsh/tenet/internal/report"
	"github.com/zoidsh/tenet/internal/source"
	"github.com/zoidsh/tenet/internal/tenets"
)

// ruleFiles are the instruction files coding agents read, in the order init
// reads them. A repository that has several is drafted from all of them.
var ruleFiles = []string{
	"CLAUDE.md",
	"AGENTS.md",
	".cursorrules",
	".cursor/rules/*.mdc",
	".github/copilot-instructions.md",
	".github/instructions/*.instructions.md",
	"BUGBOT.md",
}

type initOptions struct {
	g        *globalOptions
	from     []string
	presets  []string
	agents   []string
	dryRun   bool
	force    bool
	config   string
	format   string
	noCache  bool
	verbose  bool
	noPrompt bool
}

func newInitCmd(g *globalOptions) *cobra.Command {
	o := &initOptions{g: g}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Draft a tenets.yml from the rule files your agents already read",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runInit(cmd, o) },
	}
	f := cmd.Flags()
	f.StringArrayVar(&o.from, "from", nil, "read this rule file instead of the ones init looks for; repeatable")
	f.StringArrayVar(&o.presets, "preset", nil, "start from this built-in preset instead of reading any rule file; repeatable")
	f.StringArrayVar(&o.agents, "agent", nil, "write the tenet instructions where this agent reads them: cursor, agents or claude; repeatable")
	f.BoolVar(&o.dryRun, "dry-run", false, "print what would be drafted without writing it")
	f.BoolVar(&o.force, "force", false, "overwrite an existing tenets.yml")
	f.StringVar(&o.config, "config", "", "path to write, tenets.yml in the repository root by default")
	f.StringVar(&o.format, "format", "", "output format: text or json (default text on a terminal, json otherwise)")
	f.BoolVar(&o.noCache, "no-cache", false, "ask the model again instead of reusing cached answers, which are still written")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "report every call on stderr")
	f.BoolVar(&o.noPrompt, "no-prompt", false, "never ask for an API key, even at a terminal")
	return cmd
}

// keyForInit asks for a key rather than only naming the variable, because
// init is the first command most people run and the sort it is about to do
// needs one. Anything that is not a person at a terminal is told the same
// thing every other command tells them.
func keyForInit(cmd *cobra.Command, o *initOptions) (provider.Provider, string, error) {
	p, err := provider.Lookup(provider.Default)
	if err != nil {
		return p, "", err
	}
	key, _, err := resolveKey(o.g, p)
	if err != nil || key != "" {
		return p, key, err
	}
	if o.noPrompt || !isTerminal(cmd.InOrStdin()) || !isTerminal(cmd.OutOrStdout()) {
		return p, "", missingKeyError(p)
	}
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "Sorting the rules asks %s's jev model, which needs an API key from https://typesafe.ai.\n", p.Label); err != nil {
		return p, "", err
	}
	if _, err := fmt.Fprint(out, "Enter it now? [Y/n] "); err != nil {
		return p, "", err
	}
	yes, err := confirm(cmd.InOrStdin())
	if err != nil {
		return p, "", err
	}
	if !yes {
		return p, "", missingKeyError(p)
	}
	key, err = saveKey(cmd, p, &authOptions{g: o.g})
	return p, key, err
}

// confirm reads the answer to a yes-or-no question, where a bare newline is
// the yes the prompt capitalised and end of input is a no.
func confirm(r io.Reader) (bool, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return false, scanner.Err()
	}
	switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
	case "", "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func runInit(cmd *cobra.Command, o *initOptions) error {
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

	// The instructions are about running the lint, not about what it runs, so
	// asking for them is a whole run of its own and leaves any tenets.yml,
	// drafted or hand-written, alone.
	if len(o.agents) > 0 {
		return o.writeAgents(cmd, root, dir)
	}

	target, err := o.target(root)
	if err != nil {
		return fail(err)
	}
	if err := o.checkTarget(target); err != nil {
		return fail(err)
	}

	for _, name := range o.presets {
		if _, err := tenets.BuiltinPreset(name); err != nil {
			return fail(err)
		}
	}

	paths, err := o.rulePaths(root)
	if err != nil {
		return fail(err)
	}
	shown, err := shortPath(dir, target)
	if err != nil {
		return fail(err)
	}
	// A preset is already a set of rules, so on its own it is the whole file;
	// only --from asks for rule files to be read as well.
	if len(o.presets) > 0 && len(o.from) == 0 {
		return o.writePresets(cmd, o.presets, target, shown, "")
	}
	if len(paths) == 0 {
		return o.writePresets(cmd, []string{importer.DefaultPreset}, target, shown, "found no rule files to read; ")
	}

	var candidates []importer.Candidate
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return fail(err)
		}
		name, err := sourceName(root, dir, path)
		if err != nil {
			return fail(err)
		}
		candidates = append(candidates, importer.Split(name, data)...)
	}

	p, key, err := keyForInit(cmd, o)
	if err != nil {
		return fail(err)
	}
	sorted, stats, err := sortCandidates(ctx, cmd, o, p, key, candidates)
	if err != nil {
		return fail(err)
	}

	importer.Assign(sorted)
	r := importer.Report{Candidates: sorted, Stats: stats, DryRun: o.dryRun}
	if stats.Tenets > 0 && !o.dryRun {
		draft, err := importer.Draft(sorted, o.presets)
		if err != nil {
			return fail(err)
		}
		if err := os.WriteFile(target, draft, 0o600); err != nil {
			return fail(err)
		}
		r.Written = shown
	}
	return writeImport(out, r, o.format)
}

func sortCandidates(ctx context.Context, cmd *cobra.Command, o *initOptions, p provider.Provider, key string, candidates []importer.Candidate) ([]importer.Sorted, importer.Stats, error) {
	sorter := &importer.Sorter{
		Asker: p.Client(key, jev.DefaultModel, o.g.clientOptions(p)...),
		Model: jev.DefaultModel,
	}
	c, err := openCache(o.noCache)
	if err != nil {
		return nil, importer.Stats{}, err
	}
	sorter.Cache = c
	if o.verbose {
		errOut := cmd.ErrOrStderr()
		sorter.Log = func(line string) { _, _ = fmt.Fprintln(errOut, line) }
	}
	return sorter.Sort(ctx, candidates)
}

func writeImport(out io.Writer, r importer.Report, format string) error {
	if format == report.FormatJSON {
		if err := r.JSON(out); err != nil {
			return fail(err)
		}
		return nil
	}
	if err := r.Text(out); err != nil {
		return fail(err)
	}
	return nil
}

// writePresets writes the rules that ship in the binary and nothing else,
// which is what a repository with no instruction file of its own starts from.
func (o *initOptions) writePresets(cmd *cobra.Command, presets []string, target, shown, note string) error {
	r := importer.Report{DryRun: o.dryRun}
	if !o.dryRun {
		if err := os.WriteFile(target, importer.PresetFile(presets), 0o600); err != nil {
			return fail(err)
		}
		r.Written = shown
	}
	if o.format == report.FormatJSON {
		return writeImport(cmd.OutOrStdout(), r, o.format)
	}
	verb := "wrote "
	if o.dryRun {
		verb = "would write "
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s%s%s with the %s preset in it, to %s\n",
		note, verb, tenets.FileName, strings.Join(presets, " and "), shown)
	return err
}

// draftFlags are about drafting a tenets.yml, which a run that writes agent
// instructions does not do.
var draftFlags = []string{"from", "preset", "config", "force"}

func checkAgentFlags(cmd *cobra.Command) error {
	for _, name := range draftFlags {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--agent writes no %s, so it cannot be combined with --%s", tenets.FileName, name)
		}
	}
	return nil
}

// writeAgents puts the tenet instructions where each named agent reads them.
// Every name is resolved before anything is written, so that a typo in the
// second one does not leave the first one written.
func (o *initOptions) writeAgents(cmd *cobra.Command, root, dir string) error {
	if err := checkAgentFlags(cmd); err != nil {
		return fail(err)
	}
	targets := make([]importer.AgentTarget, 0, len(o.agents))
	for _, name := range o.agents {
		target, err := importer.Agent(name)
		if err != nil {
			return fail(err)
		}
		targets = append(targets, target)
	}
	out := cmd.OutOrStdout()
	for _, target := range targets {
		path := filepath.Join(root, filepath.FromSlash(target.Path))
		replaced, err := o.writeAgentFile(path, target)
		if err != nil {
			return fail(err)
		}
		shown, err := shortPath(dir, path)
		if err != nil {
			return fail(err)
		}
		if _, err := fmt.Fprintf(out, "%s the tenet instructions in %s\n", agentVerb(o.dryRun, replaced), shown); err != nil {
			return fail(err)
		}
	}
	return nil
}

func agentVerb(dryRun, replaced bool) string {
	switch {
	case dryRun && replaced:
		return "would replace"
	case dryRun:
		return "would write"
	case replaced:
		return "replaced"
	default:
		return "wrote"
	}
}

// writeAgentFile reads the file even on a dry run, because what it would say
// about a file it is not writing is whether that file already has a section.
func (o *initOptions) writeAgentFile(path string, target importer.AgentTarget) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	data, replaced := target.Content(existing)
	if o.dryRun {
		return replaced, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return false, err
	}
	return replaced, os.WriteFile(path, data, 0o600)
}

// target is the file to write, absolute because everything downstream of it
// is stated relative to somewhere: the repository root for a source line, the
// working directory for what is printed.
func (o *initOptions) target(root string) (string, error) {
	if o.config == "" {
		return filepath.Join(root, tenets.FileName), nil
	}
	return filepath.Abs(o.config)
}

// checkTarget refuses to overwrite a tenets.yml somebody has edited. A dry run
// writes nothing, so it has nothing to refuse.
func (o *initOptions) checkTarget(target string) error {
	if o.dryRun || o.force {
		return nil
	}
	switch _, err := os.Stat(target); {
	case err == nil:
		return fmt.Errorf("%s already exists; pass --force to replace it", target)
	case os.IsNotExist(err):
		return nil
	default:
		return err
	}
}

// rulePaths are the files to draft from: the ones named on the command line,
// or every well-known instruction file in the repository.
func (o *initOptions) rulePaths(root string) ([]string, error) {
	if len(o.from) > 0 {
		for _, path := range o.from {
			if _, err := os.Stat(path); err != nil {
				return nil, err
			}
		}
		return o.from, nil
	}
	var paths []string
	for _, pattern := range ruleFiles {
		full := filepath.Join(root, filepath.FromSlash(pattern))
		if !strings.Contains(pattern, "*") {
			if info, err := os.Stat(full); err == nil && !info.IsDir() {
				paths = append(paths, full)
			}
			continue
		}
		matches, err := filepath.Glob(full)
		if err != nil {
			return nil, err
		}
		sort.Strings(matches)
		paths = append(paths, matches...)
	}
	return paths, nil
}

// initRoot is the repository the rule files and the tenets.yml belong to, or
// the current directory when there is no repository to speak of.
func initRoot(ctx context.Context, dir string) (string, error) {
	root, err := source.RepoRoot(ctx, dir)
	if errors.Is(err, source.ErrNotARepository) {
		return dir, nil
	}
	if err != nil {
		return "", err
	}
	return root, nil
}

// sourceName is what a tenet's source line names the file by: its path inside
// the repository, so that the reference holds wherever it is read from. A file
// from outside the repository keeps the path the terminal can open.
func sourceName(root, dir, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return shortPath(dir, abs)
	}
	return filepath.ToSlash(rel), nil
}

func shortPath(dir, path string) (string, error) {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
