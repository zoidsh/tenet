package source_test

import (
	"testing"

	"github.com/zoidsh/tenet/internal/source"
)

func TestKind(t *testing.T) {
	for _, tc := range []struct {
		path string
		want string
	}{
		{source.CommitMsgPath, source.KindCommit},
		{source.PRTextPath, source.KindPR},
		{"main.go", source.KindCode},
		{"internal/source/kind.go", source.KindCode},
		{"src/app.tsx", source.KindCode},
		{"Makefile", source.KindCode},
		{"README.md", source.KindProse},
		{"README", source.KindProse},
		{"CHANGELOG", source.KindProse},
		{"CONTRIBUTING.rst", source.KindProse},
		{"LICENSE", source.KindProse},
		{"notes.txt", source.KindProse},
		{"guide.mdx", source.KindProse},
		{"book.markdown", source.KindProse},
		{"manual.adoc", source.KindProse},
		{"docs/architecture/overview.txt", source.KindProse},
		{"docs/gen.go", source.KindProse},
		{"locales/en.strings", source.KindProse},
		{"app/i18n/de.strings", source.KindProse},
		{"locales/en.json", source.KindProse},
		{"app/i18n/de.yml", source.KindProse},
		{"settings.yml", source.KindData},
		{"package.json", source.KindData},
		{"Cargo.toml", source.KindData},
		{"go.work.lock", source.KindData},
		{"go.sum", source.KindData},
		{"go.work.sum", source.KindData},
		{"yarn.lock", source.KindData},
		{"data/rows.csv", source.KindData},
		{"pom.xml", source.KindData},
		{"setup.ini", source.KindData},
		{"docs/api.json", source.KindData},
	} {
		if got := source.Kind(tc.path); got != tc.want {
			t.Errorf("Kind(%q) is %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestFileKind(t *testing.T) {
	f := &source.File{Path: "docs/design.md"}
	if got := f.Kind(); got != source.KindProse {
		t.Errorf("file kind is %q", got)
	}
}
