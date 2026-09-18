package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	write(t, dir, "seed.txt", "seed\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "seed")
	return dir
}

func fileByPath(t *testing.T, set *Set, path string) *File {
	t.Helper()
	for _, f := range set.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("%s not collected, got %v", path, paths(set))
	return nil
}

func paths(set *Set) []string {
	var out []string
	for _, f := range set.Files {
		out = append(out, f.Path)
	}
	return out
}

func TestCollectStaged(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.go", "one\ntwo\nthree\n")
	// git writes the header of a path with whitespace in it differently.
	write(t, dir, "my file.go", "one\ntwo\nthree\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "a")

	write(t, dir, "a.go", "one\ntwo changed\nthree\n")
	write(t, dir, "my file.go", "one\ntwo\nthree changed\n")
	write(t, dir, "b.go", "new\n")
	write(t, dir, "unstaged.go", "nope\n")
	run(t, dir, "git", "add", "a.go", "b.go", "my file.go")
	// The working tree moves on after staging, to prove the index is what is
	// read.
	write(t, dir, "a.go", "one\ntwo changed again\nthree\n")

	set, err := Collect(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(set); len(got) != 3 {
		t.Fatalf("collected %v", got)
	}
	spaced := fileByPath(t, set, "my file.go")
	if !spaced.Reportable(3) {
		t.Error("the changed line of a path with a space is not reportable")
	}
	if spaced.Reportable(1) || spaced.Reportable(2) {
		t.Error("an unchanged line of a path with a space is reportable")
	}
	a := fileByPath(t, set, "a.go")
	if a.Lines[1] != "two changed" {
		t.Errorf("line 2 is %q, want the staged content", a.Lines[1])
	}
	if !a.Reportable(2) {
		t.Error("the changed line is not reportable")
	}
	if a.Reportable(1) || a.Reportable(3) {
		t.Error("an unchanged line is reportable")
	}
	b := fileByPath(t, set, "b.go")
	if !b.Reportable(1) {
		t.Error("a new file's line is not reportable")
	}
}

func TestCollectStagedContentThatLooksLikeAHeader(t *testing.T) {
	dir := newRepo(t)
	body := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	write(t, dir, "c.go", body)
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "c")

	// The first hunk adds a line that reaches the parser as "+++ b/evil.go",
	// which must not take over from the file the hunks belong to.
	changed := "one\n++ b/evil.go\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten changed\n"
	write(t, dir, "c.go", changed)
	run(t, dir, "git", "add", "-A")

	set, err := Collect(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(set); len(got) != 1 || got[0] != "c.go" {
		t.Fatalf("collected %v", got)
	}
	c := fileByPath(t, set, "c.go")
	if !c.Reportable(2) || !c.Reportable(10) {
		t.Errorf("reportable lines are %v, want the two changed ones", c.reportable)
	}
}

func TestCollectStagedRename(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "old.go", "one\ntwo\nthree\nfour\nfive\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "old")

	run(t, dir, "git", "mv", "old.go", "new.go")
	write(t, dir, "new.go", "one\ntwo\nthree\nfour\nfive\nsix\n")
	run(t, dir, "git", "add", "-A")

	set, err := Collect(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(set); len(got) != 1 || got[0] != "new.go" {
		t.Fatalf("collected %v", got)
	}
	f := fileByPath(t, set, "new.go")
	for line := 1; line <= 5; line++ {
		if f.Reportable(line) {
			t.Errorf("line %d of a renamed file is reportable, only the added line is", line)
		}
	}
	if !f.Reportable(6) {
		t.Error("the line added along with the rename is not reportable")
	}
}

func TestCollectStagedPureRenameHasNoWindow(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "old.go", "one\ntwo\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "old")
	run(t, dir, "git", "mv", "old.go", "new.go")

	set, err := Collect(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(set); len(got) != 0 {
		t.Fatalf("collected %v, want nothing to lint", got)
	}
	if len(set.Windows()) != 0 {
		t.Errorf("a rename that touched no line produced %d windows", len(set.Windows()))
	}
}

func TestCollectStagedDeletionOnly(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.go", "one\ntwo\nthree\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "a")
	write(t, dir, "a.go", "one\nthree\n")
	run(t, dir, "git", "add", "-A")

	set, err := Collect(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(set); len(got) != 0 {
		t.Fatalf("collected %v, want nothing: only a line was removed", got)
	}
	if len(set.Windows()) != 0 {
		t.Errorf("a deletion produced %d windows", len(set.Windows()))
	}
}

func TestCollectStagedModeOnly(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.go", "one\ntwo\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "a")
	run(t, dir, "git", "update-index", "--chmod=+x", "a.go")

	set, err := Collect(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(set); len(got) != 0 {
		t.Fatalf("collected %v, want nothing: only the mode changed", got)
	}
}

func TestCollectBase(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.go", "one\ntwo\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "a")
	run(t, dir, "git", "checkout", "-q", "-b", "work")
	write(t, dir, "a.go", "one\ntwo\nthree\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "b")
	write(t, dir, "a.go", "one\ntwo\nthree\nfour\n")

	set, err := Collect(context.Background(), Options{Dir: dir, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	a := fileByPath(t, set, "a.go")
	if len(a.Lines) != 4 {
		t.Errorf("got %d lines, want the working tree's", len(a.Lines))
	}
	if !a.Reportable(3) || !a.Reportable(4) || a.Reportable(1) {
		t.Errorf("reportable lines wrong: %v", a.reportable)
	}
}

func TestCollectStagedSkips(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ".env", "TYPESAFE_API_KEY=x\n")
	write(t, dir, "key.pem", "secret\n")
	write(t, dir, "id_rsa", "secret\n")
	write(t, dir, "vendor/v.go", "package v\n")
	write(t, dir, "big.go", strings.Repeat("a", MaxFileBytes+1))
	write(t, dir, "bin.go", "package main\x00\n")
	write(t, dir, ConfigFile, "version: 1\n")
	write(t, dir, ConfigDir+"/"+BaselineName, "{}\n")
	write(t, dir, "ok.go", "package main\n")
	run(t, dir, "git", "add", "-Af")

	set, err := Collect(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(set); len(got) != 1 || got[0] != "ok.go" {
		t.Fatalf("collected %v, want only ok.go", got)
	}
	reasons := map[string]string{}
	for _, s := range set.Skipped {
		reasons[s.File] = s.Reason
	}
	for _, want := range []struct{ file, reason string }{
		{".env", "may hold a secret"},
		{"key.pem", "may hold a secret"},
		{"id_rsa", "may hold a secret"},
		{"vendor/v.go", "in vendor"},
		{"big.go", "larger than 1MB"},
		{"bin.go", "binary"},
		{ConfigFile, "in " + ConfigDir},
		{ConfigDir + "/" + BaselineName, "in " + ConfigDir},
	} {
		if reasons[want.file] != want.reason {
			t.Errorf("%s skipped as %q, want %q", want.file, reasons[want.file], want.reason)
		}
	}
}

func TestCollectPathsSkipsOwnFiles(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ConfigFile, "version: 1\n")
	write(t, dir, ConfigDir+"/"+BaselineName, "{}\n")
	write(t, dir, "ok.go", "package main\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "files")

	set, err := Collect(context.Background(), Options{Dir: dir, Paths: []string{"."}})
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range paths(set) {
		if strings.HasPrefix(got, ConfigDir+"/") {
			t.Errorf("collected %s", got)
		}
	}
}

func TestCollectPaths(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ".gitignore", "ignored.go\n")
	write(t, dir, "pkg/a.go", "one\ntwo\n")
	write(t, dir, "pkg/ignored.go", "hidden\n")
	write(t, dir, "pkg/node_modules/x.go", "noise\n")
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "files")

	set, err := Collect(context.Background(), Options{Dir: dir, Paths: []string{"pkg"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(set); len(got) != 1 || got[0] != "pkg/a.go" {
		t.Fatalf("collected %v", got)
	}
	a := fileByPath(t, set, "pkg/a.go")
	if !a.Reportable(1) || !a.Reportable(2) {
		t.Error("every line should be reportable when paths are given")
	}
	if a.Reportable(3) {
		t.Error("a line past the end is reportable")
	}
}

func TestCollectOutsideRepository(t *testing.T) {
	dir := t.TempDir()
	if _, err := Collect(context.Background(), Options{Dir: dir}); err == nil {
		t.Fatal("want an error outside a repository")
	}
}

func TestCollectStripsDirectivesFromContent(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.go", "x := 1 // tenet\x3aignore comment-why\n")
	run(t, dir, "git", "add", "-A")

	set, err := Collect(context.Background(), Options{Dir: dir, Tenets: []string{"comment-why"}})
	if err != nil {
		t.Fatal(err)
	}
	a := fileByPath(t, set, "a.go")
	if strings.Contains(a.Lines[0], "tenet:ignore") {
		t.Errorf("the directive reached the model: %q", a.Lines[0])
	}
	if !a.Sup.Line(1, "comment-why") {
		t.Error("the directive was not recorded")
	}
}

func TestCollectRejectsAnUnknownTenetInADirective(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.go", "x := 1 // tenet\x3aignore comment-why\n")
	run(t, dir, "git", "add", "-A")

	_, err := Collect(context.Background(), Options{Dir: dir, Tenets: []string{"no-fallback"}})
	if err == nil || !strings.Contains(err.Error(), "comment-why") {
		t.Fatalf("error is %v", err)
	}
}
