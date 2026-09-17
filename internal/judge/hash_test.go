package judge_test

import (
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/judge"
)

const hashed = `package main

import "fmt"

func show(n int) {
	fmt.Println(n)
}

func inc(n int) int {
	// add one
	return n + 1
}

func dec(n int) int {
	// take one off
	return n - 1
}
`

func lines(text string) []string {
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// at is the one-based line the text holds, so that a test names the line it
// means rather than counting.
func at(text, want string) int {
	for i, line := range lines(text) {
		if strings.TrimSpace(line) == want {
			return i + 1
		}
	}
	panic("no line " + want)
}

func TestFindingHashSurvivesAnInsertionElsewhere(t *testing.T) {
	before := hashed
	after := "// A new line at the top.\n\n" + hashed

	got := judge.FindingHash("t", lines(before), at(before, "// add one"))
	want := judge.FindingHash("t", lines(after), at(after, "// add one"))
	if got != want {
		t.Errorf("hash is %s after an insertion above, was %s", want, got)
	}
}

func TestFindingHashSurvivesReindentation(t *testing.T) {
	before := hashed
	after := strings.ReplaceAll(hashed, "\t", "    ")

	got := judge.FindingHash("t", lines(before), at(before, "// add one"))
	want := judge.FindingHash("t", lines(after), at(after, "// add one"))
	if got != want {
		t.Errorf("hash is %s after reindentation, was %s", want, got)
	}
}

func TestFindingHashChangesWhenTheLineIsEdited(t *testing.T) {
	before := hashed
	after := strings.Replace(hashed, "// add one", "// The caller counts from one.", 1)

	got := judge.FindingHash("t", lines(before), at(before, "// add one"))
	edited := judge.FindingHash("t", lines(after), at(after, "// The caller counts from one."))
	if got == edited {
		t.Errorf("editing the line left the hash at %s", got)
	}
}

func TestFindingHashTellsIdenticalLinesApart(t *testing.T) {
	text := `func inc(n int) int {
	// off by one
	return n + 1
}

func dec(n int) int {
	// off by one
	return n - 1
}
`
	first := judge.FindingHash("t", lines(text), 2)
	second := judge.FindingHash("t", lines(text), 7)
	if first == second {
		t.Errorf("both copies of the line hash to %s", first)
	}
}

func TestFindingHashChangesWithTheTenet(t *testing.T) {
	l := lines(hashed)
	id := at(hashed, "// add one")
	if judge.FindingHash("one", l, id) == judge.FindingHash("two", l, id) {
		t.Error("two tenets share a hash on the same line")
	}
}

// A finding on the first or the last line has context on one side only, which
// is the case an off-by-one in the neighbourhood would show up in.
func TestFindingHashAtTheEdgesOfAFile(t *testing.T) {
	l := lines(hashed)
	first, last := judge.FindingHash("t", l, 1), judge.FindingHash("t", l, len(l))
	if first == "" || last == "" || first == last {
		t.Fatalf("first line hashes to %q and last to %q", first, last)
	}

	grown := lines(hashed + "\nfunc noop() {}\n")
	if judge.FindingHash("t", grown, 1) != first {
		t.Error("appending to the file changed the first line's hash")
	}
	if judge.FindingHash("t", grown, len(grown)) == last {
		t.Error("the new last line hashes the same as the old one")
	}

	shifted := lines("// A new line at the top.\n\n" + hashed)
	if judge.FindingHash("t", shifted, len(shifted)) != last {
		t.Error("an insertion at the top changed the last line's hash")
	}
}

// One line is a whole file, and it is its own only context.
func TestFindingHashOfASingleLine(t *testing.T) {
	if judge.FindingHash("t", []string{"x := 1"}, 1) == "" {
		t.Error("a one-line file hashes to nothing")
	}
}
