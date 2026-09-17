package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/report"
	"github.com/zoidsh/tenetlint/internal/source"
)

const commitMsgConfig = `version: 1
model: jev-1.13.0
tenets:
  - id: subject-imperative
    tenet: The commit subject is in the imperative mood.
    kind: [commit]
`

// A tenet may still reach the message by its name alone, without naming the
// kind.
const commitMsgIncludeConfig = `version: 1
model: jev-1.13.0
tenets:
  - id: subject-imperative
    tenet: The commit subject is in the imperative mood.
    include: ["COMMIT_EDITMSG"]
`

// The comment lines are what git strips, so the third line of the message the
// model is shown is the third line of the file, which is where the stand-in
// server puts its finding.
const commitMsg = `Renamed parseAll to parse
# Please enter the commit message for your changes.
Because the old name lied about what it did.
# On branch main
`

func writeCommitMsg(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "COMMIT_EDITMSG")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLintCommitMsgEndToEnd(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", commitMsgConfig)
	path := writeCommitMsg(t, dir, commitMsg)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "json", "--no-cache", "--commit-msg", path})

	if code := execute(root); code != 1 {
		t.Fatalf("exit %d, want 1\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	var got jsonReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout.String())
	}
	if len(got.Findings) != 1 {
		t.Fatalf("findings are %#v", got.Findings)
	}
	f := got.Findings[0]
	if f.File != source.CommitMsgPath || f.Line != 3 || f.Tenet != "subject-imperative" {
		t.Errorf("finding is %#v", f)
	}
	if got.Stats.Files != 1 || got.Stats.Windows != 1 {
		t.Errorf("stats are %#v", got.Stats)
	}
	if got.Next != report.NextCommitMsg {
		t.Errorf("next is %q", got.Next)
	}
}

func TestLintCommitMsgText(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", commitMsgIncludeConfig)
	path := writeCommitMsg(t, dir, commitMsg)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "text", "--no-cache", "--commit-msg", path})

	if code := execute(root); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "COMMIT_EDITMSG:3: subject-imperative") {
		t.Errorf("stdout is %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), report.NextCommitMsg) {
		t.Errorf("stdout does not say what to do about the message: %q", stdout.String())
	}
}

// A repository whose tenets are all about code says so in its summary rather
// than paying for a call on every commit.
func TestLintCommitMsgWithoutATenetForIt(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	path := writeCommitMsg(t, dir, commitMsg)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "text", "--commit-msg", path})

	if code := execute(root); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "0 findings") || !strings.Contains(stdout.String(), "0 windows") {
		t.Errorf("stdout is %q", stdout.String())
	}
}

func TestLintCommitMsgIsAloneOnTheCommandLine(t *testing.T) {
	for _, args := range [][]string{
		{"--commit-msg", "COMMIT_EDITMSG", "inc.go"},
		{"--commit-msg", "COMMIT_EDITMSG", "--base", "main"},
	} {
		dir := t.TempDir()
		git(t, dir, "init", "-q", "-b", "main")
		writeFile(t, dir, "tenets.yml", commitMsgConfig)
		writeCommitMsg(t, dir, commitMsg)
		t.Chdir(dir)
		t.Setenv(jev.APIKeyEnv, "test-key")

		var stdout, stderr bytes.Buffer
		root := newRootCmd()
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs(args)

		if code := execute(root); code != 2 {
			t.Errorf("%v exited %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "--commit-msg") {
			t.Errorf("%v: stderr is %q", args, stderr.String())
		}
	}
}
