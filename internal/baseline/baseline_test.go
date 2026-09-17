package baseline_test

import (
	"testing"

	"github.com/zoidsh/tenetlint/internal/baseline"
)

func paths(p ...string) baseline.Scope {
	return baseline.Scope{Mode: baseline.ModePaths, Paths: p}
}

func TestScopeCovers(t *testing.T) {
	cases := []struct {
		name  string
		run   baseline.Scope
		wrote baseline.Scope
		want  bool
	}{
		{"the same paths", paths("internal"), paths("internal"), true},
		{"the whole tree over a directory", paths("."), paths("internal"), true},
		{"a directory over a file in it", paths("internal"), paths("internal/judge/judge.go"), true},
		{"a directory over its neighbour", paths("internal"), paths("cmd"), false},
		{"a file over the tree", paths("inc.go"), paths("."), false},
		{"a prefix that is not a directory", paths("internal"), paths("internals/a.go"), false},
		{"several paths over one of them", paths("cmd", "internal"), paths("internal"), true},
		{"the staged changes", baseline.Scope{Mode: baseline.ModeStaged}, baseline.Scope{Mode: baseline.ModeStaged}, true},
		{"the same base", baseline.Scope{Mode: baseline.ModeBase, Base: "main"}, baseline.Scope{Mode: baseline.ModeBase, Base: "main"}, true},
		{"another base", baseline.Scope{Mode: baseline.ModeBase, Base: "main"}, baseline.Scope{Mode: baseline.ModeBase, Base: "v1"}, false},
		{"paths against a base", paths("."), baseline.Scope{Mode: baseline.ModeBase, Base: "main"}, false},
		{"a baseline with no scope", paths("."), baseline.Scope{}, false},
	}
	for _, c := range cases {
		if got := c.run.Covers(c.wrote); got != c.want {
			t.Errorf("%s: covers = %v, want %v", c.name, got, c.want)
		}
	}
}
