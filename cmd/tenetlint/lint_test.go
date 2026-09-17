package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/judge"
)

const testConfig = `version: 1
model: jev-1.13.0
tenets:
  - id: comment-why
    tenet: A comment says why.
    severity: error
    include: ["**/*.go"]
`

const staged = `package main

func inc(n int) int {
	return n + 1
}
`

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// answerServer stands in for the API, answering that every window violates
// its tenet on line 3.
func answerServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if strings.HasPrefix(name, "verdict:") {
				answers[name] = jev.Answer{Type: jev.KindNoul, Noul: 0.91}
				continue
			}
			answers[name] = jev.Answer{
				Type:          jev.KindChoice,
				Probabilities: map[string]float64{"L003": 0.8, "L005": 0.1, "none": 0.1},
			}
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

type jsonReport struct {
	Version  int             `json:"version"`
	Findings []judge.Finding `json:"findings"`
	Stats    struct {
		Files       int     `json:"files"`
		Windows     int     `json:"windows"`
		Calls       int     `json:"calls"`
		CacheHits   int     `json:"cache_hits"`
		InputTokens int     `json:"input_tokens"`
		CostUSD     float64 `json:"cost_usd"`
		DurationMS  int64   `json:"duration_ms"`
	} `json:"stats"`
	Skipped []struct {
		File   string `json:"file"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

func TestLintStagedEndToEnd(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	writeFile(t, dir, "inc.go", staged)
	writeFile(t, dir, ".env", "TYPESAFE_API_KEY=not-a-real-key\n")
	git(t, dir, "add", "-Af")
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	server := answerServer(t)
	restore := newAsker
	newAsker = func(key, model string) judge.Asker {
		return jev.New(key, jev.WithModel(model), jev.WithBaseURL(server.URL))
	}
	t.Cleanup(func() { newAsker = restore })

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "json", "--no-cache"})

	if code := execute(root); code != 1 {
		t.Fatalf("exit %d, want 1\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	var got jsonReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout.String())
	}
	if got.Version != 1 {
		t.Errorf("version is %d", got.Version)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("findings are %#v", got.Findings)
	}
	want := judge.Finding{
		File: "inc.go", Line: 3, Tenet: "comment-why", Severity: "error",
		Probability: 0.91, Message: "A comment says why.",
	}
	if got.Findings[0] != want {
		t.Errorf("finding is %#v, want %#v", got.Findings[0], want)
	}
	// tenets.yml is staged too: it is a window that no tenet applies to.
	if got.Stats.Files != 2 || got.Stats.Windows != 2 || got.Stats.Calls != 2 || got.Stats.InputTokens != 240 {
		t.Errorf("stats are %#v", got.Stats)
	}
	if got.Stats.CostUSD <= 0 {
		t.Errorf("cost is %v", got.Stats.CostUSD)
	}
	if len(got.Skipped) != 1 || got.Skipped[0].File != ".env" {
		t.Errorf("skipped is %#v", got.Skipped)
	}
}

func TestLintWithoutAKey(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "")

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(nil)

	if code := execute(root); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), jev.APIKeyEnv) {
		t.Errorf("stderr is %q", stderr.String())
	}
}

func TestLintWithoutAConfig(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(nil)

	if code := execute(root); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "tenets.yml") || !strings.Contains(stderr.String(), dir) {
		t.Errorf("stderr is %q, want the search path", stderr.String())
	}
}

func TestLintWithNothingStaged(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "config")
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(nil)

	if code := execute(root); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "0 findings") || !strings.Contains(stdout.String(), "0 windows") {
		t.Errorf("stdout is %q", stdout.String())
	}
}
