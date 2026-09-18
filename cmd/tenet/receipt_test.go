package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zoidsh/tenet/internal/cache"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/receipt"
	"github.com/zoidsh/tenet/internal/tenets"
)

// cleanServer answers that every window is innocent and counts the requests,
// which is how these tests tell a run that asked the model from one a receipt
// excused.
type cleanServer struct {
	*httptest.Server
	calls atomic.Int64
}

func newCleanServer(t *testing.T) *cleanServer {
	t.Helper()
	s := &cleanServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		var req struct {
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		answers := map[string]jev.Answer{}
		for name := range req.Questions {
			answers[name] = jev.Answer{Type: jev.KindNoul, Noul: 0.02}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   jev.DefaultModel,
			"answers": answers,
			"usage":   map[string]int{"input_tokens": 120, "output_tokens": 0},
		})
	}))
	t.Cleanup(s.Close)
	return s
}

// receiptRepo is a repository with a config and one staged file. These tests
// count the calls a run makes, so the answer cache is pointed away from the
// developer's own and every run starts cold.
func receiptRepo(t *testing.T) (dir string, server *cleanServer) {
	t.Helper()
	dir = t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeConfigFile(t, dir, testConfig)
	writeFile(t, dir, "inc.go", staged)
	git(t, dir, "add", "-f", "inc.go")
	t.Chdir(dir)
	t.Setenv(cache.DirEnv, t.TempDir())
	t.Setenv(jev.APIKeyEnv, "test-key")
	server = newCleanServer(t)
	t.Setenv(jev.BaseURLEnv, server.URL)
	return dir, server
}

func receiptPath(t *testing.T, dir string) string {
	t.Helper()
	return filepath.Join(dir, ".git", "tenet-receipt")
}

// lintJSON runs the staged lint and reads the report back.
func lintJSON(t *testing.T, args ...string) jsonReport {
	t.Helper()
	code, stdout, stderr := runRoot(t, append([]string{"--format", "json"}, args...)...)
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	var got jsonReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout)
	}
	return got
}

// A clean staged run leaves a receipt, and the next run on the same tree is
// excused by it without asking the model anything.
func TestReceiptExcusesAnUnchangedStagedTree(t *testing.T) {
	dir, server := receiptRepo(t)

	first := lintJSON(t)
	if server.calls.Load() == 0 {
		t.Fatal("the first run never asked the model")
	}
	if first.Stats.Receipt {
		t.Error("the first run claimed a receipt")
	}
	if first.Stats.Windows == 0 {
		t.Errorf("the first run judged nothing: %#v", first.Stats)
	}
	if receipt.Load(receiptPath(t, dir)) == "" {
		t.Fatal("the clean run left no receipt")
	}

	asked := server.calls.Load()
	second := lintJSON(t)
	if got := server.calls.Load(); got != asked {
		t.Errorf("the second run made %d calls", got-asked)
	}
	if !second.Stats.Receipt {
		t.Errorf("stats are %#v", second.Stats)
	}
	if second.Stats.Windows != 0 || second.Stats.Files != 0 || second.Stats.Calls != 0 {
		t.Errorf("stats are %#v", second.Stats)
	}
	if len(second.Findings) != 0 {
		t.Errorf("findings are %#v", second.Findings)
	}
}

func TestReceiptSummaryLine(t *testing.T) {
	receiptRepo(t)
	if code, _, stderr := runRoot(t, "--format", "text"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	code, stdout, stderr := runRoot(t, "--format", "text")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "0 findings · unchanged since the last clean run" {
		t.Errorf("stdout is %q", stdout)
	}

	code, stdout, stderr = runRoot(t, "--format", "text", "--quiet")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("a quiet run printed %q", stdout)
	}
}

// The receipt speaks for one tree judged by one config, so a change to either
// sends the run back to the model.
func TestReceiptIsSpentByAChange(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"the staged content": func(t *testing.T, dir string) {
			writeFile(t, dir, "inc.go", staged+"\nfunc dec(n int) int { return n - 1 }\n")
			git(t, dir, "add", "-f", "inc.go")
		},
		"the config": func(t *testing.T, dir string) {
			writeConfigFile(t, dir, strings.Replace(testConfig, "A comment says why.", "A comment says why it is here.", 1))
		},
		"the model": func(*testing.T, string) {},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			dir, server := receiptRepo(t)
			lintJSON(t)
			asked := server.calls.Load()

			change(t, dir)
			args := []string{}
			if name == "the model" {
				args = append(args, "--model", "jev-1.12.0")
			}
			got := lintJSON(t, args...)
			if got.Stats.Receipt {
				t.Error("the receipt excused a run it does not speak for")
			}
			if server.calls.Load() == asked {
				t.Error("the model was never asked again")
			}
		})
	}
}

// --no-cache is a run that wants fresh answers, which is a run that neither
// reads a receipt nor leaves one.
func TestNoCacheIgnoresTheReceipt(t *testing.T) {
	dir, server := receiptRepo(t)
	lintJSON(t)
	asked := server.calls.Load()

	const stale = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := receipt.Save(receiptPath(t, dir), stale); err != nil {
		t.Fatal(err)
	}
	got := lintJSON(t, "--no-cache")
	if got.Stats.Receipt {
		t.Error("--no-cache was excused by a receipt")
	}
	if server.calls.Load() == asked {
		t.Error("--no-cache asked the model nothing")
	}
	if held := receipt.Load(receiptPath(t, dir)); held != stale {
		t.Errorf("--no-cache wrote the receipt: %q", held)
	}
}

// A run that found something leaves no receipt: the next run must judge the
// tree again rather than be told it was clean.
func TestAFindingLeavesNoReceipt(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeConfigFile(t, dir, testConfig)
	writeFile(t, dir, "inc.go", staged)
	git(t, dir, "add", "-f", "inc.go")
	t.Chdir(dir)
	t.Setenv(cache.DirEnv, t.TempDir())
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

	if code, _, stderr := runRoot(t, "--format", "json"); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr)
	}
	if held := receipt.Load(receiptPath(t, dir)); held != "" {
		t.Errorf("a run with a finding left the receipt %q", held)
	}
}

// Only the default staged run has a receipt to go by: every other mode judges
// something the index does not say.
func TestOtherModesLeaveNoReceipt(t *testing.T) {
	text := t.TempDir()
	msg := filepath.Join(text, "COMMIT_EDITMSG")
	if err := os.WriteFile(msg, []byte("Add a receipt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pr := filepath.Join(text, "PULL_REQUEST")
	if err := os.WriteFile(pr, []byte("Add a receipt\n\nIt saves the second lint of a commit.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"paths":      {"inc.go"},
		"base":       {"--base", "main"},
		"commit msg": {"--commit-msg", msg},
		"pr text":    {"--pr-text", pr},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			dir, _ := receiptRepo(t)
			git(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qm", "staged")
			lintJSON(t, args...)
			if held := receipt.Load(receiptPath(t, dir)); held != "" {
				t.Errorf("a run over %s left the receipt %q", name, held)
			}
		})
	}
}

// A repository whose config names examples in a sibling file is judged by that
// file too, so editing it spends the receipt. The answers are still cached,
// since an example never enters a tenet's hash: what the edit costs is the
// collecting and the windows, not the calls.
func TestReceiptIsSpentByExamples(t *testing.T) {
	dir, _ := receiptRepo(t)
	writeConfigFile(t, dir, testConfig+"    examples_from: examples/comment-why.yml\n")
	writeFile(t, dir, filepath.Join(filepath.Dir(tenets.FileName), "examples", "comment-why.yml"),
		"- label: violation\n  code: \"x := 1 // set x\"\n")
	lintJSON(t)

	writeFile(t, dir, filepath.Join(filepath.Dir(tenets.FileName), "examples", "comment-why.yml"),
		"- label: violation\n  code: \"y := 2 // set y\"\n")
	got := lintJSON(t)
	if got.Stats.Receipt {
		t.Error("the receipt survived an edit to the examples")
	}
	if got.Stats.Windows == 0 {
		t.Errorf("nothing was judged again: %#v", got.Stats)
	}
}
