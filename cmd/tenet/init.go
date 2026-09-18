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

// draftOptions are what init and sync both read: which rule files to split,
// what the sort may do and where the answer goes.
type draftOptions struct {
	g        *globalOptions
	from     []string
	dryRun   bool
	config   string
	format   string
	noCache  bool
	verbose  bool
	noPrompt bool
}

type initOptions struct {
	draftOptions
	presets []string
	agents  []string
	force   bool
}

// addDraftFlags gives a command the flags init and sync share, worded the same
// way in both because they do the same thing in both.
func addDraftFlags(cmd *cobra.Command, o *draftOptions, dryRun, config string) {
	f := cmd.Flags()
	f.StringArrayVar(&o.from, "from", nil, "read this rule file instead of the ones "+cmd.Name()+" looks for; repeatable")
	f.BoolVar(&o.dryRun, "dry-run", false, dryRun)
	f.StringVar(&o.config, "config", "", config)
	f.StringVar(&o.format, "format", "", "output format: text or json (default text on a terminal, json otherwise)")
	f.BoolVar(&o.noCache, "no-cache", false, "ask the model again instead of reusing cached answers, which are still written")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "report every call on stderr")
	f.BoolVar(&o.noPrompt, "no-prompt", false, "never ask for an API key, even at a terminal")
}

func newInitCmd(g *globalOptions) *cobra.Command {
	o := &initOptions{draftOptions: draftOptions{g: g}}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Draft a " + tenets.FileName + " from the rule files your agents already read",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runInit(cmd, o) },
	}
	addDraftFlags(cmd, &o.draftOptions,
		"print what would be drafted without writing it",
		"path to write, "+tenets.FileName+" in the repository root by default")
	f := cmd.Flags()
	f.StringArrayVar(&o.presets, "preset", nil, "start from this built-in preset instead of reading any rule file; repeatable")
	f.StringArrayVar(&o.agents, "agent", nil, "write the tenet instructions where this agent reads them: cursor, agents or claude; repeatable")
	f.BoolVar(&o.force, "force", false, "overwrite an existing "+tenets.FileName)
	return cmd
}

// keyForDraft asks for a key rather than only naming the variable, because
// init is the first command most people run and the sort it is about to do
// needs one. Anything that is not a person at a terminal is told the same
// thing every other command tells them.
func keyForDraft(cmd *cobra.Command, o *draftOptions) (provider.Provider, string, error) {
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
	// asking for them is a whole run of its own and leaves any config,
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

	paths, err := rulePaths(root, o.from)
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

	candidates, _, err := splitRules(root, dir, paths)
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

	importer.Assign(sorted)
	r := importer.Report{Candidates: sorted, Stats: stats, DryRun: o.dryRun}
	if stats.Tenets > 0 && !o.dryRun {
		draft, err := importer.Draft(sorted, o.presets)
		if err != nil {
			return fail(err)
		}
		if err := writeConfig(target, draft); err != nil {
			return fail(err)
		}
		r.Written = shown
	}
	return writeImport(out, r, o.format)
}

func sortCandidates(ctx context.Context, cmd *cobra.Command, o *draftOptions, p provider.Provider, key string, candidates []importer.Candidate) ([]importer.Sorted, importer.Stats, error) {
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
		if err := writeConfig(target, importer.PresetFile(presets)); err != nil {
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

// draftFlags are about drafting a config, which a run that writes agent
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
func (o *draftOptions) target(root string) (string, error) {
	if o.config == "" {
		return source.ConfigPath(root), nil
	}
	return filepath.Abs(o.config)
}

// writeConfig writes the drafted config, making the directory it belongs in.
func writeConfig(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o600)
}

// checkTarget refuses to overwrite a config somebody has edited. A dry run
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

// splitRules turns every rule file into the candidates the sort decides on,
// each naming the file by the path a tenet's source will quote. The names come
// back as well, because a file that was read and held no rule still says
// something about the rules drafted from it before.
func splitRules(root, dir string, paths []string) ([]importer.Candidate, []string, error) {
	var candidates []importer.Candidate
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		name, err := sourceName(root, dir, path)
		if err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		candidates = append(candidates, importer.Split(name, data)...)
	}
	return candidates, names, nil
}

// rulePaths are the files to draft from: the ones named on the command line,
// or every well-known instruction file in the repository.
func rulePaths(root string, from []string) ([]string, error) {
	if len(from) > 0 {
		for _, path := range from {
			if _, err := os.Stat(path); err != nil {
				return nil, err
			}
		}
		return from, nil
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

// initRoot is the repository the rule files and the config belong to, or
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
