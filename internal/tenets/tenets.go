// Package tenets loads the rules tenet judges code against.
package tenets

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"

	"github.com/zoidsh/tenet/internal/provider"
	"github.com/zoidsh/tenet/internal/source"
)

// FileName is the config file Find looks for, relative to a repository root.
const FileName = source.ConfigFile

// ErrNotFound is what Find wraps, so that a caller can tell a repository that
// does not use tenet from a run that broke.
var ErrNotFound = errors.New("no " + FileName + " found")

// DefaultFail is the probability at or above which a verdict becomes a
// finding, for a tenet that names no cutoff of its own. Measured over the
// spike's 126 cases, 0.8 keeps 84% of the violations against two borderline
// false alarms, where 0.9 keeps 59%.
const DefaultFail = 0.8

// Label is what an example is known to be.
type Label string

// The labels an example may carry.
const (
	LabelViolation Label = "violation"
	LabelOK        Label = "ok"
)

// Example is a labelled snippet that check measures a tenet against. Lines is
// where within Code a finding should land, left out when the example is not
// about particular lines. Note is for a label the tenet's sentence does not
// obviously settle; nothing reads it but a person deciding whether the label
// is still right.
type Example struct {
	Label Label     `yaml:"label"`
	Lines LineRange `yaml:"lines"`
	Lang  string    `yaml:"lang"`
	Note  string    `yaml:"note"`
	Code  string    `yaml:"code"`
}

// CodeLines are the example's code lines. The newline a YAML block scalar ends
// on is not a line of code.
func (e Example) CodeLines() []string {
	return strings.Split(strings.TrimRight(e.Code, "\n"), "\n")
}

// LineRange is the one-based span of code a violation covers. A violation that
// runs over several lines has no single right line to be reported on, so any
// line inside the span counts as the finding landing where it should.
type LineRange struct {
	First int
	Last  int
}

// Set reports whether the example named any lines at all.
func (r LineRange) Set() bool { return r.First > 0 }

// Contains reports whether a line falls inside the span. A range nobody set
// holds no line, not even the zero one a question that was never asked
// answers with.
func (r LineRange) Contains(line int) bool {
	return r.Set() && line >= r.First && line <= r.Last
}

// UnmarshalYAML reads `lines: 12` as well as `lines: [12, 14]`, because most
// violations are one line and writing a pair for every one of them is noise.
func (r *LineRange) UnmarshalYAML(node *yaml.Node) error {
	var one int
	if err := node.Decode(&one); err == nil {
		*r = LineRange{First: one, Last: one}
		return r.check()
	}
	var pair []int
	if err := node.Decode(&pair); err != nil {
		return fmt.Errorf("lines: must be a line or a pair of lines: %w", err)
	}
	switch len(pair) {
	case 1:
		*r = LineRange{First: pair[0], Last: pair[0]}
	case 2:
		*r = LineRange{First: pair[0], Last: pair[1]}
	default:
		return fmt.Errorf("lines: must hold one or two lines, got %d", len(pair))
	}
	return r.check()
}

func (r LineRange) check() error {
	if r.First < 1 {
		return fmt.Errorf("lines: a line is counted from 1, got %d", r.First)
	}
	if r.Last < r.First {
		return fmt.Errorf("lines: %d to %d ends before it starts", r.First, r.Last)
	}
	return nil
}

// Criteria describe to the model what a true and a false verdict mean.
type Criteria struct {
	True  string `yaml:"true"`
	False string `yaml:"false"`
}

// Tenet is one rule. Source is where init read the rule from, as the file
// followed by the sentence that file wrote; nothing judges with it, it is
// there so a reader can go back to the sentence the tenet was drafted from and
// so sync can tell a reworded rule from a deleted one.
type Tenet struct {
	ID       string    `yaml:"id"`
	Tenet    string    `yaml:"tenet"`
	Criteria *Criteria `yaml:"criteria"`
	Source   string    `yaml:"source"`
	Fail     *float64  `yaml:"fail"`
	Include  []string  `yaml:"include"`
	Exclude  []string  `yaml:"exclude"`
	Kind     []string  `yaml:"kind"`
	Tags     []string  `yaml:"tags"`

	Examples     []Example `yaml:"examples"`
	ExamplesFrom string    `yaml:"examples_from"`

	// ExamplesPath is the file the examples beside the config were read from,
	// empty for a tenet whose examples are all inline or built in.
	ExamplesPath string `yaml:"-"`

	// Origin is where the tenet reached the resolved config from: the name of
	// a preset, OriginRules or OriginLocal.
	Origin string `yaml:"-"`

	model string
}

// The origins a tenet that came from no preset carries.
const (
	OriginRules = "rules"
	OriginLocal = "local"
)

// Config is a whole config file. Tenets holds what the file wrote itself until
// the config is resolved, and every tenet that will run afterwards.
type Config struct {
	Version  int                  `yaml:"version"`
	Provider string               `yaml:"provider"`
	Model    string               `yaml:"model"`
	Presets  []string             `yaml:"presets"`
	Rules    []string             `yaml:"rules"`
	Disable  []string             `yaml:"disable"`
	Override map[string]*Override `yaml:"override"`
	Tenets   []*Tenet             `yaml:"tenets"`

	// Path is where the config was read from, for error messages.
	Path string `yaml:"-"`
}

// Override patches the fields of a built-in rule a repository wants
// differently. Anything it leaves out the rule keeps.
type Override struct {
	Fail    *float64 `yaml:"fail"`
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
	Kind    []string `yaml:"kind"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Path = path
	if err := cfg.resolveExamples(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := cfg.resolve(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse validates the bytes of a config file and resolves the presets and
// rules it names into the tenets that will run.
func Parse(data []byte) (*Config, error) {
	cfg, err := parse(data)
	if err != nil {
		return nil, err
	}
	if err := cfg.resolve(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parse(data []byte) (*Config, error) {
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Version != 1 {
		return fmt.Errorf("version: must be 1, got %d", c.Version)
	}
	if err := c.validateProvider(); err != nil {
		return err
	}
	seen := make(map[string]bool, len(c.Tenets))
	for i, t := range c.Tenets {
		if t.ID == "" {
			return fmt.Errorf("tenets[%d]: id is required", i)
		}
		if !idPattern.MatchString(t.ID) {
			return fmt.Errorf("tenet %q: id must match [a-z0-9-]+", t.ID)
		}
		if seen[t.ID] {
			return fmt.Errorf("tenet %q: id is used twice", t.ID)
		}
		seen[t.ID] = true
		if strings.TrimSpace(t.Tenet) == "" {
			return fmt.Errorf("tenet %q: tenet is required", t.ID)
		}
		if err := checkFraction(fmt.Sprintf("tenet %q", t.ID), "fail", t.Fail); err != nil {
			return err
		}
		for _, set := range []struct {
			field    string
			patterns []string
		}{{"include", t.Include}, {"exclude", t.Exclude}} {
			for _, p := range set.patterns {
				if !doublestar.ValidatePattern(p) {
					return fmt.Errorf("tenet %q: %s: %q is not a valid glob", t.ID, set.field, p)
				}
			}
		}
		if err := checkKinds(fmt.Sprintf("tenet %q", t.ID), t.Kind); err != nil {
			return err
		}
		if err := validateExamples(t.ID, t.Kind, t.Examples); err != nil {
			return err
		}
		t.model = c.Model
	}
	return c.validateOverrides()
}

// validateProvider settles the name here rather than where a key is looked
// for, so that a typo is a broken config instead of a missing key.
func (c *Config) validateProvider() error {
	if c.Provider == "" {
		return nil
	}
	p, err := provider.Lookup(c.Provider)
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	if err := p.Available(); err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	return nil
}

func (c *Config) validateOverrides() error {
	for _, id := range sortedKeys(c.Override) {
		o := c.Override[id]
		if o == nil {
			return fmt.Errorf("override %q: at least one field is required", id)
		}
		if err := checkFraction(fmt.Sprintf("override %q", id), "fail", o.Fail); err != nil {
			return err
		}
		for _, set := range []struct {
			field    string
			patterns []string
		}{{"include", o.Include}, {"exclude", o.Exclude}} {
			for _, p := range set.patterns {
				if !doublestar.ValidatePattern(p) {
					return fmt.Errorf("override %q: %s: %q is not a valid glob", id, set.field, p)
				}
			}
		}
		if err := checkKinds(fmt.Sprintf("override %q", id), o.Kind); err != nil {
			return err
		}
	}
	return nil
}

func checkKinds(subject string, kinds []string) error {
	for _, k := range kinds {
		if !source.IsKind(k) {
			return fmt.Errorf("%s: kind: must be one of %s, %s, %s, %s or %s, got %q",
				subject, source.KindCode, source.KindProse, source.KindData, source.KindCommit, source.KindPR, k)
		}
	}
	return nil
}

func validateExamples(id string, kinds []string, examples []Example) error {
	for i, e := range examples {
		switch e.Label {
		case LabelViolation, LabelOK:
		default:
			return fmt.Errorf("tenet %q: examples[%d]: label must be %s or %s, got %q", id, i, LabelViolation, LabelOK, e.Label)
		}
		// A lang naming a kind chooses the framing among the tenet's own, so
		// one it does not declare would be read as a language nothing knows
		// and frame the example under the kind the author was moving it off.
		if source.IsKind(e.Lang) && !slices.Contains(kinds, e.Lang) {
			return fmt.Errorf("tenet %q: examples[%d]: lang %q names a kind this tenet does not declare, so it chooses no framing", id, i, e.Lang)
		}
		if strings.TrimSpace(e.Code) == "" {
			return fmt.Errorf("tenet %q: examples[%d]: code is required", id, i)
		}
		if e.Lines.Set() && e.Lines.Last > len(e.CodeLines()) {
			return fmt.Errorf("tenet %q: examples[%d]: line %d is outside the %d lines of code", id, i, e.Lines.Last, len(e.CodeLines()))
		}
	}
	return nil
}

// resolveExamples appends the examples each tenet keeps in a sibling file,
// named relative to dir, to the ones written inline. The file is
// examples/<id>.yml by convention, and examples_from names another instead.
func (c *Config) resolveExamples(dir string) error {
	for _, t := range c.Tenets {
		named := t.ExamplesFrom != ""
		path := filepath.Join(dir, "examples", t.ID+".yml")
		if named {
			path = filepath.Join(dir, filepath.FromSlash(t.ExamplesFrom))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if !named && errors.Is(err, os.ErrNotExist) {
				continue
			}
			if named {
				return fmt.Errorf("tenet %q: examples_from: %w", t.ID, err)
			}
			return fmt.Errorf("tenet %q: %w", t.ID, err)
		}
		var examples []Example
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&examples); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := validateExamples(t.ID, t.Kind, examples); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		t.Examples = append(t.Examples, examples...)
		t.ExamplesPath = path
	}
	return nil
}

func checkFraction(subject, field string, v *float64) error {
	if v == nil {
		return nil
	}
	if *v <= 0 || *v >= 1 {
		return fmt.Errorf("%s: %s: must be between 0 and 1 exclusive, got %v", subject, field, *v)
	}
	return nil
}

// FailValue is the probability at or above which a verdict becomes a finding,
// and a finding fails the run.
func (t *Tenet) FailValue() float64 {
	if t.Fail == nil {
		return DefaultFail
	}
	return *t.Fail
}

// Applies reports whether the tenet judges a file. The path is slash
// separated and relative to the repository root, which is what the globs in
// the config are written against, whatever directory the lint was started
// from. A kind narrows the globs further rather than widening them: a tenet
// about prose is not asked about the code that happens to match its include.
func (t *Tenet) Applies(relPath string) bool {
	relPath = filepath.ToSlash(relPath)
	if len(t.Kind) > 0 && !slices.Contains(t.Kind, source.Kind(relPath)) {
		return false
	}
	for _, p := range t.Exclude {
		if ok, _ := doublestar.Match(p, relPath); ok {
			return false
		}
	}
	if len(t.Include) == 0 {
		return true
	}
	for _, p := range t.Include {
		if ok, _ := doublestar.Match(p, relPath); ok {
			return true
		}
	}
	return false
}

// Hash identifies what the model is asked, so that a cached answer survives
// everything that does not change the question. The cutoff is left out
// because it is applied to the answer, not asked of the model,
// and examples because they are never shown to it, so adding one must not
// throw away the answers a lint has already paid for.
func (t *Tenet) Hash() string {
	h := t.identity()
	_, _ = io.WriteString(h, t.model)
	_, _ = h.Write([]byte{0})
	return hex.EncodeToString(h.Sum(nil))
}

// IdentityHash identifies the rule itself, which is Hash without the model.
// The two differ over one thing on purpose: a cached answer is only good for
// the model that gave it, while what a baseline accepted is a violation of a
// rule, and upgrading the model must not hand a team its whole backlog again.
func (t *Tenet) IdentityHash() string {
	return hex.EncodeToString(t.identity().Sum(nil))
}

func (t *Tenet) identity() hash.Hash {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			_, _ = io.WriteString(h, p)
			_, _ = h.Write([]byte{0})
		}
	}
	write(t.ID, t.Tenet)
	if t.Criteria != nil {
		write(t.Criteria.True, t.Criteria.False)
	} else {
		write("", "")
	}
	return h
}

// SetModel records the model the tenet will be judged by, which is part of its
// hash.
func (t *Tenet) SetModel(model string) { t.model = model }

// SetModel records the model for every tenet in the config.
func (c *Config) SetModel(model string) {
	c.Model = model
	for _, t := range c.Tenets {
		t.SetModel(model)
	}
}

// Find searches upward from startDir, stopping at the root of the git
// repository so that a config belonging to some enclosing directory is never
// picked up. The directories it searched come back either way, for the error
// message.
func Find(startDir string) (string, []string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", nil, err
	}
	var searched []string
	for {
		searched = append(searched, dir)
		candidate := source.ConfigPath(dir)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, searched, nil
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", searched, fmt.Errorf("%w from %s up to %s; run tenet init to draft one",
		ErrNotFound, searched[0], searched[len(searched)-1])
}
