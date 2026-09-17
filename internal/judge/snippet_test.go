package judge_test

import (
	"testing"

	"github.com/zoidsh/tenet/internal/judge"
	"github.com/zoidsh/tenet/internal/source"
)

func TestStateOfFraming(t *testing.T) {
	body := []string{"one", "two"}
	for _, tc := range []struct {
		kind string
		lang string
		path string
		want string
	}{
		{
			kind: source.KindCode,
			lang: "go",
			path: "internal/a.go",
			want: "Language: go. File: internal/a.go. Source file excerpt:\nL001 one\nL002 two\n",
		},
		{
			kind: source.KindProse,
			lang: "markdown",
			path: "README.md",
			want: "Document: markdown. File: README.md. Text excerpt:\nL001 one\nL002 two\n",
		},
		{
			kind: source.KindData,
			lang: "yaml",
			path: "tenet.yml",
			want: "Data file: yaml. File: tenet.yml. Excerpt:\nL001 one\nL002 two\n",
		},
		{
			kind: source.KindCommit,
			lang: "text",
			path: source.CommitMsgPath,
			want: "Commit message. Text excerpt:\nL001 one\nL002 two\n",
		},
		{
			kind: source.KindPR,
			lang: "text",
			path: source.PRTextPath,
			want: "Pull request title and description. Text excerpt:\nL001 one\nL002 two\n",
		},
	} {
		if got := judge.StateOf(tc.kind, tc.lang, tc.path, body); got != tc.want {
			t.Errorf("%s state is\n%q\nwant\n%q", tc.kind, got, tc.want)
		}
	}
}

func TestStateFramesFileByItsKind(t *testing.T) {
	f := &source.File{Path: "docs/guide.md", Lines: []string{"A paragraph."}}
	want := "Document: markdown. File: docs/guide.md. Text excerpt:\nL001 A paragraph.\n"
	if got := judge.State(f.Windows()[0]); got != want {
		t.Errorf("state is %q, want %q", got, want)
	}
}

func TestKindForLanguage(t *testing.T) {
	for lang, want := range map[string]string{
		"go":       source.KindCode,
		"markdown": source.KindProse,
		"text":     source.KindProse,
		"rst":      source.KindProse,
		"asciidoc": source.KindProse,
		"yaml":     source.KindData,
		"json":     source.KindData,
		"klingon":  source.KindCode,
	} {
		if got := source.KindForLanguage(lang); got != want {
			t.Errorf("KindForLanguage(%q) is %q, want %q", lang, got, want)
		}
	}
}
