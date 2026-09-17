package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenetlint/internal/cache"
	"github.com/zoidsh/tenetlint/internal/importer"
	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/report"
	"github.com/zoidsh/tenetlint/internal/source"
	"github.com/zoidsh/tenetlint/internal/tenets"
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
	from    []string
	dryRun  bool
	force   bool
	config  string
	format  string
	noCache bool
	verbose bool
}

func newInitCmd() *cobra.Command {
	o := &initOptions{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Draft a tenets.yml from the rule files your agents already read",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runInit(cmd, o) },
	}
	f := cmd.Flags()
	f.StringArrayVar(&o.from, "from", nil, "read this rule file instead of the ones init looks for; repeatable")
	f.BoolVar(&o.dryRun, "dry-run", false, "print what would be drafted without writing it")
	f.BoolVar(&o.force, "force", false, "overwrite an existing tenets.yml")
	f.StringVar(&o.config, "config", "", "path to write, tenets.yml in the repository root by default")
	f.StringVar(&o.format, "format", report.FormatText, "output format: text or json")
	f.BoolVar(&o.noCache, "no-cache", false, "ask the model again instead of reusing cached answers")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "report every call on stderr")
	return cmd
}

func runInit(cmd *cobra.Command, o *initOptions) error {
	if o.format != report.FormatText && o.format != report.FormatJSON {
		return fail(fmt.Errorf("--format must be %s or %s, got %q", report.FormatText, report.FormatJSON, o.format))
	}
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	dir, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	root, err := initRoot(ctx, dir)
	if err != nil {
		return fail(err)
	}

	target := o.config
	if target == "" {
		target = filepath.Join(root, tenets.FileName)
	}
	if err := o.checkTarget(target); err != nil {
		return fail(err)
	}

	paths, err := o.rulePaths(root)
	if err != nil {
		return fail(err)
	}
	shown, err := shortPath(dir, target)
	if err != nil {
		return fail(err)
	}
	if len(paths) == 0 {
		return o.starter(cmd, target, shown)
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

	key := jev.KeyFromEnv()
	if key == "" {
		return fail(missingKeyError())
	}
	sorted, stats, err := sortCandidates(ctx, cmd, o, key, candidates)
	if err != nil {
		return fail(err)
	}

	importer.Assign(sorted)
	r := importer.Report{Candidates: sorted, Stats: stats, DryRun: o.dryRun}
	if stats.Tenets > 0 && !o.dryRun {
		draft, err := importer.Draft(sorted)
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

func sortCandidates(ctx context.Context, cmd *cobra.Command, o *initOptions, key string, candidates []importer.Candidate) ([]importer.Sorted, importer.Stats, error) {
	sorter := &importer.Sorter{
		Asker: jev.New(key, jev.WithModel(jev.DefaultModel)),
		Model: jev.DefaultModel,
	}
	if !o.noCache {
		c, err := cache.Open("")
		if err != nil {
			return nil, importer.Stats{}, err
		}
		sorter.Cache = c
	}
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

// starter writes the one rule a repository with no instruction file can start
// from, so that init always leaves something to edit.
func (o *initOptions) starter(cmd *cobra.Command, target, shown string) error {
	r := importer.Report{DryRun: o.dryRun}
	if !o.dryRun {
		if err := os.WriteFile(target, importer.StarterFile(), 0o600); err != nil {
			return fail(err)
		}
		r.Written = shown
	}
	if o.format == report.FormatJSON {
		return writeImport(cmd.OutOrStdout(), r, o.format)
	}
	verb := "wrote a starter " + tenets.FileName + " to "
	if o.dryRun {
		verb = "would write a starter " + tenets.FileName + " to "
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "found no rule files to read; %s%s\n", verb, shown)
	return err
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
