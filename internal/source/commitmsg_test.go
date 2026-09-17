package source

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const message = `Refactor the parser

Rename parseAll to parse.
# Please enter the commit message for your changes.
#
# On branch main
` + scissorsLine + `
diff --git a/parse.go b/parse.go
+func parse() {}
`

func TestCollectCommitMsgStripsGitsOwnLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "COMMIT_EDITMSG")
	if err := os.WriteFile(path, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}

	set, err := Collect(context.Background(), Options{Dir: dir, CommitMsg: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 1 {
		t.Fatalf("collected %d files", len(set.Files))
	}
	file := set.Files[0]
	if file.Path != CommitMsgPath {
		t.Errorf("path is %q", file.Path)
	}
	want := []string{"Refactor the parser", "", "Rename parseAll to parse."}
	if !reflect.DeepEqual(file.Lines, want) {
		t.Errorf("lines are %#v, want %#v", file.Lines, want)
	}
	for line := 1; line <= len(want); line++ {
		if !file.Reportable(line) {
			t.Errorf("line %d is not reportable", line)
		}
	}
}

// A comment between two lines of the message is blanked rather than removed,
// so that what follows it keeps the number the editor showed it under.
func TestCollectCommitMsgKeepsLineNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "COMMIT_EDITMSG")
	body := "Rename the parser\n# a comment git will strip\n\nIt was called parseAll.\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	set, err := Collect(context.Background(), Options{Dir: dir, CommitMsg: path})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Rename the parser", "", "", "It was called parseAll."}
	if !reflect.DeepEqual(set.Files[0].Lines, want) {
		t.Errorf("lines are %#v, want %#v", set.Files[0].Lines, want)
	}
}

func TestCollectCommitMsgWithNothingLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "COMMIT_EDITMSG")
	if err := os.WriteFile(path, []byte("# nothing but comments\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	set, err := Collect(context.Background(), Options{Dir: dir, CommitMsg: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 0 || len(set.Windows()) != 0 {
		t.Errorf("collected %#v", set.Files)
	}
}
