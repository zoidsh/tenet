package tenets

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	corpus "github.com/zoidsh/tenetlint"
)

// Where the corpus sits inside the embedded file system.
const (
	rulesDir   = "rules"
	presetsDir = "presets"

	ruleFile     = "rule.yml"
	examplesFile = "examples.yml"
	docFile      = "README.md"
)

// StandaloneTag marks a rule that belongs in no preset, so that one left out
// of every preset is a decision rather than an oversight.
const StandaloneTag = "standalone"

// Rule is one built-in rule as it ships: the tenet it holds, the text of the
// file it is written in, the paragraph beside it, and the presets that
// include it.
type Rule struct {
	ID      string
	Tenet   *Tenet
	YAML    string
	Doc     string
	Presets []string
}

// Preset is a named list of rule ids. A rule may appear in several presets.
type Preset struct {
	Version     int      `yaml:"version"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Rules       []string `yaml:"rules"`
}

// BuiltinRules are every rule in the corpus, ordered by id.
func BuiltinRules() ([]*Rule, error) {
	c, err := readCorpus()
	if err != nil {
		return nil, err
	}
	rules := make([]*Rule, 0, len(c.ruleIDs))
	for _, id := range c.ruleIDs {
		rule, err := c.rule(id)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// BuiltinRule is the rule with this id.
func BuiltinRule(id string) (*Rule, error) {
	c, err := readCorpus()
	if err != nil {
		return nil, err
	}
	if _, ok := c.rules[id]; !ok {
		return nil, fmt.Errorf("no built-in rule %q; run tenet rules to see them", id)
	}
	return c.rule(id)
}

// BuiltinPresets are every preset in the corpus, ordered by name.
func BuiltinPresets() ([]*Preset, error) {
	c, err := readCorpus()
	if err != nil {
		return nil, err
	}
	return c.presets, nil
}

// BuiltinPreset is the preset with this name.
func BuiltinPreset(name string) (*Preset, error) {
	c, err := readCorpus()
	if err != nil {
		return nil, err
	}
	for _, p := range c.presets {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no built-in preset %q; run tenet presets to see them", name)
}

// files is a rule's directory, held as it ships so that every caller parses
// its own tenet: a resolved config edits the tenets it was given, and two
// configs in one process must not be the same pointers.
type files struct {
	rule     []byte
	examples []byte
	doc      string
	presets  []string
}

type readCorpusResult struct {
	rules   map[string]*files
	ruleIDs []string
	presets []*Preset
}

func (c *readCorpusResult) rule(id string) (*Rule, error) {
	f := c.rules[id]
	tenet, err := parseRule(id, f.rule, f.examples)
	if err != nil {
		return nil, err
	}
	return &Rule{ID: id, Tenet: tenet, YAML: string(f.rule), Doc: f.doc, Presets: f.presets}, nil
}

var readCorpus = sync.OnceValues(func() (*readCorpusResult, error) {
	c := &readCorpusResult{rules: map[string]*files{}}
	entries, err := fs.ReadDir(corpus.Corpus, rulesDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		f, err := readRuleFiles(id)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path.Join(rulesDir, id), err)
		}
		c.rules[id] = f
		c.ruleIDs = append(c.ruleIDs, id)
	}
	sort.Strings(c.ruleIDs)

	presets, err := fs.ReadDir(corpus.Corpus, presetsDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range presets {
		name := strings.TrimSuffix(entry.Name(), ".yml")
		p, err := readPreset(name, entry.Name(), c.rules)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path.Join(presetsDir, entry.Name()), err)
		}
		c.presets = append(c.presets, p)
		for _, id := range p.Rules {
			c.rules[id].presets = append(c.rules[id].presets, p.Name)
		}
	}
	sort.Slice(c.presets, func(i, j int) bool { return c.presets[i].Name < c.presets[j].Name })
	for _, f := range c.rules {
		sort.Strings(f.presets)
	}
	return c, nil
})

func readRuleFiles(id string) (*files, error) {
	rule, err := corpus.Corpus.ReadFile(path.Join(rulesDir, id, ruleFile))
	if err != nil {
		return nil, err
	}
	examples, err := corpus.Corpus.ReadFile(path.Join(rulesDir, id, examplesFile))
	if err != nil {
		return nil, err
	}
	f := &files{rule: rule, examples: examples}
	if doc, err := corpus.Corpus.ReadFile(path.Join(rulesDir, id, docFile)); err == nil {
		f.doc = strings.TrimSpace(string(doc))
	}
	if _, err := parseRule(id, rule, examples); err != nil {
		return nil, err
	}
	return f, nil
}

func parseRule(id string, rule, examples []byte) (*Tenet, error) {
	var t Tenet
	decoder := yaml.NewDecoder(bytes.NewReader(rule))
	decoder.KnownFields(true)
	if err := decoder.Decode(&t); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", ruleFile, err)
	}
	if t.ExamplesFrom != "" {
		return nil, fmt.Errorf("%s: examples_from is not a rule field; the examples are the %s beside it", ruleFile, examplesFile)
	}
	if t.ID != id {
		return nil, fmt.Errorf("%s: id is %q, which is not the directory it ships in", ruleFile, t.ID)
	}
	decoder = yaml.NewDecoder(bytes.NewReader(examples))
	decoder.KnownFields(true)
	if err := decoder.Decode(&t.Examples); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", examplesFile, err)
	}
	if err := (&Config{Version: 1, Tenets: []*Tenet{&t}}).validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

func readPreset(name, file string, rules map[string]*files) (*Preset, error) {
	data, err := corpus.Corpus.ReadFile(path.Join(presetsDir, file))
	if err != nil {
		return nil, err
	}
	var p Preset
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&p); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if p.Version != 1 {
		return nil, fmt.Errorf("version: must be 1, got %d", p.Version)
	}
	if p.Name != name {
		return nil, fmt.Errorf("name is %q, which is not the file it ships as", p.Name)
	}
	if strings.TrimSpace(p.Description) == "" {
		return nil, errors.New("description is required")
	}
	if len(p.Rules) == 0 {
		return nil, errors.New("rules: a preset names at least one rule")
	}
	seen := map[string]bool{}
	for _, id := range p.Rules {
		if _, ok := rules[id]; !ok {
			return nil, fmt.Errorf("rules: no built-in rule %q", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("rules: %q is named twice", id)
		}
		seen[id] = true
	}
	return &p, nil
}
