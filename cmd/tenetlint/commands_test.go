package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

// goldenDir is settled before any test moves the working directory, which the
// config listing has to do to read this repository's own config.
var goldenDir = func() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Join(dir, "testdata", "golden")
}()

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output is\n%s\nwant\n%s\nUPDATE_GOLDEN=1 settles it", got, want)
	}
}

func TestRulesListing(t *testing.T) {
	code, stdout, stderr := runCmd(t, "rules")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	golden(t, "rules.txt", stdout)
}

func TestPresetsListing(t *testing.T) {
	code, stdout, stderr := runCmd(t, "presets")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	golden(t, "presets.txt", stdout)
}

// The resolved config is this repository's own, which is the one a reader of
// the golden can go and look at.
func TestConfigListing(t *testing.T) {
	t.Chdir("../..")
	code, stdout, stderr := runCmd(t, "config", "--config", "tenets.yml")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	golden(t, "config.txt", stdout)
}

func TestRuleShown(t *testing.T) {
	code, stdout, stderr := runCmd(t, "rules", "no-mocking")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	rule, err := tenets.BuiltinRule("no-mocking")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"rules/no-mocking/rule.yml",
		rule.YAML,
		rule.Doc,
		exampleCount(rule.Tenet.Examples),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not hold %q:\n%s", want, stdout)
		}
	}
}

func TestUnknownRuleShown(t *testing.T) {
	code, _, stderr := runCmd(t, "rules", "no-such-rule")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "no-such-rule") {
		t.Errorf("stderr is %q", stderr)
	}
}

func TestCheckBuiltinEndToEnd(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, exampleServer(t).URL)

	code, stdout, stderr := runCmd(t, "check", "--builtin", "--format", "json", "--no-cache")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	var got jsonCheck
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout)
	}
	rules, err := tenets.BuiltinRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tenets) != len(rules) {
		t.Fatalf("measured %d of %d rules", len(got.Tenets), len(rules))
	}
	var examples int
	for i, rule := range rules {
		if got.Tenets[i].Tenet != rule.ID {
			t.Errorf("row %d is %q, want %q", i, got.Tenets[i].Tenet, rule.ID)
		}
		examples += len(rule.Tenet.Examples)
	}
	if got.Stats.Examples != examples {
		t.Errorf("judged %d of %d examples", got.Stats.Examples, examples)
	}
}

// --builtin is the corpus, so there is nothing for a config to say about it.
func TestCheckBuiltinRefusesAConfig(t *testing.T) {
	code, _, stderr := runCmd(t, "check", "--builtin", "--config", "tenets.yml")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "--config") {
		t.Errorf("stderr is %q", stderr)
	}
}
