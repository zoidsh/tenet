package source

import (
	"strings"
	"testing"
)

// A fixture escapes the colon only where this file's own line carries a
// comment marker, or sits inside a block comment opened by an earlier line;
// there the directive would apply to this file as well as to the fixture.
func TestDirectiveMustSitInAComment(t *testing.T) {
	const directive = "tenet:ignore comment-why"
	for _, tc := range []struct {
		name  string
		path  string
		lines []string
		want  bool
	}{
		{"go line comment", "a.go", []string{"x := 1 // " + directive}, true},
		{"go code", "a.go", []string{"x := call(" + directive + ")"}, false},
		{"go string literal", "a.go", []string{`x := "// ` + directive + `"`}, true},
		{"go block on one line", "a.go", []string{"x := 1 /* " + directive + " */ + 2"}, true},
		{"go after a closed block", "a.go", []string{"x := 1 /* c */ + call(" + directive + ")"}, false},
		{"python comment", "a.py", []string{"x = 1  # " + directive}, true},
		{"python code", "a.py", []string{"x = f('" + directive + "')"}, false},
		{"python docstring", "a.py", []string{`"""`, directive, `"""`, "x = 1"}, true},
		{"typescript comment", "a.ts", []string{"const x = 1 // " + directive}, true},
		{"typescript code", "a.ts", []string{"const x = call(" + directive + ")"}, false},
		{"ruby comment", "a.rb", []string{"x = 1 # " + directive}, true},
		{"ruby block", "a.rb", []string{"=begin", directive, "=end", "x = 1"}, true},
		{"ruby =begin indented", "a.rb", []string{"  =begin", directive, "  =end", "x = 1"}, false},
		{"ruby =begin mid line", "a.rb", []string{"x = 1 =begin", directive, "=end"}, false},
		{"ruby code", "a.rb", []string{"x = call(" + directive + ")"}, false},
		{"sql comment", "a.sql", []string{"select 1 -- " + directive}, true},
		{"sql code", "a.sql", []string{"select '" + directive + "' from t"}, false},
		{"yaml comment of its own", "a.yml", []string{"# " + directive, "key: value"}, true},
		{"yaml mid line", "a.yml", []string{"key: value # " + directive}, false},
		{"markdown line start", "a.md", []string{directive}, true},
		{"markdown list item", "a.md", []string{"- " + directive}, true},
		{"markdown html comment", "a.md", []string{"<!-- " + directive + " -->"}, true},
		{"markdown mid sentence", "a.md", []string{"Write " + directive + " to exempt a line."}, false},
		{"commit message anywhere", CommitMsgPath, []string{"Fix it, with " + directive + " on the call"}, true},
		{"unknown language anywhere", "a.lisp", []string{"(call " + directive + ")"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, sup, err := stripDirectives(tc.path, tc.lines, knownTenets)
			if err != nil {
				t.Fatal(err)
			}
			got := false
			for i := range tc.lines {
				got = got || sup.Line(i+1, "comment-why")
			}
			if got != tc.want {
				t.Errorf("counted as a directive: %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDirectiveInABlockCommentSpanningLines(t *testing.T) {
	lines := []string{
		"x := 1",
		"/* a note",
		"   tenet\x3aignore-next-line comment-why",
		"   and more */",
		"y := 2",
		`z := call("tenet:ignore-file no-such-rule")`,
	}
	stripped, sup, err := stripDirectives("a.go", lines, knownTenets)
	if err != nil {
		t.Fatal(err)
	}
	if !sup.Line(4, "comment-why") {
		t.Error("a directive inside a multi-line block comment was not honoured")
	}
	if stripped[5] != lines[5] {
		t.Errorf("a mention after the block closed was treated as a directive: %q", stripped[5])
	}
}

func TestUnknownTenetInACommentStillErrors(t *testing.T) {
	_, _, err := stripDirectives("a.py", []string{"x = 1  # tenet:ignore no-such-rule"}, knownTenets)
	if err == nil || !strings.Contains(err.Error(), "no-such-rule") {
		t.Fatalf("error is %v", err)
	}
}

func TestUnknownTenetOutsideACommentIsLeftAlone(t *testing.T) {
	lines := []string{`x := call("tenet:ignore no-such-rule")`}
	stripped, _, err := stripDirectives("a.go", lines, knownTenets)
	if err != nil {
		t.Fatalf("a mention outside a comment was validated: %v", err)
	}
	if stripped[0] != lines[0] {
		t.Errorf("stripped %q", stripped[0])
	}
}
