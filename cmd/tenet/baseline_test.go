// The answer server below stands in for the API, as the one in lint_test.go
// does, so that a run can be judged without a key or the network, and the
// fixture's comment is written to violate comment-why, which is what the
// answers here are about.
// tenet:ignore-file no-mocking, comment-why
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zoidsh/tenet/internal/baseline"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/judge"
	"github.com/zoidsh/tenet/internal/tenets"
)

// marked is a file whose one violation is on a line the answer server finds by
// its text, so that a test can move the line and still be answered about the
// same line. It keeps three lines above the violation, which is as much
// context as a finding's hash takes.
const marked = `package main

import "fmt"

func show(n int) {
	fmt.Println(n)
}

func inc(n int) int {
	// bump it
	return n + 1
}
`

const (
	violation     = "// bump it"
	violationLine = 10
)

// markerServer answers that every window violates its tenet, on the line that
// holds the violation.
func markerServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			State     string                     `json:"state"`
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		answers := map[string]jev.Answer{}
		for name := range req.Questions {
			if strings.HasPrefix(name, "verdict:") {
				answers[name] = jev.Answer{Type: jev.KindNoul, Noul: 0.91}
				continue
			}
			answers[name] = jev.Answer{Type: jev.KindChoice, Probabilities: markedLabel(req.State)}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "jev-1.13.0",
			"answers": answers,
			"usage":   map[string]int{"input_tokens": 120, "output_tokens": 0},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// markedLabel is the location answer for the line the violation is on, read
// back out of the state the model was shown.
func markedLabel(state string) map[string]float64 {
	for _, line := range strings.Split(state, "\n") {
		label, text, ok := strings.Cut(line, " ")
		if ok && strings.Contains(text, violation) {
			return map[string]float64{label: 0.9, "none": 0.1}
		}
	}
	return map[string]float64{"none": 1}
}

// baselineRepo is a repository holding one file the answer server finds a
// violation in, ready for a run started in it.
func baselineRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	writeFile(t, dir, "inc.go", marked)
	git(t, dir, "add", "-A")
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, markerServer(t).URL)
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

// writeBaseline accepts everything the repository has today, which is what a
// team adopting tenetlint does first.
func writeBaseline(t *testing.T) {
	t.Helper()
	if code, _, stderr := runCmd(t, "baseline", "--no-cache", "."); code != 0 {
		t.Fatalf("baseline exit %d: %s", code, stderr)
	}
}

// found is the line a finding in the fixture is printed on.
var found = fmt.Sprintf("inc.go:%d", violationLine)

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
// run was started in, and holds the paths that file will be read against.
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

func TestLintPassesOverABaselinedFinding(t *testing.T) {
	baselineRepo(t)
	writeBaseline(t)

	code, stdout, stderr := runCmd(t, "--no-cache", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, stdout, stderr)
	}
	if strings.Contains(stdout, found) {
		t.Errorf("the baselined finding is still listed:\n%s", stdout)
	}
	if !strings.Contains(stdout, "0 findings, 1 baselined") {
		t.Errorf("summary is %q", stdout)
	}
}

func TestLintCountsBaselinedFindingsInJSON(t *testing.T) {
	baselineRepo(t)
	writeBaseline(t)

	code, stdout, stderr := runCmd(t, "--no-cache", "--format", "json", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var got jsonReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout)
	}
	if len(got.Findings) != 0 {
		t.Errorf("findings are %#v", got.Findings)
	}
	if got.Stats.Baselined != 1 {
		t.Errorf("stats counted %d baselined", got.Stats.Baselined)
	}
}

func TestLintShowsBaselinedFindings(t *testing.T) {
	baselineRepo(t)
	writeBaseline(t)

	code, stdout, stderr := runCmd(t, "--no-cache", "--show-baselined", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, found+": comment-why (p=0.91) [baselined]") {
		t.Errorf("stdout is %q", stdout)
	}
}

func TestLintIgnoresTheBaselineWhenAsked(t *testing.T) {
	baselineRepo(t)
	writeBaseline(t)

	code, stdout, _ := runCmd(t, "--no-cache", "--no-baseline", ".")
	if code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stdout)
	}
	if !strings.Contains(stdout, found) {
		t.Errorf("stdout is %q", stdout)
	}
}

// The baseline accepts a finding, not a line number: code above it may grow,
// and the same violation stays accepted.
func TestABaselinedFindingSurvivesAShift(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)
	writeFile(t, dir, "inc.go", "// Package main is a fixture.\n\n"+marked)

	code, stdout, stderr := runCmd(t, "--no-cache", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "1 baselined") {
		t.Errorf("summary is %q", stdout)
	}
}

// Editing the offending line is a new finding, whatever the baseline says
// about the line that was there.
func TestABaselinedFindingReturnsWhenItsLineIsEdited(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)
	writeFile(t, dir, "inc.go", strings.Replace(marked, violation+"\n", violation+" twice\n", 1))

	code, stdout, _ := runCmd(t, "--no-cache", ".")
	if code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stdout)
	}
	if !strings.Contains(stdout, found) {
		t.Errorf("stdout is %q", stdout)
	}
}

func TestLintWithAMissingNamedBaseline(t *testing.T) {
	baselineRepo(t)

	code, _, stderr := runCmd(t, "--no-cache", "--baseline", "nowhere.json", ".")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "nowhere.json") {
		t.Errorf("stderr is %q", stderr)
	}
}

func TestBaselinePruneDropsWhatIsFixed(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)
	writeFile(t, dir, "inc.go", strings.Replace(marked, "\t"+violation+"\n", "", 1))

	code, stdout, stderr := runCmd(t, "baseline", "--prune", "--no-cache", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dropped 1 finding") {
		t.Errorf("stdout is %q", stdout)
	}
	if got := readBaseline(t, filepath.Join(dir, baseline.Name)); len(got.Findings) != 0 {
		t.Errorf("findings are %#v", got.Findings)
	}
}

func TestBaselinePruneKeepsWhatIsStillFound(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)

	code, stdout, stderr := runCmd(t, "baseline", "--prune", "--no-cache", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dropped 0 findings") {
		t.Errorf("stdout is %q", stdout)
	}
	if got := readBaseline(t, filepath.Join(dir, baseline.Name)); len(got.Findings) != 1 {
		t.Errorf("findings are %#v", got.Findings)
	}
}

// A run that finds something new is no reason to accept it: prune only ever
// takes entries away.
func TestBaselinePruneAcceptsNothingNew(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)
	writeFile(t, dir, "dec.go", strings.ReplaceAll(marked, "inc", "dec"))

	code, _, stderr := runCmd(t, "baseline", "--prune", "--no-cache", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := readBaseline(t, filepath.Join(dir, baseline.Name))
	if len(got.Findings) != 1 || got.Findings[0].File != "inc.go" {
		t.Errorf("findings are %#v", got.Findings)
	}
}

func TestBaselinePruneWithoutABaseline(t *testing.T) {
	baselineRepo(t)

	code, _, stderr := runCmd(t, "baseline", "--prune", "--no-cache", ".")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, baseline.Name) {
		t.Errorf("stderr is %q", stderr)
	}
}

// A baseline accepts violations of a rule, not of a model, so a model bump
// must not hand the team its whole backlog back.
func TestABaselinedFindingSurvivesAModelBump(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)
	writeFile(t, dir, "tenets.yml", strings.Replace(testConfig, "jev-1.13.0", "jev-1.14.0", 1))

	code, stdout, stderr := runCmd(t, "--no-cache", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "1 baselined") {
		t.Errorf("summary is %q", stdout)
	}
}

func TestBaselineRecordsItsScope(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)

	got := readBaseline(t, filepath.Join(dir, baseline.Name)).Scope
	if got.Mode != baseline.ModePaths || len(got.Paths) != 1 || got.Paths[0] != "." {
		t.Errorf("scope is %#v", got)
	}
}

func TestBaselineRecordsABaseScope(t *testing.T) {
	dir := baselineRepo(t)
	git(t, dir, "commit", "-qm", "first")

	code, _, stderr := runCmd(t, "baseline", "--no-cache", "--base", "main")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := readBaseline(t, filepath.Join(dir, baseline.Name)).Scope
	if got.Mode != baseline.ModeBase || got.Base != "main" {
		t.Errorf("scope is %#v", got)
	}
}

// Pruning from a narrower run would drop every entry it never looked at, so
// it refuses instead.
func TestBaselinePruneRefusesANarrowerScope(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)

	code, _, stderr := runCmd(t, "baseline", "--prune", "--no-cache", "inc.go")
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, stderr)
	}
	for _, want := range []string{".", "inc.go", baseline.Name} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q does not name %s", stderr, want)
		}
	}
	if len(readBaseline(t, filepath.Join(dir, baseline.Name)).Findings) != 1 {
		t.Error("the refused prune wrote the file anyway")
	}
}

func TestBaselinePruneAcceptsAWiderScope(t *testing.T) {
	baselineRepo(t)
	if code, _, stderr := runCmd(t, "baseline", "--no-cache", "inc.go"); code != 0 {
		t.Fatalf("baseline exit %d: %s", code, stderr)
	}

	code, stdout, stderr := runCmd(t, "baseline", "--prune", "--no-cache", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dropped 0 findings") {
		t.Errorf("stdout is %q", stdout)
	}
}

// A diff against a ref and a list of paths hold different code, so neither
// covers the other.
func TestBaselinePruneRefusesAnotherMode(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)
	git(t, dir, "commit", "-qm", "first")

	code, _, stderr := runCmd(t, "baseline", "--prune", "--no-cache", "--base", "main")
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, stderr)
	}
	if !strings.Contains(stderr, "main") {
		t.Errorf("stderr is %q", stderr)
	}
}

// The baseline is committed and reviewed, so it is written whole and readable
// to everyone.
func TestBaselineIsWrittenWholeAndReadable(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)

	info, err := os.Stat(filepath.Join(dir, baseline.Name))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode is %v", info.Mode().Perm())
	}
	left, err := filepath.Glob(filepath.Join(dir, baseline.Name+".*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// refusingServer fails the test if the model is asked anything at all.
func refusingServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the model was asked about a run that was going to be refused")
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	return server
}

// A prune that is going to be refused must not be paid for first.
func TestBaselinePruneRefusesBeforeAskingTheModel(t *testing.T) {
	baselineRepo(t)
	writeBaseline(t)
	t.Setenv(jev.BaseURLEnv, refusingServer(t).URL)

	code, _, stderr := runCmd(t, "baseline", "--prune", "--no-cache", "inc.go")
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, stderr)
	}
}

// A lint pointed at a baseline that is not there says so instead of paying
// for the answers first.
func TestLintWithAMissingNamedBaselineAsksNothing(t *testing.T) {
	baselineRepo(t)
	t.Setenv(jev.BaseURLEnv, refusingServer(t).URL)

	code, _, stderr := runCmd(t, "--no-cache", "--baseline", "nowhere.json", ".")
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, stderr)
	}
}

// A file that is gone produces nothing, so prune takes its entries with it.
func TestBaselinePruneDropsADeletedFile(t *testing.T) {
	dir := baselineRepo(t)
	writeBaseline(t)
	if err := os.Remove(filepath.Join(dir, "inc.go")); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCmd(t, "baseline", "--prune", "--no-cache", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dropped 1 finding") {
		t.Errorf("stdout is %q", stdout)
	}
	if got := readBaseline(t, filepath.Join(dir, baseline.Name)); len(got.Findings) != 0 {
		t.Errorf("findings are %#v", got.Findings)
	}
}

func TestLintListsBaselinedFindingsInJSON(t *testing.T) {
	baselineRepo(t)
	writeBaseline(t)

	code, stdout, stderr := runCmd(t, "--no-cache", "--format", "json", "--show-baselined", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var got struct {
		Findings  []judge.Finding `json:"findings"`
		Baselined []judge.Finding `json:"baselined"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout)
	}
	if len(got.Findings) != 0 {
		t.Errorf("findings are %#v", got.Findings)
	}
	if len(got.Baselined) != 1 {
		t.Fatalf("baselined are %#v", got.Baselined)
	}
	want := judge.Finding{
		File: "inc.go", Line: violationLine, Tenet: "comment-why",
		Probability: 0.91, Fail: tenets.DefaultFail, Message: "A comment says why.",
	}
	if got.Baselined[0] != want {
		t.Errorf("baselined finding is %#v, want %#v", got.Baselined[0], want)
	}
}

// Without the flag there is no array at all, so that a reader can tell an
// empty one from a run that never looked.
func TestJSONHoldsNoBaselinedArrayByDefault(t *testing.T) {
	baselineRepo(t)
	writeBaseline(t)

	code, stdout, stderr := runCmd(t, "--no-cache", "--format", "json", ".")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(stdout, `"baselined": [`) {
		t.Errorf("stdout holds a baselined array:\n%s", stdout)
	}
}
