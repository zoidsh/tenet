package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/cache"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/report"
	"github.com/zoidsh/tenet/internal/source"
	"github.com/zoidsh/tenet/internal/trace"
)

// traceRepo is a staged repository whose windows the model finds fault with,
// with a cache of its own so that every run asks and every run traces.
func traceRepo(t *testing.T) string {
	t.Helper()
	dir := stagedRepo(t)
	t.Setenv(cache.DirEnv, t.TempDir())
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)
	return dir
}

func traceDir(dir string) string {
	return filepath.Join(dir, source.ConfigDir, trace.DirName)
}

func traced(t *testing.T, path string) map[string]any {
	t.Helper()
	var out map[string]any
	readJSON(t, path, &out)
	return out
}

func listed(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestTraceWritesAFilePerWindowAndARunFile(t *testing.T) {
	dir := traceRepo(t)

	code, _, stderr := runRoot(t, "--format", "json", "--no-cache", "--trace")
	if code != report.ExitFinding {
		t.Fatalf("exit %d: %s", code, stderr)
	}

	got := listed(t, traceDir(dir))
	want := []string{".gitignore", "001-inc.go-00001-00005.json", trace.RunFile}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("the trace holds %v, want %v", got, want)
	}

	window := traced(t, filepath.Join(traceDir(dir), "001-inc.go-00001-00005.json"))
	if window["file"] != "inc.go" || window["cached"] != false || window["model"] != "jev-1.13.0" {
		t.Errorf("the window file says %v", window)
	}
	if state, _ := window["state"].(string); !strings.Contains(state, "func inc") {
		t.Errorf("the traced state is %q, want the lines that were judged", state)
	}
	questions, _ := window["questions"].(map[string]any)
	answers, _ := window["answers"].(map[string]any)
	if _, ok := questions["verdict:comment-why"]; !ok {
		t.Errorf("the traced questions are %v", questions)
	}
	if _, ok := answers["where:comment-why"]; !ok {
		t.Errorf("the traced answers are %v", answers)
	}

	run := traced(t, filepath.Join(traceDir(dir), trace.RunFile))
	if run["model"] != "jev-1.13.0" {
		t.Errorf("the run file says %v", run)
	}
	if config, _ := run["config"].(string); !strings.HasSuffix(config, source.ConfigFile) {
		t.Errorf("the run file names the config as %q", config)
	}
	if command, _ := run["command"].([]any); len(command) == 0 {
		t.Errorf("the run file names no command")
	}
	if tree, _ := run["tree"].(string); tree == "" {
		t.Errorf("a staged run traced no tree: %v", run)
	}
	stats, _ := run["stats"].(map[string]any)
	if stats["windows"] != float64(1) || stats["calls"] != float64(2) {
		t.Errorf("the run file's stats are %v", stats)
	}
}

// A trace is written to be handed to somebody, so the key a flag carried is
// the one part of the command line it does not repeat.
func TestTraceRedactsAKeyGivenOnTheCommandLine(t *testing.T) {
	const key = "flag-key-0123456789abcdef"
	for _, args := range [][]string{
		{"--typesafe-api-key", key},
		{"--typesafe-api-key=" + key},
	} {
		dir := traceRepo(t)
		if code, _, stderr := runRoot(t, append([]string{"--format", "json", "--no-cache", "--trace"}, args...)...); code != report.ExitFinding {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		data, err := os.ReadFile(filepath.Join(traceDir(dir), trace.RunFile))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), key) {
			t.Errorf("%v put the key in the run file:\n%s", args, data)
		}
		if !strings.Contains(string(data), "typesafe-api-key") {
			t.Errorf("%v left the flag out of the run file altogether:\n%s", args, data)
		}
	}
}

// The trace holds source text, so the directory it lands in keeps git away
// from it however the run named that directory.
func TestTraceDirIsNamedAndEmptied(t *testing.T) {
	dir := traceRepo(t)
	named := filepath.Join(t.TempDir(), "elsewhere")

	if code, _, stderr := runRoot(t, "--format", "json", "--no-cache", "--trace-dir", named); code != report.ExitFinding {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(traceDir(dir)); err == nil {
		t.Error("--trace-dir wrote the default directory as well")
	}
	gitignore, err := os.ReadFile(filepath.Join(named, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gitignore) != trace.Marker+"\n*\n" {
		t.Errorf("the .gitignore says %q", gitignore)
	}

	stale := filepath.Join(named, "000-gone.json")
	if err := os.WriteFile(stale, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runRoot(t, "--format", "json", "--no-cache", "--trace-dir", named); code != report.ExitFinding {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("the second run left the first run's files behind")
	}
}

func TestTraceDirRefusesADirectoryTenetDidNotWrite(t *testing.T) {
	traceRepo(t)
	named := t.TempDir()
	kept := filepath.Join(named, "notes.txt")
	if err := os.WriteFile(kept, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := runRoot(t, "--format", "json", "--no-cache", "--trace-dir", named)
	if code != report.ExitError {
		t.Fatalf("exit %d, want %d: %s", code, report.ExitError, stderr)
	}
	if !strings.Contains(stderr, "tenet did not write") {
		t.Errorf("the refusal says %q", stderr)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("the refused run removed %s anyway", kept)
	}
}

// A run a receipt excuses judges nothing, so its trace is the run file alone,
// saying what answered it.
func TestTraceOfAnExcusedRun(t *testing.T) {
	dir, _ := receiptRepo(t)

	if code, _, stderr := runRoot(t, "--format", "json", "--trace"); code != report.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if code, _, stderr := runRoot(t, "--format", "json", "--trace"); code != report.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}

	if got := listed(t, traceDir(dir)); strings.Join(got, " ") != ".gitignore "+trace.RunFile {
		t.Fatalf("the excused run's trace holds %v", got)
	}
	run := traced(t, filepath.Join(traceDir(dir), trace.RunFile))
	if run["excused"] != true {
		t.Errorf("the run file says %v", run)
	}
}
