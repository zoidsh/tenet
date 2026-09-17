package tenets_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/source"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

const sample = `
version: 1
model: jev-1.13.0
tenets:
  - id: comment-why
    tenet: A comment says why.
    source: CLAUDE.md:42
    criteria:
      true: It restates the code.
      false: It gives a reason.
    fail: 0.6
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
	if first.Source != "CLAUDE.md:42" || second.Source != "" {
		t.Errorf("source not decoded: %q, %q", first.Source, second.Source)
	}
	if first.FailValue() != 0.6 {
		t.Errorf("explicit fields lost: %#v", first)
	}
	if second.FailValue() != tenets.DefaultFail {
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

func TestAppliesNarrowedByKind(t *testing.T) {
	cfg, err := tenets.Parse([]byte(`
version: 1
tenets:
  - id: plain-english
    tenet: Write plainly.
    kind: [prose]
  - id: no-secrets
    tenet: Keep secrets out.
    kind: [prose, data]
    exclude: ["**/testdata/**"]
`))
	if err != nil {
		t.Fatal(err)
	}
	prose, both := cfg.Tenets[0], cfg.Tenets[1]
	cases := []struct {
		tenet *tenets.Tenet
		path  string
		want  bool
	}{
		{prose, "README.md", true},
		{prose, "docs/design.txt", true},
		{prose, "main.go", false},
		{prose, "tenets.yml", false},
		{both, "tenets.yml", true},
		{both, "CHANGELOG", true},
		{both, "main.go", false},
		{both, "internal/testdata/notes.md", false},
	}
	for _, c := range cases {
		if got := c.tenet.Applies(c.path); got != c.want {
			t.Errorf("%s applies to %s = %v, want %v", c.tenet.ID, c.path, got, c.want)
		}
	}
}

func TestAppliesToACommitMessage(t *testing.T) {
	cfg, err := tenets.Parse([]byte(`
version: 1
tenets:
  - id: subject-imperative
    tenet: Write the subject in the imperative.
    kind: [commit]
  - id: by-name
    tenet: Write the subject in the imperative.
    include: ["COMMIT_EDITMSG"]
  - id: about-code
    tenet: A comment says why.
    kind: [code]
    include: ["COMMIT_EDITMSG"]
`))
	if err != nil {
		t.Fatal(err)
	}
	byKind, byName, code := cfg.Tenets[0], cfg.Tenets[1], cfg.Tenets[2]
	cases := []struct {
		tenet *tenets.Tenet
		path  string
		want  bool
	}{
		{byKind, source.CommitMsgPath, true},
		{byKind, "main.go", false},
		{byKind, "README.md", false},
		{byName, source.CommitMsgPath, true},
		{code, source.CommitMsgPath, false},
	}
	for _, c := range cases {
		if got := c.tenet.Applies(c.path); got != c.want {
			t.Errorf("%s applies to %s = %v, want %v", c.tenet.ID, c.path, got, c.want)
		}
	}
}

func TestAppliesToPullRequestText(t *testing.T) {
	cfg, err := tenets.Parse([]byte(`
version: 1
tenets:
  - id: title-for-a-reader
    tenet: The title says what changed for a reader.
    kind: [pr]
  - id: by-name
    tenet: The title says what changed for a reader.
    include: ["PULL_REQUEST"]
  - id: either-text
    tenet: The subject or title says what changed for a reader.
    kind: [commit, pr]
`))
	if err != nil {
		t.Fatal(err)
	}
	byKind, byName, both := cfg.Tenets[0], cfg.Tenets[1], cfg.Tenets[2]
	cases := []struct {
		tenet *tenets.Tenet
		path  string
		want  bool
	}{
		{byKind, source.PRTextPath, true},
		{byKind, source.CommitMsgPath, false},
		{byKind, "main.go", false},
		{byName, source.PRTextPath, true},
		{both, source.PRTextPath, true},
		{both, source.CommitMsgPath, true},
		{both, "README.md", false},
	}
	for _, c := range cases {
		if got := c.tenet.Applies(c.path); got != c.want {
			t.Errorf("%s applies to %s = %v, want %v", c.tenet.ID, c.path, got, c.want)
		}
	}
}

func TestHashCoversOnlyWhatIsAsked(t *testing.T) {
	cfg, err := tenets.Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	base := cfg.Tenets[0].Hash()

	raised := strings.Replace(sample, "fail: 0.6", "fail: 0.9", 1)
	other, err := tenets.Parse([]byte(raised))
	if err != nil {
		t.Fatal(err)
	}
	if other.Tenets[0].Hash() != base {
		t.Error("the cutoff changed the hash")
	}

	reworded, err := tenets.Parse([]byte(strings.Replace(sample, "A comment says why.", "A comment says why not.", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if reworded.Tenets[0].Hash() == base {
		t.Error("rewording the tenet left the hash alone")
	}

	moved, err := tenets.Parse([]byte(strings.Replace(sample, "CLAUDE.md:42", "AGENTS.md:7", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if moved.Tenets[0].Hash() != base {
		t.Error("the source line changed the hash")
	}

	remodelled, err := tenets.Parse([]byte(strings.Replace(sample, "jev-1.13.0", "jev-1.14.0", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if remodelled.Tenets[0].Hash() == base {
		t.Error("changing the model left the hash alone")
	}
}

func TestIdentityHashIsTheRuleWithoutTheModel(t *testing.T) {
	cfg, err := tenets.Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	base := cfg.Tenets[0].IdentityHash()

	remodelled, err := tenets.Parse([]byte(strings.Replace(sample, "jev-1.13.0", "jev-1.14.0", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if remodelled.Tenets[0].IdentityHash() != base {
		t.Error("changing the model changed the identity hash")
	}

	reworded, err := tenets.Parse([]byte(strings.Replace(sample, "A comment says why.", "A comment says why not.", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if reworded.Tenets[0].IdentityHash() == base {
		t.Error("rewording the tenet left the identity hash alone")
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
		{"bad fail", "version: 1\ntenets:\n  - id: a\n    tenet: x\n    fail: 1.5\n", `"a": fail`},
		{"a fail of zero", "version: 1\ntenets:\n  - id: a\n    tenet: x\n    fail: 0\n", `"a": fail`},
		{"bad glob", "version: 1\ntenets:\n  - id: a\n    tenet: x\n    include: [\"[\"]\n", `"a": include`},
		{"unknown field", "version: 1\ntenets:\n  - id: a\n    tenet: x\n    weight: 3\n", "weight"},
		{
			"a lang naming an undeclared kind",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    kind: [commit, pr]\n    examples:\n      - label: ok\n        lang: prose\n        code: \"x\"\n",
			`lang "prose" names a kind this tenet does not declare`,
		},
		{
			"a lang naming a kind where none are declared",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: ok\n        lang: pr\n        code: \"x\"\n",
			`lang "pr" names a kind this tenet does not declare`,
		},
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

// The error is the first thing a newcomer with no config sees, so it says
// where it looked and what to run.
func TestFindNamesTheSearchAndTheWayOut(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	nested := filepath.Join(root, "a", "b")
	mustMkdir(t, nested)

	_, _, err := tenets.Find(nested)
	if err == nil {
		t.Fatal("want an error, there is no config")
	}
	want := "no " + tenets.FileName + " found from " + nested + " up to " + root + "; run tenet init to draft one"
	if err.Error() != want {
		t.Errorf("error is %q, want %q", err, want)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
}
