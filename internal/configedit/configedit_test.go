package configedit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/configedit"
	"github.com/zoidsh/tenet/internal/importer"
	"github.com/zoidsh/tenet/internal/tenets"
)

const written = `# Criteria under a tenet sharpen its verdicts.
version: 1

# The built-in rules we lean on.
rules:
  # CLAUDE.md: A comment says why the code exists, not what it does.
  - comment-why

  # CLAUDE.md: Never mock anything in tests.
  - no-mocking

presets: [agent-hygiene]

tenets:
  # Tim wrote this one by hand, so leave it alone.
  - id: keep-it-short
    tenet: Keep every function under forty lines.
    source: 'CLAUDE.md: Keep every function under forty lines.'
    criteria:
      true: A function that runs past forty lines.
      false: A table of test cases that happens to be long.
`

func parse(t *testing.T, data string) *configedit.File {
	t.Helper()
	f, err := configedit.Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func bytesOf(t *testing.T, f *configedit.File) string {
	t.Helper()
	out, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestAnUntouchedFileComesBackWordForWord(t *testing.T) {
	if got := bytesOf(t, parse(t, written)); got != written {
		t.Errorf("a round trip rewrote the file:\n%s\nwant:\n%s", got, written)
	}
}

func TestNoTrailingNewlineStaysThatWay(t *testing.T) {
	want := strings.TrimRight(written, "\n")
	if got := bytesOf(t, parse(t, want)); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestEditingOneSentenceLeavesTheRestAlone(t *testing.T) {
	f := parse(t, written)
	list := f.Tenets()
	if len(list) != 1 || list[0].ID != "keep-it-short" {
		t.Fatalf("tenets are %#v", list)
	}
	if list[0].Source != "CLAUDE.md: Keep every function under forty lines." {
		t.Errorf("source is %q", list[0].Source)
	}
	list[0].SetTenet("Keep every function under thirty lines.")
	list[0].SetSource(importer.SourceLine("CLAUDE.md", "Keep every function under thirty lines."))

	got := bytesOf(t, f)
	for _, want := range []string{
		"# Criteria under a tenet sharpen its verdicts.",
		"# Tim wrote this one by hand, so leave it alone.",
		"  # CLAUDE.md: Never mock anything in tests.",
		"presets: [agent-hygiene]",
		"      false: A table of test cases that happens to be long.",
		"    tenet: Keep every function under thirty lines.",
		"    source: 'CLAUDE.md: Keep every function under thirty lines.'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the edited file does not hold %q:\n%s", want, got)
		}
	}
	// The criteria still say forty, because sync rewords the sentence a rule
	// file wrote and nothing a person wrote underneath it.
	if !strings.Contains(got, "      true: A function that runs past forty lines.") {
		t.Errorf("the criteria were touched:\n%s", got)
	}
	if strings.Contains(got, "under forty lines.'") || strings.Contains(got, "tenet: Keep every function under forty") {
		t.Errorf("the old sentence is still there:\n%s", got)
	}
	if !strings.Contains(got, "\n\npresets:") {
		t.Errorf("the blank lines are gone:\n%s", got)
	}
}

func TestRuleCommentsAreListedAndRewritten(t *testing.T) {
	f := parse(t, written)
	comments := f.RuleComments()
	if len(comments) != 2 {
		t.Fatalf("comments are %#v", comments)
	}
	if comments[0].Rule != "comment-why" || comments[0].Text != "CLAUDE.md: A comment says why the code exists, not what it does." {
		t.Errorf("the first comment is %#v", comments[0])
	}
	if comments[1].Rule != "no-mocking" {
		t.Errorf("the second comment is %#v", comments[1])
	}
	comments[1].Set("CLAUDE.md: Never mock anything in a test.")

	got := bytesOf(t, f)
	if !strings.Contains(got, "  # CLAUDE.md: Never mock anything in a test.\n\n  - no-mocking") &&
		!strings.Contains(got, "  # CLAUDE.md: Never mock anything in a test.\n  - no-mocking") {
		t.Errorf("the comment was not rewritten above its rule:\n%s", got)
	}
	if !strings.Contains(got, "# CLAUDE.md: A comment says why the code exists, not what it does.") {
		t.Errorf("the other comment changed:\n%s", got)
	}
}

// A blank line before a comment belongs to the comment block, not to the line
// the encoder would otherwise hang it on.
func TestABlankLineAboveACommentSurvives(t *testing.T) {
	got := bytesOf(t, parse(t, written))
	if !strings.Contains(got, "  - comment-why\n\n  # CLAUDE.md: Never mock") {
		t.Errorf("the blank line inside rules: is gone:\n%s", got)
	}
}

const appended = `version: 1
tenets:
  - id: keep-it-short
    tenet: Keep every function under forty lines.
    source: 'CLAUDE.md: Keep every function under forty lines.'
`

func draft() configedit.Draft {
	return configedit.Draft{
		ID:     "comment-says-why",
		Tenet:  "A comment says why, not what.",
		Kind:   []string{"code"},
		Source: importer.SourceLine("CLAUDE.md", "A comment says why, not what."),
	}
}

func appendOne(t *testing.T, data string) string {
	t.Helper()
	f := parse(t, data)
	if err := f.Append(draft()); err != nil {
		t.Fatal(err)
	}
	got := bytesOf(t, f)
	if _, err := tenets.Parse([]byte(got)); err != nil {
		t.Fatalf("the edited config does not load: %v\n%s", err, got)
	}
	return got
}

func TestAppendCoversEveryShapeOfTenetsKey(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"a sequence of its own", appended},
		{"no tenets key", "version: 1\npresets: [agent-hygiene]\n"},
		{"an empty flow sequence", "version: 1\ntenets: []\n"},
		{"a key holding nothing", "version: 1\ntenets:\npresets: [agent-hygiene]\n"},
		{"not the last key", "version: 1\ntenets:\n  - id: a\n    tenet: Keep it short.\npresets: [agent-hygiene]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := appendOne(t, c.in)
			want := "  - id: comment-says-why\n" +
				"    tenet: A comment says why, not what.\n" +
				"    kind: [code]\n" +
				"    source: 'CLAUDE.md: A comment says why, not what.'\n"
			if !strings.Contains(got, want) {
				t.Errorf("appended:\n%s\nwant it to hold:\n%s", got, want)
			}
			cfg, err := tenets.Parse([]byte(got))
			if err != nil {
				t.Fatal(err)
			}
			if last := cfg.Tenets[len(cfg.Tenets)-1]; last.ID != "comment-says-why" {
				t.Errorf("the draft is not the last tenet, %q is", last.ID)
			}
		})
	}
}

// An appended tenet has to read like one tenet init drafted, because a config
// half grown by each should not show which line came from which command.
func TestAnAppendedTenetMatchesADraftedOne(t *testing.T) {
	sorted := []importer.Sorted{{
		Candidate: importer.Candidate{File: "CLAUDE.md", Line: 3, Text: "A comment says why, not what."},
		ID:        "comment-says-why",
		Kind:      importer.KindCodeRule,
		Accepted:  true,
	}}
	drafted, err := importer.Draft(sorted, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, entry, ok := strings.Cut(string(drafted), "tenets:\n")
	if !ok {
		t.Fatalf("the draft has no tenets:\n%s", drafted)
	}
	if got := appendOne(t, "version: 1\ntenets: []\n"); !strings.HasSuffix(got, entry) {
		t.Errorf("appended:\n%s\ndrafted:\n%s", got, entry)
	}
}

// A comment at the top of the file hangs on the document rather than on the
// first key, and the encoder writes the blank line under it itself. Counting
// it again would add a blank on every write and then walk it down the file.
func TestALeadingCommentDoesNotDriftAcrossWrites(t *testing.T) {
	for _, in := range []string{
		"# why this file is here\n\nversion: 1\ntenets: []\n",
		"# why this file is here\nversion: 1\ntenets: []\n",
		"# why this file is here\n\n\nversion: 1\ntenets: []\n",
	} {
		once := bytesOf(t, parse(t, in))
		if once != in {
			t.Errorf("one write turned\n%q\ninto\n%q", in, once)
		}
		if twice := bytesOf(t, parse(t, once)); twice != once {
			t.Errorf("a second write turned\n%q\ninto\n%q", once, twice)
		}
	}
}

const withExample = `version: 1
tenets:
  - id: one
    tenet: Keep it short.
    examples:
      - label: violation
        lang: go
        code: |
          func a() {
              b()

              c()
          }

  - id: two
    tenet: Keep it shorter.
`

// A block scalar inside a sequence item opens at the indentation of the entry,
// not of its content, so the paragraph break after one has to be recognised as
// ending the block rather than sitting inside it.
func TestABlankLineAfterABlockScalar(t *testing.T) {
	got := bytesOf(t, parse(t, withExample))
	if got != withExample {
		t.Errorf("got:\n%q\nwant:\n%q", got, withExample)
	}
	if strings.Contains(got, "  \n") {
		t.Errorf("a separator line carries indentation:\n%q", got)
	}
	// The blank line between b() and c() is the example's own.
	if !strings.Contains(got, "              b()\n\n              c()") {
		t.Errorf("the blank line inside the example is gone:\n%s", got)
	}
}

// A config written on Windows comes back with LF endings and nothing else
// changed: the blank lines are still one blank line each, not two.
func TestCRLFComesBackAsLF(t *testing.T) {
	crlf := strings.ReplaceAll(written, "\n", "\r\n")
	got := bytesOf(t, parse(t, crlf))
	if got != written {
		t.Errorf("got:\n%q\nwant:\n%q", got, written)
	}
	if strings.Contains(got, "\r") {
		t.Errorf("a carriage return survived:\n%q", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("a blank line was doubled:\n%q", got)
	}
	if twice := bytesOf(t, parse(t, got)); twice != got {
		t.Errorf("a second write changed it:\n%q", twice)
	}
}

func TestWriteKeepsTheFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := configedit.Write(path, []byte(written)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("mode is %o, want 644", got)
	}
}

func TestAConfigThatIsNotAMapping(t *testing.T) {
	for _, in := range []string{"- one\n- two\n", "just a string\n"} {
		if _, err := configedit.Parse([]byte(in)); err == nil {
			t.Errorf("%q parsed as a config", in)
		}
	}
}

func TestWriteReplacesTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := configedit.Write(path, []byte(written)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != written {
		t.Errorf("wrote:\n%s", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the temporary file was left behind: %v", entries)
	}
}
