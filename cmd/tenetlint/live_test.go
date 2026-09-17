package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/zoidsh/tenetlint/internal/jev"
)

// The line narrating.go narrates on, which is the only finding the tenet
// should have there.
const narratingLine = 5

func TestLiveComment(t *testing.T) {
	if jev.KeyFromEnv() == "" {
		t.Skipf("%s is not set", jev.APIKeyEnv)
	}

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--config", "testdata/tenets.yml",
		"--format", "json",
		"--no-cache",
		"testdata/narrating.go",
	})

	// A finding the model is unsure of exits 0, so only a broken run is a
	// failure here; what the finding is gets checked below.
	if code := execute(root); code > 1 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	var got jsonReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout.String())
	}
	if len(got.Findings) != 1 {
		t.Fatalf("got %d findings, want one: %#v", len(got.Findings), got.Findings)
	}
	f := got.Findings[0]
	if f.Tenet != "comment-why" || f.Line != narratingLine {
		t.Errorf("finding is %#v", f)
	}
}
