package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/report"
	"github.com/zoidsh/tenet/internal/source"
	"github.com/zoidsh/tenet/internal/tenets"
)

// TestMain asks for the format a person gets. These tests capture the output
// in a buffer, which is no terminal, so the default would otherwise be the
// JSON an agent reads and every text assertion here would be about the wrong
// thing.
func TestMain(m *testing.M) {
	if err := os.Setenv(report.FormatEnv, report.FormatText); err != nil {
		panic(err)
	}
	// A test run inside GitHub Actions would otherwise append every --format
	// github report it makes to the job's real summary.
	if err := os.Unsetenv(report.StepSummaryEnv); err != nil {
		panic(err)
	}
	// The config home is moved somewhere empty so that a key saved on the
	// machine running the tests is not the key a test that saved none finds.
	empty, err := os.MkdirTemp("", "tenet-config")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", empty); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(empty)
	os.Exit(code)
}

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
	code, stdout, stderr := runCmd(t, "config", "--config", tenets.FileName)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	golden(t, "config.txt", stdout)
}

func TestConfigListingShowsTheExamplesFile(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "version: 1\ntenets:\n  - id: comment-why\n    tenet: A comment says why.\n")
	writeFile(t, dir, filepath.Join(source.ConfigDir, "examples", "comment-why.yml"), "- label: ok\n  code: \"x = 1\"\n")
	t.Chdir(dir)

	code, stdout, stderr := runCmd(t, "config")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "examples/comment-why.yml") {
		t.Errorf("listing does not name the examples file:\n%s", stdout)
	}
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
	code, _, stderr := runCmd(t, "check", "--builtin", "--config", tenets.FileName)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "--config") {
		t.Errorf("stderr is %q", stderr)
	}
}

func TestVersionFlagMatchesTheSubcommand(t *testing.T) {
	code, flagged, stderr := runCmd(t, "--version")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	code, sub, stderr := runCmd(t, "version")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if flagged != sub || strings.TrimSpace(flagged) == "" {
		t.Errorf("--version printed %q and version printed %q", flagged, sub)
	}
}

// Every subcommand answers --help with its own help rather than the root's.
func TestSubcommandHelpIsItsOwn(t *testing.T) {
	for _, c := range []struct{ args, want []string }{
		{[]string{"hook", "install", "--help"}, []string{"tenet hook install", "--force"}},
		{[]string{"hook", "uninstall", "--help"}, []string{"tenet hook uninstall"}},
		{[]string{"init", "--help"}, []string{"tenet init", "--agent"}},
		{[]string{"baseline", "--help"}, []string{"tenet baseline", "--prune"}},
	} {
		code, stdout, stderr := runCmd(t, c.args...)
		if code != 0 {
			t.Fatalf("%v exited %d: %s", c.args, code, stderr)
		}
		for _, want := range c.want {
			if !strings.Contains(stdout, want) {
				t.Errorf("%v printed %q, which does not mention %s", c.args, stdout, want)
			}
		}
	}
}
