package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zoidsh/tenetlint/internal/baseline"
	"github.com/zoidsh/tenetlint/internal/jev"
)

// baselineRepo is a repository holding one file the answer server finds a
// violation in, ready for a run started in it.
func baselineRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	writeFile(t, dir, "inc.go", staged)
	git(t, dir, "add", "-A")
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)
	return dir
}

func readBaseline(t *testing.T, path string) baseline.File {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f baseline.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("%v in %s", err, data)
	}
	return f
}

func TestBaselineWrite(t *testing.T) {
	dir := baselineRepo(t)

	code, stdout, stderr := runCmd(t, "baseline", "--no-cache", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "1 finding") || !strings.Contains(stdout, baseline.Name) {
		t.Errorf("stdout is %q", stdout)
	}

	got := readBaseline(t, filepath.Join(dir, baseline.Name))
	if got.Version != baseline.Version {
		t.Errorf("version is %d", got.Version)
	}
	if _, err := time.Parse(time.RFC3339, got.Generated); err != nil {
		t.Errorf("generated %q: %v", got.Generated, err)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("findings are %#v", got.Findings)
	}
	entry := got.Findings[0]
	if entry.File != "inc.go" || entry.Tenet != "comment-why" || entry.Hash == "" {
		t.Errorf("entry is %#v", entry)
	}
}

// The baseline is written where it will be committed, whatever directory the
// run was started in.
func TestBaselineWriteFromASubdirectory(t *testing.T) {
	dir := baselineRepo(t)
	if err := os.Mkdir(filepath.Join(dir, "pkg"), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(dir, "pkg"))

	code, _, stderr := runCmd(t, "baseline", "--no-cache", "..")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := readBaseline(t, filepath.Join(dir, baseline.Name))
	if len(got.Findings) != 1 || got.Findings[0].File != "inc.go" {
		t.Errorf("findings are %#v", got.Findings)
	}
}

func TestBaselineWriteToANamedFile(t *testing.T) {
	dir := baselineRepo(t)
	path := filepath.Join(dir, "accepted.json")

	code, _, stderr := runCmd(t, "baseline", "--no-cache", "--output", path, ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if len(readBaseline(t, path).Findings) != 1 {
		t.Error("the named file holds no findings")
	}
	if _, err := os.Stat(filepath.Join(dir, baseline.Name)); !os.IsNotExist(err) {
		t.Errorf("the default file was written too: %v", err)
	}
}
