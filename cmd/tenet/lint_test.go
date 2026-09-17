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
	"github.com/zoidsh/tenetlint/internal/report"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

const testConfig = `version: 1
model: jev-1.13.0
tenets:
  - id: comment-why
    tenet: A comment says why.
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
	Next     string          `json:"next"`
	Stats    struct {
		Baselined   int     `json:"baselined"`
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

	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

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
		File: "inc.go", Line: 3, Tenet: "comment-why",
		Probability: 0.91, Fail: tenets.DefaultFail, Message: "A comment says why.",
	}
	if got.Findings[0] != want {
		t.Errorf("finding is %#v, want %#v", got.Findings[0], want)
	}
	// The one window costs a verdict call and a location call; tenets.yml is
	// staged too and skipped along with the key file.
	if got.Stats.Files != 1 || got.Stats.Windows != 1 || got.Stats.Calls != 2 || got.Stats.InputTokens != 240 {
		t.Errorf("stats are %#v", got.Stats)
	}
	if got.Stats.CostUSD <= 0 {
		t.Errorf("cost is %v", got.Stats.CostUSD)
	}
	skipped := map[string]string{}
	for _, s := range got.Skipped {
		skipped[s.File] = s.Reason
	}
	if len(skipped) != 2 || skipped[".env"] == "" || skipped["tenets.yml"] == "" {
		t.Errorf("skipped is %#v", got.Skipped)
	}
}

// What reads a pipe is a script or an agent, so a run nobody asked a format
// of answers in JSON.
func TestLintOnAPipeAnswersInJSON(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	writeFile(t, dir, "inc.go", staged)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)
	t.Setenv(report.FormatEnv, "")

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--no-cache", "inc.go"})

	if code := execute(root); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	var got jsonReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout.String())
	}
	if len(got.Findings) != 1 {
		t.Errorf("findings are %#v", got.Findings)
	}
}

func TestLintExplicitPath(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	writeFile(t, dir, "inc.go", staged)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--no-cache", "--format", "text", "inc.go"})

	if code := execute(root); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "inc.go:3: comment-why (p=0.91)") {
		t.Errorf("stdout is %q", stdout.String())
	}
}

func TestLintFromASubdirectory(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	if err := os.Mkdir(filepath.Join(dir, "pkg"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, filepath.Join("pkg", "inc.go"), staged)
	git(t, dir, "add", "-A")
	t.Chdir(filepath.Join(dir, "pkg"))
	t.Setenv(jev.APIKeyEnv, "test-key")

	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--no-cache", "--format", "text"})

	if code := execute(root); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	// The globs match pkg/inc.go, but the path printed is the one this
	// terminal can open.
	if !strings.HasPrefix(stdout.String(), "inc.go:3: comment-why") {
		t.Errorf("stdout is %q", stdout.String())
	}
}

// An annotation is resolved against the checkout root whatever directory the
// step ran in, which is the one path a lint prints that is not relative to the
// terminal.
func TestLintGitHubFormatFromASubdirectory(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	if err := os.Mkdir(filepath.Join(dir, "pkg"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, filepath.Join("pkg", "inc.go"), staged)
	git(t, dir, "add", "-A")
	t.Chdir(filepath.Join(dir, "pkg"))
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--no-cache", "--format", "github"})

	if code := execute(root); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	want := "::error file=pkg/inc.go,line=3,title=comment-why::A comment says why.\n"
	if !strings.HasPrefix(stdout.String(), want) {
		t.Errorf("stdout is %q, want it to start with %q", stdout.String(), want)
	}
	if !strings.Contains(stdout.String(), "1 findings") {
		t.Errorf("stdout has no summary: %q", stdout.String())
	}
}

func TestLintRejectsAnUnknownFormat(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "gitlab"})

	if code := execute(root); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), report.FormatGitHub) {
		t.Errorf("stderr %q does not name the formats", stderr.String())
	}
}

func TestLintRejectsAnUnknownTenetInADirective(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	writeFile(t, dir, "inc.go", "package main // tenet\x3aignore no-such-rule\n")
	git(t, dir, "add", "-A")
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
	for _, want := range []string{"inc.go", ":1:", "no-such-rule"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q does not mention %s", stderr.String(), want)
		}
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
	if !strings.Contains(stderr.String(), jev.APIKeyEnv) || !strings.Contains(stderr.String(), SkipEnv) {
		t.Errorf("stderr %q should name both the key and the escape hatch", stderr.String())
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
	root.SetArgs([]string{"--format", "text"})

	if code := execute(root); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "0 findings") || !strings.Contains(stdout.String(), "0 windows") {
		t.Errorf("stdout is %q", stdout.String())
	}
}

// A ref that does not exist is a typo on the command line, so the message is
// about the ref rather than about git.
func TestLintUnknownBaseRef(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", testConfig)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "config")
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	code, stdout, stderr := runCmd(t, "--base", "nope")
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s%s", code, stdout, stderr)
	}
	if got := strings.TrimSpace(stderr); got != "tenet: unknown git ref nope" {
		t.Errorf("stderr is %q", got)
	}
}

// Only a run that was about to commit tells anyone to commit again.
func TestNextLineFitsTheRun(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"staged", []string{"--no-cache", "--format", "json"}, report.NextStaged},
		{"paths", []string{"--no-cache", "--format", "json", "inc.go"}, report.Next},
		{"base", []string{"--no-cache", "--format", "json", "--base", "main"}, report.Next},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			git(t, dir, "init", "-q", "-b", "main")
			writeFile(t, dir, "tenets.yml", testConfig)
			git(t, dir, "add", "-A")
			git(t, dir, "commit", "-qm", "config")
			writeFile(t, dir, "inc.go", staged)
			git(t, dir, "add", "-A")
			t.Chdir(dir)
			t.Setenv(jev.APIKeyEnv, "test-key")
			t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

			code, stdout, stderr := runCmd(t, c.args...)
			if code != 1 {
				t.Fatalf("exit %d, want 1: %s%s", code, stdout, stderr)
			}
			var got jsonReport
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("%v in %s", err, stdout)
			}
			if got.Next != c.want {
				t.Errorf("next is %q, want %q", got.Next, c.want)
			}
		})
	}
}
