package importer_test

import (
	"testing"

	"github.com/zoidsh/tenet/internal/importer"
)

func TestSourceLine(t *testing.T) {
	const text = "A comment says why the code exists, not what it does."
	got := importer.SourceLine("CLAUDE.md", text)
	if want := "CLAUDE.md: " + text; got != want {
		t.Errorf("SourceLine = %q, want %q", got, want)
	}
	back, ok := importer.ParseSource(got)
	if !ok || back.File != "CLAUDE.md" || back.Sentence != text || back.Line != 0 {
		t.Errorf("ParseSource(%q) = %#v, %v", got, back, ok)
	}
}

func TestParseSource(t *testing.T) {
	cases := []struct {
		in   string
		want importer.Source
		ok   bool
	}{
		{"CLAUDE.md:42", importer.Source{File: "CLAUDE.md", Line: 42}, true},
		{".github/instructions/go.instructions.md:7", importer.Source{File: ".github/instructions/go.instructions.md", Line: 7}, true},
		{"CLAUDE.md: Never mock anything in tests.", importer.Source{File: "CLAUDE.md", Sentence: "Never mock anything in tests."}, true},
		// The first ": " separates the file from the sentence, so a sentence
		// with a colon of its own arrives whole.
		{"CLAUDE.md: Setup: `mise install` before the tests.", importer.Source{File: "CLAUDE.md", Sentence: "Setup: `mise install` before the tests."}, true},
		// A sentence opening with a number is not a line number.
		{"CLAUDE.md: 12 columns at most.", importer.Source{File: "CLAUDE.md", Sentence: "12 columns at most."}, true},
		// A sentence ending in a number is not a line number either.
		{"CLAUDE.md: Never set a timeout above 30", importer.Source{File: "CLAUDE.md", Sentence: "Never set a timeout above 30"}, true},
		// Nor is one whose own colon has digits after it.
		{"CLAUDE.md: Keep the ratio at 16:9", importer.Source{File: "CLAUDE.md", Sentence: "Keep the ratio at 16:9"}, true},
		{"CLAUDE.md: Read RFC 2119: 3", importer.Source{File: "CLAUDE.md", Sentence: "Read RFC 2119: 3"}, true},
		{"tenet synthesis", importer.Source{}, false},
		{"", importer.Source{}, false},
		{"CLAUDE.md:0", importer.Source{}, false},
		{"CLAUDE.md: ", importer.Source{}, false},
	}
	for _, c := range cases {
		got, ok := importer.ParseSource(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseSource(%q) = %#v, %v; want %#v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}
