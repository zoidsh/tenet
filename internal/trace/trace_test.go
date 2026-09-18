package trace_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/judge"
	"github.com/zoidsh/tenet/internal/trace"
)

func read(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return out
}

func names(t *testing.T, dir string) []string {
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

func sample() judge.Trace {
	return judge.Trace{
		Seq:       7,
		File:      "internal/judge/judge.go",
		First:     12,
		Last:      41,
		Tenets:    []string{"comment-why"},
		State:     "L012: // why <a & b>\n",
		Questions: map[string]jev.Question{"verdict:comment-why": jev.Noul("Rule: A comment says why.", "", "")},
		Answers:   map[string]jev.Answer{"verdict:comment-why": {Type: jev.KindNoul, Noul: 0.91}},
		Model:     "jev-1.13.0",

		InputTokens: 2169,
		CostUSD:     0.00009123,
		Duration:    760 * time.Millisecond,
	}
}

func TestWindowFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "trace")
	w, err := trace.Open(dir, 12)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Window(sample()); err != nil {
		t.Fatal(err)
	}

	name := "007-internal-judge-judge.go-00012-00041.json"
	got := read(t, filepath.Join(dir, name))
	if got["file"] != "internal/judge/judge.go" {
		t.Errorf("traced file %v", got["file"])
	}
	lines, ok := got["lines"].([]any)
	if !ok || len(lines) != 2 || lines[0] != float64(12) || lines[1] != float64(41) {
		t.Errorf("traced lines %v, want [12, 41]", got["lines"])
	}
	if got["cached"] != false || got["model"] != "jev-1.13.0" || got["tokens"] != float64(2169) {
		t.Errorf("traced %v", got)
	}
	if got["duration_ms"] != float64(760) || got["cost_usd"] != jev.RoundCost(0.00009123) {
		t.Errorf("traced cost %v and duration %v", got["cost_usd"], got["duration_ms"])
	}
	questions, _ := got["questions"].(map[string]any)
	answers, _ := got["answers"].(map[string]any)
	if _, ok := questions["verdict:comment-why"]; !ok {
		t.Errorf("traced questions %v", got["questions"])
	}
	if _, ok := answers["verdict:comment-why"]; !ok {
		t.Errorf("traced answers %v", got["answers"])
	}

	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<a & b>") {
		t.Errorf("the traced source was escaped as if for a web page:\n%s", data)
	}
}

func TestSequenceIsPaddedToTheRunsWidth(t *testing.T) {
	for _, c := range []struct {
		windows int
		want    string
	}{
		{windows: 9, want: "007-a.go-00012-00041.json"},
		{windows: 1200, want: "0007-a.go-00012-00041.json"},
	} {
		dir := filepath.Join(t.TempDir(), "trace")
		w, err := trace.Open(dir, c.windows)
		if err != nil {
			t.Fatal(err)
		}
		rec := sample()
		rec.File = "a.go"
		if err := w.Window(rec); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, c.want)); err != nil {
			t.Errorf("a run of %d windows wrote %v, want %s", c.windows, names(t, dir), c.want)
		}
	}
}

// The halves of one window share its sequence number, so a listing has only
// their line ranges to put them back in order.
func TestHalvesOfAWindowSortByTheirRange(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "trace")
	w, err := trace.Open(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, half := range [][2]int{{10, 18}, {1, 9}} {
		rec := sample()
		rec.Seq, rec.File, rec.First, rec.Last = 1, "a.go", half[0], half[1]
		if err := w.Window(rec); err != nil {
			t.Fatal(err)
		}
	}
	got := names(t, dir)
	want := []string{".gitignore", "001-a.go-00001-00009.json", "001-a.go-00010-00018.json"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the listing is %v, want %v", got, want)
	}
}

func TestRunFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "trace")
	w, err := trace.Open(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	err = w.Run(trace.Run{
		Command: []string{"tenet", "--trace"},
		Config:  ".tenet/config.yml",
		Model:   "jev-1.13.0",
		Head:    "abc123",
		Tree:    "def456",
		Stats:   judge.Stats{Files: 2, Windows: 3, Calls: 4, CacheHits: 1, InputTokens: 1200, CostUSD: 0.0012, Duration: 800 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(dir, trace.RunFile))
	if got["config"] != ".tenet/config.yml" || got["model"] != "jev-1.13.0" || got["head"] != "abc123" || got["tree"] != "def456" {
		t.Errorf("the run file says %v", got)
	}
	if _, ok := got["excused"]; ok {
		t.Errorf("a run nothing excused says excused: %v", got["excused"])
	}
	stats, _ := got["stats"].(map[string]any)
	if stats["windows"] != float64(3) || stats["calls"] != float64(4) || stats["duration_ms"] != float64(800) {
		t.Errorf("the run file's stats are %v", stats)
	}
	command, _ := got["command"].([]any)
	if len(command) != 2 || command[1] != "--trace" {
		t.Errorf("the run file's command is %v", got["command"])
	}
}

func TestExcusedRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "trace")
	w, err := trace.Open(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Run(trace.Run{Command: []string{"tenet"}, Excused: true}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, trace.RunFile)); got["excused"] != true {
		t.Errorf("an excused run says %v", got)
	}
	if got := names(t, dir); len(got) != 2 {
		t.Errorf("an excused run left %v, want the run file and the .gitignore", got)
	}
}

func TestOpenEmptiesItsOwnDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "trace")
	w, err := trace.Open(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Window(sample()); err != nil {
		t.Fatal(err)
	}

	if _, err = trace.Open(dir, 1); err != nil {
		t.Fatal(err)
	}
	if got := names(t, dir); len(got) != 1 || got[0] != ".gitignore" {
		t.Errorf("the second run found %v, want the last run's files gone", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != trace.Marker+"\n*\n" {
		t.Errorf("the .gitignore says %q", data)
	}
}

func TestOpenRefusesADirectoryTenetDidNotWrite(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(kept, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := trace.Open(dir, 1); err == nil {
		t.Fatal("a directory of somebody's own was cleared")
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("the refusal removed %s anyway", kept)
	}
}

func TestOpenRefusesAGitignoreItDidNotWrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := trace.Open(dir, 1); err == nil {
		t.Error("a .gitignore without tenet's marker was taken for tenet's own")
	}
}

func TestOpenTakesAnEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := trace.Open(dir, 1); err != nil {
		t.Fatalf("an empty directory was refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		t.Error("an empty directory was left unmarked")
	}
}
