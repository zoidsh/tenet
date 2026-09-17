package tenets_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/tenets"
)

const sample = `
version: 1
model: jev-1.13.0
tenets:
  - id: comment-why
    tenet: A comment says why.
    criteria:
      true: It restates the code.
      false: It gives a reason.
    severity: error
    threshold: 0.6
    confident: 0.8
    include: ["**/*.go"]
    exclude: ["**/vendor/**"]
  - id: no-mocking
    tenet: Never mock.
`

func TestParseDefaults(t *testing.T) {
	cfg, err := tenets.Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tenets) != 2 {
		t.Fatalf("got %d tenets", len(cfg.Tenets))
	}
	first, second := cfg.Tenets[0], cfg.Tenets[1]
	if first.Criteria == nil || first.Criteria.True != "It restates the code." || first.Criteria.False != "It gives a reason." {
		t.Fatalf("criteria not decoded: %#v", first.Criteria)
	}
	if first.Severity != tenets.SeverityError || first.ThresholdValue() != 0.6 || first.ConfidentValue() != 0.8 {
		t.Errorf("explicit fields lost: %#v", first)
	}
	if second.Severity != tenets.SeverityWarn || second.ThresholdValue() != 0.5 || second.ConfidentValue() != 0.7 {
		t.Errorf("defaults wrong: %#v", second)
	}
	if second.Criteria != nil {
		t.Errorf("criteria should be absent")
	}
}

func TestApplies(t *testing.T) {
	cfg, err := tenets.Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	scoped, unscoped := cfg.Tenets[0], cfg.Tenets[1]
	cases := []struct {
		tenet *tenets.Tenet
		path  string
		want  bool
	}{
		{scoped, "internal/judge/judge.go", true},
		{scoped, "main.go", true},
		{scoped, "README.md", false},
		{scoped, "vendor/x/y.go", false},
		{unscoped, "README.md", true},
	}
	for _, c := range cases {
		if got := c.tenet.Applies(c.path); got != c.want {
			t.Errorf("%s applies to %s = %v, want %v", c.tenet.ID, c.path, got, c.want)
		}
	}
}

func TestHashIgnoresThresholdAndSeverity(t *testing.T) {
	cfg, err := tenets.Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	base := cfg.Tenets[0].Hash()

	raised := strings.Replace(sample, "threshold: 0.6", "threshold: 0.9", 1)
	raised = strings.Replace(raised, "severity: error", "severity: info", 1)
	other, err := tenets.Parse([]byte(raised))
	if err != nil {
		t.Fatal(err)
	}
	if other.Tenets[0].Hash() != base {
		t.Error("threshold or severity changed the hash")
	}

	reworded, err := tenets.Parse([]byte(strings.Replace(sample, "A comment says why.", "A comment says why not.", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if reworded.Tenets[0].Hash() == base {
		t.Error("rewording the tenet left the hash alone")
	}

	remodelled, err := tenets.Parse([]byte(strings.Replace(sample, "jev-1.13.0", "jev-1.14.0", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if remodelled.Tenets[0].Hash() == base {
		t.Error("changing the model left the hash alone")
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"version", "version: 2\ntenets:\n  - id: a\n    tenet: x\n", "version"},
		{"no tenets", "version: 1\ntenets: []\n", "at least one"},
		{"missing id", "version: 1\ntenets:\n  - tenet: x\n", "id is required"},
		{"bad id", "version: 1\ntenets:\n  - id: Comment_Why\n    tenet: x\n", `"Comment_Why": id`},
		{"duplicate id", "version: 1\ntenets:\n  - id: a\n    tenet: x\n  - id: a\n    tenet: y\n", "used twice"},
		{"missing text", "version: 1\ntenets:\n  - id: a\n    tenet: \"  \"\n", `"a": tenet is required`},
		{"bad severity", "version: 1\ntenets:\n  - id: a\n    tenet: x\n    severity: loud\n", `"a": severity`},
		{"bad threshold", "version: 1\ntenets:\n  - id: a\n    tenet: x\n    threshold: 1.5\n", `"a": threshold`},
		{"bad confident", "version: 1\ntenets:\n  - id: a\n    tenet: x\n    confident: 0\n", `"a": confident`},
		{"bad glob", "version: 1\ntenets:\n  - id: a\n    tenet: x\n    include: [\"[\"]\n", `"a": include`},
		{"unknown field", "version: 1\ntenets:\n  - id: a\n    tenet: x\n    weight: 3\n", "weight"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := tenets.Parse([]byte(c.yaml))
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	nested := filepath.Join(root, "a", "b")
	mustMkdir(t, nested)
	want := filepath.Join(root, tenets.FileName)
	if err := os.WriteFile(want, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}

	got, _, err := tenets.Find(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("found %s, want %s", got, want)
	}

	if _, err := tenets.Load(got); err != nil {
		t.Fatal(err)
	}
}

func TestFindStopsAtGitRoot(t *testing.T) {
	outer := t.TempDir()
	if err := os.WriteFile(filepath.Join(outer, tenets.FileName), []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(outer, "repo")
	mustMkdir(t, filepath.Join(repo, ".git"))

	_, searched, err := tenets.Find(repo)
	if err == nil {
		t.Fatal("want an error, the config is above the git root")
	}
	if len(searched) != 1 {
		t.Errorf("searched %v, want only the repo root", searched)
	}
	if !strings.Contains(err.Error(), repo) {
		t.Errorf("error %q does not name the search path", err)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
}
