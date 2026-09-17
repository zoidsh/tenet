package importer_test

import (
	"os"
	"reflect"
	"testing"

	"github.com/zoidsh/tenet/internal/importer"
	"github.com/zoidsh/tenet/internal/tenets"
)

func TestSlug(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"A comment says why the code exists, not what it does.", "comment-says-why-code-exists"},
		{"Never mock anything in tests.", "never-mock-anything-tests"},
		{"Do not add `fallbacks` or silent degradation paths.", "do-not-add-fallbacks-silent"},
		{"The it is of a to be.", "tenet"},
	}
	taken := map[string]bool{}
	for _, c := range cases {
		if got := importer.Slug(c.text, taken); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestSlugDeduplicates(t *testing.T) {
	taken := map[string]bool{}
	const text = "A comment says why, not what."
	want := []string{"comment-says-why-not", "comment-says-why-not-2", "comment-says-why-not-3"}
	for _, w := range want {
		if got := importer.Slug(text, taken); got != w {
			t.Errorf("got %q, want %q", got, w)
		}
	}
}

func sorted(text, file string, line int, accepted bool) importer.Sorted {
	return kinded(text, file, line, importer.KindCodeRule, accepted)
}

func kinded(text, file string, line int, kind string, accepted bool) importer.Sorted {
	return importer.Sorted{
		Candidate: importer.Candidate{File: file, Line: line, Text: text},
		Kind:      kind,
		Accepted:  accepted,
	}
}

func TestDraftGolden(t *testing.T) {
	candidates := []importer.Sorted{
		sorted("A comment says why the code exists, not what it does.", "CLAUDE.md", 42, true),
		sorted("Ask before installing anything.", "CLAUDE.md", 50, false),
		sorted("A comment says why it is written this way.", "AGENTS.md", 7, true),
		kinded("The commit subject says what changed for a reader.", "AGENTS.md", 9, importer.KindCommitRule, true),
		kinded("Never leave a branch without running the tests.", "AGENTS.md", 11, importer.KindProcess, true),
	}
	importer.Assign(candidates)
	draft, err := importer.Draft(candidates, []string{importer.DefaultPreset})
	if err != nil {
		t.Fatal(err)
	}
	golden := "testdata/tenets.golden.yml"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, draft, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(draft) != string(want) {
		t.Errorf("draft is:\n%s\nwant:\n%s", draft, want)
	}
	if _, err := tenets.Parse(draft); err != nil {
		t.Errorf("the draft does not load: %v", err)
	}
}

// The starter file names the preset this repository judges itself by, so it
// has to resolve to the same rules.
func TestStarterMatchesTheRepositoryRules(t *testing.T) {
	starter, err := tenets.Parse(importer.PresetFile([]string{importer.DefaultPreset}))
	if err != nil {
		t.Fatal(err)
	}
	ours, err := tenets.Load("../../tenet.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(starter), ids(ours)) {
		t.Errorf("the starter resolves to %v, this repository to %v", ids(starter), ids(ours))
	}
}

func ids(cfg *tenets.Config) []string {
	var out []string
	for _, t := range cfg.Tenets {
		out = append(out, t.ID)
	}
	return out
}
