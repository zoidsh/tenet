package source

import (
	"reflect"
	"strings"
	"testing"
)

// A fixture whose directive follows a comment marker escapes the colon: the
// marker is a real one on this file's own line, so the directive would
// otherwise apply here as well as in the fixture. A fixture with no marker
// before it needs no escape.
var knownTenets = map[string]bool{
	"comment-why":  true,
	"no-fallback":  true,
	"no-mocking":   true,
	"other-tenet":  true,
	"still-a-rule": true,
}

func TestStripDirectives(t *testing.T) {
	lines := []string{
		"package main // tenet\x3aignore-file no-mocking",
		"x := 1 // tenet\x3aignore",
		"y := 2 // tenet\x3aignore comment-why,no-fallback",
		"// tenet\x3aignore-next-line comment-why",
		"z := 3",
		"plain := 4 // an ordinary comment",
		"notatenet:ignore stays",
	}
	stripped, sup, err := stripDirectives("a.go", lines, knownTenets)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"package main // ",
		"x := 1 // ",
		"y := 2 // ",
		"// ",
		"z := 3",
		"plain := 4 // an ordinary comment",
		"notatenet:ignore stays",
	}
	if !reflect.DeepEqual(stripped, want) {
		t.Fatalf("stripped:\n%q\nwant:\n%q", stripped, want)
	}

	if !sup.File("no-mocking") {
		t.Error("ignore-file did not suppress its tenet")
	}
	if sup.File("comment-why") {
		t.Error("ignore-file suppressed a tenet it did not name")
	}
	if !sup.Line(2, "anything") {
		t.Error("bare ignore should suppress every tenet on its line")
	}
	if !sup.Line(3, "comment-why") || !sup.Line(3, "no-fallback") {
		t.Error("id list not honoured")
	}
	if sup.Line(3, "other-tenet") {
		t.Error("a listed ignore suppressed an unlisted tenet")
	}
	if !sup.Line(5, "comment-why") {
		t.Error("ignore-next-line did not reach the next line")
	}
	if sup.Line(4, "comment-why") {
		t.Error("ignore-next-line suppressed its own line")
	}
	if sup.Line(6, "comment-why") || sup.Line(7, "comment-why") {
		t.Error("a directive was recognised inside another word")
	}
}

func TestStripDirectivesIDListSpacing(t *testing.T) {
	lines := []string{"x := 1 // tenet\x3aignore comment-why, no-fallback"}
	stripped, sup, err := stripDirectives("a.go", lines, knownTenets)
	if err != nil {
		t.Fatal(err)
	}
	if stripped[0] != "x := 1 // " {
		t.Errorf("stripped %q", stripped[0])
	}
	if !sup.Line(1, "comment-why") || !sup.Line(1, "no-fallback") {
		t.Error("a space after the comma broke the id list")
	}
}

func TestStripDirectivesRejectsProseAsAnID(t *testing.T) {
	lines := []string{"x := 1 // tenet\x3aignore because this is fine"}
	_, _, err := stripDirectives("pkg/a.go", lines, knownTenets)
	if err == nil {
		t.Fatal("want an error: prose after a directive reads as a tenet id")
	}
	for _, want := range []string{"pkg/a.go", ":1:", `"because"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestStripDirectivesRejectsUnknownTenet(t *testing.T) {
	_, _, err := stripDirectives("a.go", []string{"// tenet\x3aignore-file no-such-rule"}, knownTenets)
	if err == nil || !strings.Contains(err.Error(), "no-such-rule") {
		t.Fatalf("error is %v", err)
	}
}

func TestStripDirectivesRejectsUnknownDirective(t *testing.T) {
	_, _, err := stripDirectives("a.go", []string{"// tenet\x3aignore-foo"}, knownTenets)
	if err == nil || !strings.Contains(err.Error(), "unknown directive tenet:ignore-foo") {
		t.Fatalf("error is %v", err)
	}
}

func TestStripDirectivesOnTheLastLineWithoutANewline(t *testing.T) {
	file, err := NewFile("a.go", []byte("x := 1\ny := 2 // tenet\x3aignore comment-why"), nil, knownTenets)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Lines) != 2 {
		t.Fatalf("got %d lines: %q", len(file.Lines), file.Lines)
	}
	if strings.Contains(file.Lines[1], "tenet:ignore") {
		t.Errorf("the directive survived on the last line: %q", file.Lines[1])
	}
	if !file.Sup.Line(2, "comment-why") {
		t.Error("a directive on a last line without a newline was not recorded")
	}
}

func TestStripDirectivesSkipsFencedBlocksInProse(t *testing.T) {
	lines := []string{
		"An example of a directive:",
		"```go",
		"// tenet\x3aignore no-such-rule",
		"```",
	}
	stripped, sup, err := stripDirectives("README.md", lines, knownTenets)
	if err != nil {
		t.Fatalf("a fenced example was validated: %v", err)
	}
	if !reflect.DeepEqual(stripped, lines) {
		t.Errorf("stripped:\n%q\nwant:\n%q", stripped, lines)
	}
	if sup.Line(3, "comment-why") {
		t.Error("a fenced example suppressed a tenet")
	}
}

func TestStripDirectivesCountsTheSameLineOutsideAFence(t *testing.T) {
	lines := []string{"An example of a directive:", "// tenet\x3aignore comment-why"}
	stripped, sup, err := stripDirectives("README.md", lines, knownTenets)
	if err != nil {
		t.Fatal(err)
	}
	if stripped[1] != "// " {
		t.Errorf("stripped %q", stripped[1])
	}
	if !sup.Line(2, "comment-why") {
		t.Error("a line-start directive outside a fence was not recorded")
	}
}

func TestStripDirectivesFenceRunsToEndOfFile(t *testing.T) {
	lines := []string{"~~~", "<!-- tenet\x3aignore-file no-such-rule -->", "// tenet\x3aignore-foo"}
	stripped, sup, err := stripDirectives("README.md", lines, knownTenets)
	if err != nil {
		t.Fatalf("an unclosed fence stopped holding: %v", err)
	}
	if !reflect.DeepEqual(stripped, lines) {
		t.Errorf("stripped:\n%q\nwant:\n%q", stripped, lines)
	}
	if sup.File("comment-why") || sup.Line(3, "comment-why") {
		t.Error("a line after an unclosed fence suppressed a tenet")
	}
}

func TestStripDirectivesKeepsLineNumbers(t *testing.T) {
	lines := []string{"a", "// tenet\x3aignore-next-line", "b"}
	stripped, _, err := stripDirectives("a.go", lines, knownTenets)
	if err != nil {
		t.Fatal(err)
	}
	if len(stripped) != len(lines) {
		t.Fatalf("got %d lines, want %d", len(stripped), len(lines))
	}
	if stripped[2] != "b" {
		t.Errorf("line 3 is %q", stripped[2])
	}
}
