package importer_test

import (
	"os"
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/importer"
)

func TestSplitFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/rules.md")
	if err != nil {
		t.Fatal(err)
	}
	const comments = "Project rules > Comments"
	want := []importer.Candidate{
		{File: "rules.md", Line: 15, Heading: comments, Text: "A comment says only what the code cannot."},
		{File: "rules.md", Line: 15, Heading: comments, Text: "Never what the code does; a name or a smaller function says that."},
		{File: "rules.md", Line: 15, Heading: comments, Text: "Call `pkg.Func()` when you need the parsed form."},
		{File: "rules.md", Line: 17, Heading: comments, Text: "Strip narrating comments from any code you touch."},
		{File: "rules.md", Line: 18, Heading: comments, Text: "Do not leave a commented-out block behind. It costs nothing to delete and git remembers it."},
		{File: "rules.md", Line: 20, Heading: comments, Text: "A doc comment on an exported identifier states what callers can rely on."},
		{File: "rules.md", Line: 25, Heading: "Project rules > Setup", Text: "Run the installer before anything else:"},
	}

	got := importer.Split("rules.md", data)
	if len(got) != len(want) {
		t.Fatalf("got %d candidates, want %d:\n%s", len(got), len(want), show(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d is %#v, want %#v", i, got[i], want[i])
		}
	}
}

func show(candidates []importer.Candidate) string {
	var b strings.Builder
	for _, c := range candidates {
		b.WriteString(c.Heading + " | " + c.Text + "\n")
	}
	return b.String()
}

func TestSplitDropsShortLongAndLinks(t *testing.T) {
	long := "word " + strings.Repeat("filler ", importer.MaxWords)
	source := "Too short here.\n\n" + long + "\n\n[the plan](docs/plan.md)\n\n~/projects/tenet\n"
	if got := importer.Split("f.md", []byte(source)); len(got) != 0 {
		t.Errorf("kept %#v", got)
	}
}

func TestSplitKeepsAThreeWordItem(t *testing.T) {
	const item = "Lint: `golangci-lint run`"
	got := importer.Split("f.md", []byte("- "+item+"\n"))
	if len(got) != 1 || got[0].Text != item {
		t.Errorf("got %#v, want the item kept", got)
	}
}

func TestSplitDropsATwoWordItem(t *testing.T) {
	if got := importer.Split("f.md", []byte("- Lint: run\n")); len(got) != 0 {
		t.Errorf("kept %#v", got)
	}
}

func TestSplitStripsBoldAnywhere(t *testing.T) {
	cases := []struct {
		source string
		want   string
	}{
		{"- **Never mock anything in tests.**\n", "Never mock anything in tests."},
		{"- **Do not** leave a commented-out block behind.\n", "Do not leave a commented-out block behind."},
		{"- __really__ important that you read this.\n", "really important that you read this."},
		{"- Strip **bold** and __underlined__ emphasis from a rule.\n", "Strip bold and underlined emphasis from a rule."},
		{"- **x__ is not a pair, so leave the markers alone.\n", "**x__ is not a pair, so leave the markers alone."},
		{"- Name the `__init__.py` that the package needs.\n", "Name the `__init__.py` that the package needs."},
		{"- Do not define __slots__ and __repr__ on a dataclass.\n", "Do not define __slots__ and __repr__ on a dataclass."},
		{"- A **`code span`** keeps its backticks, not its stars.\n", "A `code span` keeps its backticks, not its stars."},
	}
	for _, c := range cases {
		got := importer.Split("f.md", []byte(c.source))
		if len(got) != 1 || got[0].Text != c.want {
			t.Errorf("Split(%q) = %#v, want %q", c.source, got, c.want)
		}
	}
}

func TestSplitNumberedItems(t *testing.T) {
	source := "1. Run the setup step first.\n2) Then run the tests.\n"
	got := importer.Split("f.md", []byte(source))
	if len(got) != 2 || got[0].Text != "Run the setup step first." || got[1].Line != 2 {
		t.Errorf("got %#v", got)
	}
}

func TestSplitStripsBlockquoteMarkers(t *testing.T) {
	cases := []struct {
		source string
		want   string
	}{
		{"> A comment says why, not what.\n", "A comment says why, not what."},
		{"> > Nested quotes are still one rule.\n", "Nested quotes are still one rule."},
		{"- > A quoted rule inside an item.\n", "A quoted rule inside an item."},
	}
	for _, c := range cases {
		got := importer.Split("f.md", []byte(c.source))
		if len(got) != 1 || got[0].Text != c.want {
			t.Errorf("Split(%q) = %#v, want %q", c.source, got, c.want)
		}
	}
}

func TestSplitKeepsAbbreviationsWhole(t *testing.T) {
	source := "Name the narrow case, e.g. When a window is empty, in the criteria. Everything else is prose.\n"
	got := importer.Split("f.md", []byte(source))
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Text != "Name the narrow case, e.g. When a window is empty, in the criteria." {
		t.Errorf("first sentence is %q", got[0].Text)
	}
}

func TestSplitSentenceEdges(t *testing.T) {
	source := "Use it e.g. when the cache is cold and nothing else applies. Version 1.13.0 is the default one.\n"
	got := importer.Split("f.md", []byte(source))
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Text != "Use it e.g. when the cache is cold and nothing else applies." {
		t.Errorf("first sentence is %q", got[0].Text)
	}
}

// The section --agent claude writes says how to run the lint, so a later init
// must not draft its sentences back as tenets.
func TestSplitSkipsTheAgentSection(t *testing.T) {
	doc := "## Comments\n\nA comment says why the code is here.\n\n" +
		importer.AgentHeading + "\n\n" + importer.AgentSkill + "\n\n" +
		"## Tests\n\nEvery bug fix arrives with a failing test first.\n"
	got := importer.Split("CLAUDE.md", []byte(doc))
	if len(got) != 2 {
		t.Fatalf("got %d candidates:\n%s", len(got), show(got))
	}
	if got[0].Heading != "Comments" || got[1].Heading != "Tests" {
		t.Errorf("candidates are %#v", got)
	}
}

// A deeper heading inside the section is part of it; the next one at its own
// level ends it.
func TestSplitSkipsToTheNextHeadingOfTheSameLevel(t *testing.T) {
	doc := importer.AgentHeading + "\n\n### Reading the report\n\n" +
		"Read the report with the json format flag.\n\n" +
		"## Tests\n\nEvery bug fix arrives with a failing test first.\n"
	got := importer.Split("AGENTS.md", []byte(doc))
	if len(got) != 1 || got[0].Heading != "Tests" {
		t.Errorf("got %d candidates:\n%s", len(got), show(got))
	}
}

func TestSplitSkipsTheCursorRule(t *testing.T) {
	if got := importer.Split(importer.CursorRulePath, []byte(importer.AgentSkill)); got != nil {
		t.Errorf("drafted %d candidates from its own rule file", len(got))
	}
}
