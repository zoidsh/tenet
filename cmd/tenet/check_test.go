// The examples in the config below are labelled violations of comment-why, so
// the comment that restates the code is the fixture and not a slip.
// tenet:ignore-file comment-why
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

	"github.com/zoidsh/tenetlint/internal/check"
	"github.com/zoidsh/tenetlint/internal/jev"
)

const checkConfig = `version: 1
model: jev-1.13.0
tenets:
  - id: comment-why
    tenet: A comment says why.
    include: ["**/*.go"]
    examples_from: examples/comment-why.yml
    examples:
      - label: violation
        lines: [2, 3]
        code: |
          func add(a, b int) int {
              // add a and b
              return a + b
          }
  - id: no-fallback
    tenet: No silent fallbacks.
    include: ["**/*.go"]
`

// Five more examples, so that the tenet reaches the six a verdict needs.
const checkExamplesFile = `- label: violation
  code: |
    // bump the counter
    counter++
- label: violation
  code: |
    // step one
    start()
- label: ok
  code: |
    // The API caps a page at 500.
    page(500)
- label: ok
  code: |
    // Keep this in step with the schema.
    migrate()
- label: ok
  code: |
    // The vendor SDK requires this order.
    connect()
`

// reasons are the comments the ok examples carry, which the server answers
// low for; everything else scores high, which is the shape of a tenet that
// separates its examples.
var reasons = []string{"caps a page", "in step with the schema", "vendor SDK"}

// exampleServer answers about one example per request, as check asks.
func exampleServer(t *testing.T) *httptest.Server {
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
		prob := 0.9
		for _, reason := range reasons {
			if strings.Contains(req.State, reason) {
				prob = 0.05
			}
		}
		answers := map[string]jev.Answer{}
		for name := range req.Questions {
			if name == "verdict" {
				answers[name] = jev.Answer{Type: jev.KindNoul, Noul: prob}
				continue
			}
			answers[name] = jev.Answer{
				Type:          jev.KindChoice,
				Probabilities: map[string]float64{"L003": 0.6, "L001": 0.2, "none": 0.2},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "jev-1.13.0",
			"answers": answers,
			"usage":   map[string]int{"input_tokens": 200, "output_tokens": 0},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

type jsonCheck struct {
	Version int `json:"version"`
	Tenets  []struct {
		Tenet           string  `json:"tenet"`
		Verdict         string  `json:"verdict"`
		Examples        int     `json:"examples"`
		AUC             float64 `json:"auc"`
		Accuracy        float64 `json:"accuracy"`
		LocationHits    int     `json:"location_hits"`
		LocatedExamples int     `json:"located_examples"`
		Misjudged       []struct {
			Label string `json:"label"`
		} `json:"misjudged"`
	} `json:"tenets"`
	Stats struct {
		Examples int `json:"examples"`
		Calls    int `json:"calls"`
	} `json:"stats"`
	MinExamples int `json:"min_examples"`
}

func checkDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "tenets.yml", checkConfig)
	if err := os.MkdirAll(filepath.Join(dir, "examples"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "examples/comment-why.yml", checkExamplesFile)
	return dir
}

func TestCheckEndToEnd(t *testing.T) {
	dir := checkDir(t)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, exampleServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"check", "--format", "json", "--no-cache"})

	if code := execute(root); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	var got jsonCheck
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout.String())
	}
	if got.Version != 1 || got.MinExamples != check.DefaultMinExamples {
		t.Errorf("header is %#v", got)
	}
	// Only comment-why has examples, so only it is checked.
	if len(got.Tenets) != 1 {
		t.Fatalf("checked %#v", got.Tenets)
	}
	tenet := got.Tenets[0]
	if tenet.Tenet != "comment-why" || tenet.Verdict != check.VerdictSharp {
		t.Errorf("tenet is %#v", tenet)
	}
	if tenet.Examples != 6 || tenet.AUC != 1 || tenet.Accuracy != 1 || len(tenet.Misjudged) != 0 {
		t.Errorf("numbers are %#v", tenet)
	}
	if tenet.LocatedExamples != 1 || tenet.LocationHits != 1 {
		t.Errorf("location is %d of %d", tenet.LocationHits, tenet.LocatedExamples)
	}
	if got.Stats.Examples != 6 || got.Stats.Calls != 6 {
		t.Errorf("stats are %#v", got.Stats)
	}
}

func TestCheckTextOutput(t *testing.T) {
	dir := checkDir(t)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, exampleServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"check", "comment-why", "--no-cache"})

	if code := execute(root); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"comment-why: sharp", "auc               1.00", "1 tenet over 6 examples"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout %q does not hold %q", stdout.String(), want)
		}
	}
}

func TestCheckAnUnknownTenet(t *testing.T) {
	dir := checkDir(t)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"check", "no-such-rule"})

	if code := execute(root); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "no-such-rule") || !strings.Contains(stderr.String(), "comment-why") {
		t.Errorf("stderr %q should name the id and what the config has", stderr.String())
	}
}

func TestCheckATenetWithoutExamples(t *testing.T) {
	dir := checkDir(t)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, exampleServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"check", "no-fallback", "--no-cache"})

	if code := execute(root); code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no-fallback: "+check.VerdictTooFew) {
		t.Errorf("stdout is %q", stdout.String())
	}
}

func TestCheckWithoutAKey(t *testing.T) {
	dir := checkDir(t)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "")

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"check"})

	if code := execute(root); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), jev.APIKeyEnv) {
		t.Errorf("stderr is %q", stderr.String())
	}
}
