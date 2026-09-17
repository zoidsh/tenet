package tenets_test

import (
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/tenets"
)

// resolved is the ids of the tenets that will run, each with where it came
// from, which is the whole of what resolution decides.
func resolved(t *testing.T, yaml string) []string {
	t.Helper()
	cfg, err := tenets.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, tenet := range cfg.Tenets {
		out = append(out, tenet.ID+" from "+tenet.Origin)
	}
	return out
}

func TestResolutionOrder(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []string
	}{
		{
			"a preset in the order it lists its rules",
			"version: 1\npresets: [agent-hygiene]\n",
			[]string{
				"comment-why from agent-hygiene",
				"no-fallback from agent-hygiene",
				"no-mocking from agent-hygiene",
				"no-defensive-nil from agent-hygiene",
			},
		},
		{
			"a rule on its own",
			"version: 1\nrules: [no-mocking]\n",
			[]string{"no-mocking from rules"},
		},
		{
			"rules come after presets",
			"version: 1\npresets: [agent-hygiene]\nrules: []\ndisable: [comment-why, no-fallback, no-defensive-nil]\n",
			[]string{"no-mocking from agent-hygiene"},
		},
		{
			"a local tenet comes last",
			"version: 1\nrules: [no-mocking]\ntenets:\n  - id: house-style\n    tenet: x\n",
			[]string{"no-mocking from rules", "house-style from local"},
		},
		{
			"a local tenet replaces the built-in where it stood",
			"version: 1\npresets: [agent-hygiene]\ntenets:\n  - id: no-fallback\n    tenet: mine\n",
			[]string{
				"comment-why from agent-hygiene",
				"no-fallback from local",
				"no-mocking from agent-hygiene",
				"no-defensive-nil from agent-hygiene",
			},
		},
		{
			"disable removes what the expansion added",
			"version: 1\npresets: [agent-hygiene]\ndisable: [no-mocking, comment-why]\n",
			[]string{"no-fallback from agent-hygiene", "no-defensive-nil from agent-hygiene"},
		},
		{
			"disable removes a local tenet too",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n  - id: b\n    tenet: y\ndisable: [a]\n",
			[]string{"b from local"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolved(t, c.yaml)
			if strings.Join(got, ", ") != strings.Join(c.want, ", ") {
				t.Errorf("resolved to %v, want %v", got, c.want)
			}
		})
	}
}

func TestLocalTenetReplacesTheBuiltinWholesale(t *testing.T) {
	cfg, err := tenets.Parse([]byte("version: 1\npresets: [agent-hygiene]\ntenets:\n  - id: no-mocking\n    tenet: Mock what you like.\n"))
	if err != nil {
		t.Fatal(err)
	}
	var got *tenets.Tenet
	for _, tenet := range cfg.Tenets {
		if tenet.ID == "no-mocking" {
			got = tenet
		}
	}
	if got.Tenet != "Mock what you like." {
		t.Errorf("tenet is %q", got.Tenet)
	}
	if got.Criteria != nil || len(got.Examples) != 0 || len(got.Include) != 0 {
		t.Errorf("the built-in's criteria, examples or includes survived: %#v", got)
	}
}

func TestOverrideTouchesOnlyWhatItNames(t *testing.T) {
	builtin, err := tenets.BuiltinRule("comment-why")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := tenets.Parse([]byte(`version: 1
rules: [comment-why]
override:
  comment-why:
    fail: 0.6
    exclude: ["**/testdata/**"]
`))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Tenets[0]
	if got.FailValue() != 0.6 {
		t.Errorf("the override did not take: %#v", got)
	}
	if len(got.Exclude) != 1 || got.Exclude[0] != "**/testdata/**" {
		t.Errorf("exclude is %v", got.Exclude)
	}
	if got.Tenet != builtin.Tenet.Tenet || got.Criteria == nil {
		t.Errorf("the override touched a field it does not name: %#v", got)
	}
	if len(got.Include) != len(builtin.Tenet.Include) || got.Include[0] != builtin.Tenet.Include[0] {
		t.Errorf("include is %v", got.Include)
	}
	if len(got.Examples) != len(builtin.Tenet.Examples) {
		t.Errorf("the override lost the examples: %d of %d", len(got.Examples), len(builtin.Tenet.Examples))
	}
}

// An override is applied after the disable, so it has nothing left to patch
// and says so rather than passing silently.
func TestOverrideOfADisabledTenet(t *testing.T) {
	_, err := tenets.Parse([]byte("version: 1\npresets: [agent-hygiene]\ndisable: [no-mocking]\noverride:\n  no-mocking:\n    fail: 0.6\n"))
	if err == nil || !strings.Contains(err.Error(), "override") || !strings.Contains(err.Error(), "no-mocking") {
		t.Fatalf("error is %v", err)
	}
}

func TestResolutionErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []string
	}{
		{
			"a rule that a preset already brought",
			"version: 1\npresets: [agent-hygiene]\nrules: [no-mocking]\n",
			[]string{"no-mocking", "agent-hygiene"},
		},
		{
			"a rule named twice",
			"version: 1\nrules: [no-mocking, no-mocking]\n",
			[]string{"no-mocking", "rules"},
		},
		{"an unknown preset", "version: 1\npresets: [house]\n", []string{"presets", "house"}},
		{"an unknown rule", "version: 1\nrules: [house]\n", []string{"rules", "house"}},
		{"an unknown disable", "version: 1\nrules: [no-mocking]\ndisable: [house]\n", []string{"disable", "house"}},
		{"an unknown override", "version: 1\nrules: [no-mocking]\noverride:\n  house:\n    fail: 0.6\n", []string{"override", "house"}},
		{"a config that resolves to nothing", "version: 1\nrules: [no-mocking]\ndisable: [no-mocking]\n", []string{"at least one"}},
		{"an override with a bad cutoff", "version: 1\nrules: [no-mocking]\noverride:\n  no-mocking:\n    fail: 2\n", []string{`override "no-mocking"`, "fail"}},
		{"an override with a bad glob", "version: 1\nrules: [no-mocking]\noverride:\n  no-mocking:\n    include: [\"[\"]\n", []string{`override "no-mocking"`, "include"}},
		{"an override field nobody knows", "version: 1\nrules: [no-mocking]\noverride:\n  no-mocking:\n    tenet: x\n", []string{"tenet"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := tenets.Parse([]byte(c.yaml))
			if err == nil {
				t.Fatal("want an error")
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// A rule two presets both include is what makes presets composable, so it
// arrives once rather than as a clash.
func TestARuleInSeveralPresets(t *testing.T) {
	got := resolved(t, "version: 1\npresets: [agent-hygiene, agent-hygiene]\n")
	if len(got) != 4 {
		t.Errorf("resolved to %v", got)
	}
}
