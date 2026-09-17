package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/report"
	"github.com/zoidsh/tenet/internal/source"
)

// One rule over both texts is what a team writes once, so the config a pull
// request is linted with is usually the commit message's as well.
const prTextConfig = `version: 1
model: jev-1.13.0
tenets:
  - id: title-for-a-reader
    tenet: The subject or title says what changed for a reader.
    kind: [commit, pr]
`

const prTextIncludeConfig = `version: 1
model: jev-1.13.0
tenets:
  - id: title-for-a-reader
    tenet: The subject or title says what changed for a reader.
    include: ["PULL_REQUEST"]
`

// A blank line stands between the title and the description, which puts the
// stand-in server's finding on the third line.
const prText = `Rename parseAll to parse

# Why
Because the old name lied about what it did.
`

func writePRText(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "pr.txt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLintPRTextEndToEnd(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenet.yml", prTextConfig)
	path := writePRText(t, dir, prText)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "json", "--no-cache", "--pr-text", path})

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
	if f.File != source.PRTextPath || f.Line != 3 || f.Tenet != "title-for-a-reader" {
		t.Errorf("finding is %#v", f)
	}
	if got.Stats.Files != 1 || got.Stats.Windows != 1 {
		t.Errorf("stats are %#v", got.Stats)
	}
	if got.Next != report.NextPR {
		t.Errorf("next is %q", got.Next)
	}
}

// A markdown description is prose, not a file of comments: the `#` heading it
// opens with is text the rule is about.
func TestLintPRTextKeepsEveryLine(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenet.yml", prTextIncludeConfig)
	path := writePRText(t, dir, prText)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "text", "--no-cache", "--pr-text", path})

	if code := execute(root); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "PULL_REQUEST:3: title-for-a-reader") {
		t.Errorf("stdout is %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), report.NextPR) {
		t.Errorf("stdout does not say what to do about the text: %q", stdout.String())
	}
}

// A repository whose tenets are all about code must not pay for a call on
// every push.
func TestLintPRTextWithoutATenetForIt(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenet.yml", testConfig)
	path := writePRText(t, dir, prText)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "text", "--pr-text", path})

	if code := execute(root); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "0 findings") || !strings.Contains(stdout.String(), "0 windows, 0 calls") {
		t.Errorf("stdout is %q", stdout.String())
	}
}

func TestLintPRTextIsAloneOnTheCommandLine(t *testing.T) {
	for _, args := range [][]string{
		{"--pr-text", "pr.txt", "inc.go"},
		{"--pr-text", "pr.txt", "--base", "main"},
		{"--pr-text", "pr.txt", "--commit-msg", "COMMIT_EDITMSG"},
	} {
		dir := t.TempDir()
		git(t, dir, "init", "-q", "-b", "main")
		writeFile(t, dir, "tenet.yml", prTextConfig)
		writePRText(t, dir, prText)
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
		if !strings.Contains(stderr.String(), "--pr-text") {
			t.Errorf("%v: stderr is %q", args, stderr.String())
		}
	}
}
