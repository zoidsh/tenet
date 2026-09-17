package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/report"
)

const flagKey = "flag-key-0123456789abcdef"

// recorder answers every question the way answerServer does and keeps the
// Authorization header of each request, which is how a test tells which key a
// run judged with without printing one.
type recorder struct {
	*httptest.Server
	mu   sync.Mutex
	seen []string
}

func newRecorder(t *testing.T) *recorder {
	t.Helper()
	r := &recorder{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.seen = append(r.seen, req.Header.Get("Authorization"))
		r.mu.Unlock()

		var body struct {
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		answers := map[string]jev.Answer{}
		for name := range body.Questions {
			if strings.HasPrefix(name, "verdict:") {
				answers[name] = jev.Answer{Type: jev.KindNoul, Noul: 0.91}
				continue
			}
			answers[name] = jev.Answer{
				Type:          jev.KindChoice,
				Probabilities: map[string]float64{"L003": 0.8, "none": 0.2},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   jev.DefaultModel,
			"answers": answers,
			"usage":   map[string]int{"input_tokens": 120, "output_tokens": 0},
		})
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *recorder) keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

func runRoot(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	return execute(root), stdout.String(), stderr.String()
}

// stagedRepo is a repository with one file staged and a tenet to judge it by.
func stagedRepo(t *testing.T) string {
	t.Helper()
	dir, _ := authRepo(t)
	writeFile(t, dir, "tenet.yml", testConfig)
	writeFile(t, dir, "inc.go", staged)
	git(t, dir, "add", "-Af")
	return dir
}

// The flag outranks every saved key and the environment, and never appears in
// what the run prints.
func TestKeyFlagOutranksEverything(t *testing.T) {
	stagedRepo(t)
	saveTestKey(t, "saved-key-0123456789abcdef")
	t.Setenv(jev.APIKeyEnv, "env-key-0123456789abcdef")
	server := newRecorder(t)
	t.Setenv(jev.BaseURLEnv, server.URL)

	code, stdout, stderr := runRoot(t, "--typesafe-api-key", flagKey, "--no-cache", "--format", "json")
	if code != report.ExitFinding {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	seen := server.keys()
	if len(seen) == 0 {
		t.Fatal("the model was never asked")
	}
	for _, got := range seen {
		if got != "Bearer "+flagKey {
			t.Errorf("a call carried %q", got)
		}
	}
	if strings.Contains(stdout, flagKey) || strings.Contains(stderr, flagKey) {
		t.Error("the key was printed")
	}
}

func TestStatusReportsTheKeyFlagAsTheSource(t *testing.T) {
	authRepo(t)
	saveTestKey(t, "saved-key-0123456789abcdef")

	code, stdout, stderr := runRoot(t, "auth", "--status", "--typesafe-api-key", flagKey)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "typesafe: the key from --typesafe-api-key") {
		t.Errorf("stdout is %q", stdout)
	}
	if strings.Contains(stdout, flagKey) {
		t.Error("the key was printed")
	}
}

// The flag names the host, whatever the environment says.
func TestBaseURLFlagOutranksTheEnvironment(t *testing.T) {
	stagedRepo(t)
	t.Setenv(jev.APIKeyEnv, "env-key-0123456789abcdef")
	unwanted := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the host in the environment was asked")
	}))
	t.Cleanup(unwanted.Close)
	t.Setenv(jev.BaseURLEnv, unwanted.URL)
	server := newRecorder(t)

	code, _, stderr := runRoot(t, "--typesafe-base-url", server.URL, "--no-cache", "--format", "json")
	if code != report.ExitFinding {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if len(server.keys()) == 0 {
		t.Error("the host on the command line was never asked")
	}
}

func TestColorFlagOutranksNoColor(t *testing.T) {
	const red = "\x1b[31m"
	for _, c := range []struct {
		name, choice, noColor string
		want                  bool
	}{
		{"always over NO_COLOR", report.ColorAlways, "1", true},
		{"never", report.ColorNever, "", false},
		{"auto off a terminal", report.ColorAuto, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			stagedRepo(t)
			t.Setenv(jev.APIKeyEnv, "env-key-0123456789abcdef")
			t.Setenv("NO_COLOR", c.noColor)
			t.Setenv(jev.BaseURLEnv, newRecorder(t).URL)

			code, stdout, stderr := runRoot(t, "--color", c.choice, "--no-cache", "--format", "text")
			if code != report.ExitFinding {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if got := strings.Contains(stdout, red); got != c.want {
				t.Errorf("coloured %v, want %v:\n%q", got, c.want, stdout)
			}
		})
	}
}

func TestColorFlagRefusesAnythingElse(t *testing.T) {
	stagedRepo(t)
	t.Setenv(jev.APIKeyEnv, "env-key-0123456789abcdef")

	code, _, stderr := runRoot(t, "--color", "sometimes")
	if code != report.ExitError {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "--color") {
		t.Errorf("stderr is %q", stderr)
	}
}
