package receipt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func inputs() Inputs {
	return Inputs{
		Head:        "9d2a8c1f5b3e47a06d8c2f1b4e7a903c5d6e8f10",
		Tree:        "4b825dc642cb6eb9a060e54bf8d69288fbee4904",
		Config:      []byte("version: 1\n"),
		Examples:    []Example{{Name: "examples/comment-why.yml", Data: []byte("- code: x\n")}},
		Model:       "jev-1.13.0",
		Baseline:    []byte("version: 1\n"),
		HasBaseline: true,
	}
}

func TestKeyIsStable(t *testing.T) {
	first, second := inputs(), inputs()
	if Key(first) != Key(second) {
		t.Error("the same inputs hashed differently")
	}
}

func TestKeyChangesWithEveryInput(t *testing.T) {
	base := Key(inputs())
	cases := map[string]func(*Inputs){
		"head":              func(in *Inputs) { in.Head = "1111111111111111111111111111111111111111" },
		"tree":              func(in *Inputs) { in.Tree = "0000000000000000000000000000000000000000" },
		"config":            func(in *Inputs) { in.Config = []byte("version: 2\n") },
		"example name":      func(in *Inputs) { in.Examples[0].Name = "examples/other.yml" },
		"example content":   func(in *Inputs) { in.Examples[0].Data = []byte("- code: y\n") },
		"example dropped":   func(in *Inputs) { in.Examples = nil },
		"model":             func(in *Inputs) { in.Model = "jev-1.12.0" },
		"baseline content":  func(in *Inputs) { in.Baseline = []byte("version: 2\n") },
		"baseline dropped":  func(in *Inputs) { in.HasBaseline = false },
		"baseline is empty": func(in *Inputs) { in.Baseline = nil },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			in := inputs()
			change(&in)
			if got := Key(in); got == base {
				t.Errorf("a changed %s hashed the same", name)
			}
		})
	}
}

// A field that runs into the next one would let two sets of inputs share a key.
func TestKeyDoesNotRunFieldsTogether(t *testing.T) {
	a, b := inputs(), inputs()
	a.Tree, a.Model = "abc", "def"
	b.Tree, b.Model = "abcdef", ""
	if Key(a) == Key(b) {
		t.Error("two different inputs hashed the same")
	}
}

func TestPathAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")

	path, err := Path(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Base(path) != gitPath {
		t.Fatalf("path is %q", path)
	}

	if got := Load(path); got != "" {
		t.Errorf("a repository with no receipt loaded %q", got)
	}
	key := Key(inputs())
	if err := Save(path, key); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got != key {
		t.Errorf("loaded %q, want %q", got, key)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), gitPath+".") {
			t.Errorf("the temporary file %s was left behind", e.Name())
		}
	}
}

func TestPathOutsideARepository(t *testing.T) {
	if _, err := Path(context.Background(), t.TempDir()); err == nil {
		t.Error("a directory that is no repository has a receipt path")
	}
}

func TestTree(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "a.txt", "a\n")
	run(t, dir, "add", "-A")

	tree, err := Tree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) < 40 {
		t.Fatalf("tree is %q", tree)
	}
	again, err := Tree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if again != tree {
		t.Errorf("an unchanged index wrote %q then %q", tree, again)
	}

	write(t, dir, "a.txt", "b\n")
	run(t, dir, "add", "-A")
	changed, err := Tree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if changed == tree {
		t.Error("a changed index wrote the same tree")
	}
}

func TestHead(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")

	head, err := Head(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if head != "" {
		t.Errorf("a branch with no commit on it named %q", head)
	}

	write(t, dir, "a.txt", "a\n")
	run(t, dir, "add", "-A")
	commit(t, dir, "first")
	first, err := Head(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 40 {
		t.Fatalf("head is %q", first)
	}

	// A staged tree says nothing about which commit it is a diff against, so
	// the same index on a different HEAD is a different run.
	run(t, dir, "commit", "-q", "--amend", "-m", "first, reworded")
	amended, err := Head(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if amended == first {
		t.Error("an amended commit is the same HEAD")
	}
}

// An unmerged index is no receipt: git cannot write it as a tree, and a run
// during a conflict is one that must judge rather than be excused.
func TestTreeOfAnUnmergedIndex(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "a.txt", "a\n")
	run(t, dir, "add", "-A")
	commit(t, dir, "first")

	run(t, dir, "checkout", "-q", "--orphan", "conflicting")
	write(t, dir, "a.txt", "theirs\n")
	run(t, dir, "add", "-A")
	commit(t, dir, "theirs")
	run(t, dir, "checkout", "-q", "main")

	merge := exec.Command("git", "merge", "--allow-unrelated-histories", "--no-edit", "conflicting")
	merge.Dir = dir
	if out, err := merge.CombinedOutput(); err == nil {
		t.Fatalf("the merge did not conflict: %s", out)
	}
	if tree, err := Tree(context.Background(), dir); err == nil {
		t.Errorf("an unmerged index wrote the tree %q", tree)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, message string) {
	t.Helper()
	run(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", message)
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
