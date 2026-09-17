package source

import (
	"reflect"
	"testing"
)

func TestStripDirectives(t *testing.T) {
	lines := []string{
		"package main // tenet:ignore-file no-mocking",
		"x := 1 // tenet:ignore",
		"y := 2 // tenet:ignore comment-why,no-fallback",
		"// tenet:ignore-next-line comment-why",
		"z := 3",
		"plain := 4 // an ordinary comment",
		"notatenet:ignore stays",
	}
	stripped, sup := stripDirectives(lines)

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
	if sup.Line(3, "no-defensive-nil") {
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

func TestStripDirectivesKeepsLineNumbers(t *testing.T) {
	lines := []string{"a", "// tenet:ignore-next-line", "b"}
	stripped, _ := stripDirectives(lines)
	if len(stripped) != len(lines) {
		t.Fatalf("got %d lines, want %d", len(stripped), len(lines))
	}
	if stripped[2] != "b" {
		t.Errorf("line 3 is %q", stripped[2])
	}
}
