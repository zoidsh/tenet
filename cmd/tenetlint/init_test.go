package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/importer"
	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

const rules = `# Rules

## Comments

A comment says why the code exists, not what it does.

- Run ` + "`mise install`" + ` before you run the tests.
`

const commentRule = "A comment says why the code exists, not what it does."

// sortServer stands in for the API, calling every sentence about comments a
// code rule and everything else process.
func sortServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Questions map[string]jev.Question `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		answers := map[string]jev.Answer{}
		for name, q := range req.Questions {
			rule := strings.Contains(q.Instructions, "comment")
			if strings.HasPrefix(name, "kind:") {
				kind, p := importer.KindProcess, 0.77
				if rule {
					kind, p = importer.KindCodeRule, 0.93
				}
				answers[name] = jev.Answer{
					Type:          jev.KindChoice,
					Probabilities: map[string]float64{kind: p, importer.KindOther: 1 - p},
				}
				continue
			}
			noul := 0.12
			if rule {
				noul = 0.88
			}
			answers[name] = jev.Answer{Type: jev.KindNoul, Noul: noul}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "jev-1.13.0",
			"answers": answers,
			"usage":   map[string]int{"input_tokens": 300, "output_tokens": 0},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// initRepo is a repository holding one rule file and nothing else.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "CLAUDE.md", rules)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, sortServer(t).URL)
	return dir
}

func runInitCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"init", "--no-cache"}, args...))
	return execute(root), stdout.String(), stderr.String()
}

func TestInitEndToEnd(t *testing.T) {
	dir := initRepo(t)

	code, stdout, stderr := runInitCmd(t)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}

	cfg, err := tenets.Load(filepath.Join(dir, "tenets.yml"))
	if err != nil {
		t.Fatalf("the drafted file does not load: %v", err)
	}
	if len(cfg.Tenets) != 1 {
		t.Fatalf("drafted %d tenets", len(cfg.Tenets))
	}
	got := cfg.Tenets[0]
	if got.ID != "comment-says-why-code-exists" || got.Tenet != commentRule {
		t.Errorf("tenet is %#v", got)
	}
	if got.Source != "CLAUDE.md:5" || got.Severity != tenets.SeverityWarn {
		t.Errorf("source is %q, severity %q", got.Source, got.Severity)
	}

	for _, want := range []string{
		"CLAUDE.md",
		"code-rule",
		"process",
		"comment-says-why-code-exists",
		"2 candidates, 1 tenets written to tenets.yml",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout does not mention %q:\n%s", want, stdout)
		}
	}
}

type jsonImport struct {
	Candidates []struct {
		File       string  `json:"file"`
		Line       int     `json:"line"`
		Heading    string  `json:"heading"`
		Text       string  `json:"text"`
		Kind       string  `json:"kind"`
		KindP      float64 `json:"kind_p"`
		CheckableP float64 `json:"checkable_p"`
		Accepted   bool    `json:"accepted"`
	} `json:"candidates"`
	Written *string `json:"written"`
	Stats   struct {
		Candidates  int     `json:"candidates"`
		Tenets      int     `json:"tenets"`
		Calls       int     `json:"calls"`
		CacheHits   int     `json:"cache_hits"`
		InputTokens int     `json:"input_tokens"`
		CostUSD     float64 `json:"cost_usd"`
		DurationMS  int64   `json:"duration_ms"`
	} `json:"stats"`
}

func TestInitJSON(t *testing.T) {
	initRepo(t)

	code, stdout, stderr := runInitCmd(t, "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var got jsonImport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout)
	}
	if len(got.Candidates) != 2 {
		t.Fatalf("candidates are %#v", got.Candidates)
	}
	first := got.Candidates[0]
	if first.File != "CLAUDE.md" || first.Line != 5 || first.Heading != "Rules > Comments" || first.Text != commentRule {
		t.Errorf("first candidate is %#v", first)
	}
	if first.Kind != importer.KindCodeRule || first.KindP != 0.93 || first.CheckableP != 0.88 || !first.Accepted {
		t.Errorf("first candidate sorted as %#v", first)
	}
	if second := got.Candidates[1]; second.Kind != importer.KindProcess || second.Accepted {
		t.Errorf("second candidate is %#v", second)
	}
	if got.Written == nil || *got.Written != "tenets.yml" {
		t.Errorf("written is %v", got.Written)
	}
	if got.Stats.Candidates != 2 || got.Stats.Tenets != 1 || got.Stats.Calls != 1 || got.Stats.InputTokens != 300 {
		t.Errorf("stats are %#v", got.Stats)
	}
	if got.Stats.CostUSD <= 0 {
		t.Errorf("cost is %v", got.Stats.CostUSD)
	}
}

func TestInitDryRunWritesNothing(t *testing.T) {
	dir := initRepo(t)

	code, stdout, stderr := runInitCmd(t, "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "tenets.yml")); !os.IsNotExist(err) {
		t.Errorf("a dry run wrote the file: %v", err)
	}
	if !strings.Contains(stdout, "nothing written (--dry-run)") {
		t.Errorf("stdout is %s", stdout)
	}
}

func TestInitRefusesAnExistingConfig(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "tenets.yml", testConfig)

	code, _, stderr := runInitCmd(t)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "tenets.yml") || !strings.Contains(stderr, "--force") {
		t.Errorf("stderr is %q", stderr)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "tenets.yml")); err != nil || string(data) != testConfig {
		t.Errorf("the existing file was touched: %v", err)
	}

	if code, _, stderr := runInitCmd(t, "--force"); code != 0 {
		t.Fatalf("exit %d with --force: %s", code, stderr)
	}
	cfg, err := tenets.Load(filepath.Join(dir, "tenets.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tenets[0].Tenet != commentRule {
		t.Errorf("--force left %#v", cfg.Tenets[0])
	}
}

func TestInitWithoutAKey(t *testing.T) {
	initRepo(t)
	t.Setenv(jev.APIKeyEnv, "")

	code, _, stderr := runInitCmd(t)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, jev.APIKeyEnv) || !strings.Contains(stderr, SkipEnv) {
		t.Errorf("stderr %q should name both the key and the escape hatch", stderr)
	}
}

func TestInitFromNamedFiles(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "other.md", "- Never mock a comment in a test file.\n")

	if code, _, stderr := runInitCmd(t, "--from", "other.md"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	cfg, err := tenets.Load(filepath.Join(dir, "tenets.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tenets) != 1 || cfg.Tenets[0].Source != "other.md:1" {
		t.Errorf("drafted %#v", cfg.Tenets)
	}
}

func TestInitStarterWhenNothingIsFound(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	code, stdout, stderr := runInitCmd(t)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "tenets.yml") {
		t.Errorf("stdout does not say where it wrote: %q", stdout)
	}
	cfg, err := tenets.Load(filepath.Join(dir, "tenets.yml"))
	if err != nil {
		t.Fatalf("the starter does not load: %v", err)
	}
	if len(cfg.Tenets) != 1 || cfg.Tenets[0].ID != "comment-why" {
		t.Errorf("the starter holds %#v", cfg.Tenets)
	}
}
