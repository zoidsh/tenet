// Package tenets loads the rules tenetlint judges code against.
package tenets

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// FileName is the config file Find looks for.
const FileName = "tenets.yml"

// Defaults applied to a tenet that leaves the field out.
const (
	DefaultThreshold = 0.5
	DefaultConfident = 0.7
	DefaultSeverity  = SeverityWarn
)

// Severity is how loudly a tenet's findings are reported.
type Severity string

// The severities a tenet may carry, ordered by Rank.
const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// Rank orders severities so that a --fail-on threshold is a comparison.
func (s Severity) Rank() int {
	switch s {
	case SeverityInfo:
		return 1
	case SeverityWarn:
		return 2
	case SeverityError:
		return 3
	default:
		return 0
	}
}

// ParseSeverity reads a severity name.
func ParseSeverity(s string) (Severity, error) {
	switch Severity(s) {
	case SeverityInfo, SeverityWarn, SeverityError:
		return Severity(s), nil
	default:
		return "", fmt.Errorf("severity must be error, warn or info, got %q", s)
	}
}

// Criteria describe to the model what a true and a false verdict mean.
type Criteria struct {
	True  string `yaml:"true"`
	False string `yaml:"false"`
}

// Tenet is one rule. Source is where init read the rule from, as file:line;
// nothing judges with it, it is there so a reader can go back to the sentence
// the tenet was drafted from.
type Tenet struct {
	ID        string    `yaml:"id"`
	Tenet     string    `yaml:"tenet"`
	Criteria  *Criteria `yaml:"criteria"`
	Source    string    `yaml:"source"`
	Severity  Severity  `yaml:"severity"`
	Threshold *float64  `yaml:"threshold"`
	Confident *float64  `yaml:"confident"`
	Include   []string  `yaml:"include"`
	Exclude   []string  `yaml:"exclude"`

	model string
}

// Config is a whole tenets.yml.
type Config struct {
	Version int      `yaml:"version"`
	Model   string   `yaml:"model"`
	Tenets  []*Tenet `yaml:"tenets"`

	// Path is where the config was read from, for error messages.
	Path string `yaml:"-"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// Load reads and validates a tenets.yml.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Path = path
	return cfg, nil
}

// Parse validates the bytes of a tenets.yml.
func Parse(data []byte) (*Config, error) {
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
	if len(c.Tenets) == 0 {
		return errors.New("tenets: at least one tenet is required")
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
		if t.Severity == "" {
			t.Severity = DefaultSeverity
		} else if _, err := ParseSeverity(string(t.Severity)); err != nil {
			return fmt.Errorf("tenet %q: %w", t.ID, err)
		}
		if err := checkFraction(t.ID, "threshold", t.Threshold); err != nil {
			return err
		}
		if err := checkFraction(t.ID, "confident", t.Confident); err != nil {
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
		t.model = c.Model
	}
	return nil
}

func checkFraction(id, field string, v *float64) error {
	if v == nil {
		return nil
	}
	if *v <= 0 || *v >= 1 {
		return fmt.Errorf("tenet %q: %s: must be between 0 and 1 exclusive, got %v", id, field, *v)
	}
	return nil
}

// ThresholdValue is the probability at which a verdict becomes a finding.
func (t *Tenet) ThresholdValue() float64 {
	if t.Threshold == nil {
		return DefaultThreshold
	}
	return *t.Threshold
}

// ConfidentValue is the probability below which a finding is reported as low
// confidence.
func (t *Tenet) ConfidentValue() float64 {
	if t.Confident == nil {
		return DefaultConfident
	}
	return *t.Confident
}

// Applies reports whether the tenet judges a file. The path is slash
// separated and relative to the repository root, which is what the globs in
// the config are written against, whatever directory the lint was started
// from.
func (t *Tenet) Applies(relPath string) bool {
	relPath = filepath.ToSlash(relPath)
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
// everything that does not change the question. Threshold and severity are
// left out because they are applied to the answer, not asked of the model.
func (t *Tenet) Hash() string {
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
	write(t.model)
	return hex.EncodeToString(h.Sum(nil))
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
		candidate := filepath.Join(dir, FileName)
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
	return "", searched, fmt.Errorf("no %s found in %s", FileName, strings.Join(searched, ", "))
}
