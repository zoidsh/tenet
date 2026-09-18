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

	"github.com/zoidsh/tenet/internal/auth"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/report"
)

const testKey = "ts-key-0123456789abcdef"

// authRepo is a repository to save a key in, with the config home somewhere
// empty and the environment holding no key of its own.
func authRepo(t *testing.T) (dir, config string) {
	t.Helper()
	dir, config = t.TempDir(), t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv(jev.APIKeyEnv, "")
	t.Chdir(dir)
	return dir, config
}

// verifyServer stands in for the API while a key is checked, answering with
// the status the test is about.
func verifyServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("authorization header is %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if status != http.StatusOK {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid api key"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   jev.DefaultModel,
			"answers": map[string]jev.Answer{"verify": {Type: jev.KindNoul, Noul: 0.99}},
			"usage":   map[string]int{"input_tokens": 4, "output_tokens": 0},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func runAuthCmd(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"auth"}, args...))
	return execute(root), stdout.String(), stderr.String()
}

func globalFile(config string) string {
	return filepath.Join(config, "tenet", auth.FileName)
}

// While one provider works there is nothing to choose between, so a bare
// tenet auth is the whole of signing in.
func TestAuthWithNoProviderTakesTheOnlyOne(t *testing.T) {
	_, config := authRepo(t)
	t.Setenv(jev.BaseURLEnv, verifyServer(t, http.StatusOK).URL)

	code, stdout, stderr := runAuthCmd(t, testKey+"\n")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, globalFile(config)) {
		t.Errorf("stdout does not say where the key went: %q", stdout)
	}
	key, err := auth.KeyIn(globalFile(config), "typesafe")
	if err != nil {
		t.Fatal(err)
	}
	if key != testKey {
		t.Errorf("the saved key is %q", key)
	}
}

func TestAuthRefusesTheHostedServiceUntilItExists(t *testing.T) {
	_, config := authRepo(t)

	code, _, stderr := runAuthCmd(t, "", "tenet", "--key", testKey, "--no-verify")
	if code != report.ExitError {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "not available yet") {
		t.Errorf("stderr is %q", stderr)
	}
	if _, err := os.Stat(globalFile(config)); !os.IsNotExist(err) {
		t.Errorf("a key was saved anyway: %v", err)
	}
}

func TestAuthSavesAKeyFromTheFlag(t *testing.T) {
	_, config := authRepo(t)
	t.Setenv(jev.BaseURLEnv, verifyServer(t, http.StatusOK).URL)

	code, stdout, stderr := runAuthCmd(t, "", "typesafe", "--key", testKey)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	path := globalFile(config)
	if !strings.Contains(stdout, path) {
		t.Errorf("stdout does not say where the key went: %q", stdout)
	}
	if strings.Contains(stdout, testKey) {
		t.Errorf("stdout printed the key: %q", stdout)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "typesafe="+testKey+"\n" {
		t.Errorf("the file holds %q", data)
	}
}

func TestAuthReadsAKeyOnStdin(t *testing.T) {
	_, config := authRepo(t)
	t.Setenv(jev.BaseURLEnv, verifyServer(t, http.StatusOK).URL)

	code, _, stderr := runAuthCmd(t, testKey+"\n", "typesafe")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	key, err := auth.KeyIn(globalFile(config), "typesafe")
	if err != nil {
		t.Fatal(err)
	}
	if key != testKey {
		t.Errorf("the saved key is %q", key)
	}
}

func TestAuthRejectsAKeyThatCannotBeOne(t *testing.T) {
	_, config := authRepo(t)

	code, _, stderr := runAuthCmd(t, "", "typesafe", "--key", "short", "--no-verify")
	if code != report.ExitError {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "at least 20") {
		t.Errorf("stderr is %q", stderr)
	}
	if _, err := os.Stat(globalFile(config)); !os.IsNotExist(err) {
		t.Errorf("the key was saved anyway: %v", err)
	}
}

// --key "$SOME_UNSET_VARIABLE" is a mistake worth a word, not a reason to
// start asking for a key.
func TestAuthRejectsAnEmptyKeyFlag(t *testing.T) {
	_, config := authRepo(t)

	code, _, stderr := runAuthCmd(t, testKey+"\n", "typesafe", "--key", "", "--no-verify")
	if code != report.ExitError {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "empty key") {
		t.Errorf("stderr is %q", stderr)
	}
	if _, err := os.Stat(globalFile(config)); !os.IsNotExist(err) {
		t.Errorf("something was saved anyway: %v", err)
	}
}

func TestAuthDoesNotSaveAKeyTheProviderRejects(t *testing.T) {
	_, config := authRepo(t)
	t.Setenv(jev.BaseURLEnv, verifyServer(t, http.StatusUnauthorized).URL)

	code, _, stderr := runAuthCmd(t, "", "typesafe", "--key", testKey)
	if code != report.ExitError {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "rejected") {
		t.Errorf("stderr is %q", stderr)
	}
	if _, err := os.Stat(globalFile(config)); !os.IsNotExist(err) {
		t.Errorf("the rejected key was saved: %v", err)
	}
}

func TestAuthProjectScopeKeepsTheKeyOutOfTheCommit(t *testing.T) {
	dir, _ := authRepo(t)

	code, stdout, stderr := runAuthCmd(t, "", "typesafe", "--key", testKey, "--no-verify", "--project")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	entry := auth.Dir + "/" + auth.FileName
	if !strings.Contains(stdout, entry) || !strings.Contains(stdout, ".gitignore") {
		t.Errorf("stdout is %q", stdout)
	}
	ignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(ignore) != entry+"\n" {
		t.Errorf(".gitignore holds %q", ignore)
	}
}

// A key saved while the environment holds one is a key nothing would use, so
// the save says which one wins.
func TestAuthSaysWhenTheEnvironmentWins(t *testing.T) {
	authRepo(t)
	t.Setenv(jev.APIKeyEnv, "env-key-0123456789abcdef")

	code, stdout, stderr := runAuthCmd(t, "", "typesafe", "--key", testKey, "--no-verify")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, jev.APIKeyEnv) || !strings.Contains(stdout, "instead") {
		t.Errorf("stdout is %q", stdout)
	}
}

func TestAuthStatusWithoutAKeyExitsOne(t *testing.T) {
	dir, config := authRepo(t)

	code, stdout, stderr := runAuthCmd(t, "", "--status")
	if code != report.ExitFinding {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr is %q", stderr)
	}
	for _, want := range []string{
		"typesafe: none",
		"tenet: none",
		jev.APIKeyEnv + ": not set",
		filepath.Join(dir, auth.Dir, auth.FileName),
		globalFile(config),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout does not hold %q:\n%s", want, stdout)
		}
	}
}

func TestAuthStatusNamesTheSourceButNotTheKey(t *testing.T) {
	_, config := authRepo(t)
	saveTestKey(t, testKey)

	code, stdout, stderr := runAuthCmd(t, "", "--status")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "typesafe: the key from the global file") {
		t.Errorf("stdout is %q", stdout)
	}
	if !strings.Contains(stdout, globalFile(config)+": a key") {
		t.Errorf("stdout does not say which file holds it:\n%s", stdout)
	}
	if strings.Contains(stdout, testKey) {
		t.Errorf("stdout printed the key:\n%s", stdout)
	}
}

// The environment outranks a saved key, and status says so rather than naming
// the file a run is not reading.
func TestAuthStatusPrefersTheEnvironment(t *testing.T) {
	authRepo(t)
	saveTestKey(t, testKey)
	t.Setenv(jev.APIKeyEnv, "env-key-0123456789abcdef")

	code, stdout, stderr := runAuthCmd(t, "", "--status")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "typesafe: the key from the environment") {
		t.Errorf("stdout is %q", stdout)
	}
}

func TestAuthRemove(t *testing.T) {
	_, config := authRepo(t)
	saveTestKey(t, testKey)

	code, stdout, stderr := runAuthCmd(t, "", "typesafe", "--remove")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "removed") {
		t.Errorf("stdout is %q", stdout)
	}
	if _, err := os.Stat(globalFile(config)); !os.IsNotExist(err) {
		t.Errorf("the file is still there: %v", err)
	}

	code, stdout, stderr = runAuthCmd(t, "", "typesafe", "--remove")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "no typesafe key") {
		t.Errorf("stdout is %q", stdout)
	}
}

func TestAuthRefusesFlagsThatContradict(t *testing.T) {
	authRepo(t)
	for _, args := range [][]string{
		{"--status", "--remove"},
		{"--status", "--key", testKey},
		{"typesafe", "--remove", "--no-verify"},
	} {
		code, _, stderr := runAuthCmd(t, "", args...)
		if code != report.ExitError {
			t.Errorf("%v exited %d: %s", args, code, stderr)
		}
	}
}

// A lint with nothing in the environment is what a saved key is for.
func TestLintReadsTheKeyFromTheGlobalFile(t *testing.T) {
	dir, _ := authRepo(t)
	saveTestKey(t, testKey)
	writeConfigFile(t, dir, testConfig)
	writeFile(t, dir, "inc.go", staged)
	git(t, dir, "add", "-Af")
	t.Setenv(jev.BaseURLEnv, answerServer(t).URL)

	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "json", "--no-cache"})

	if code := execute(root); code != report.ExitFinding {
		t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	var got jsonReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout.String())
	}
	if len(got.Findings) != 1 {
		t.Errorf("findings are %#v", got.Findings)
	}
}

// saveTestKey puts a key in the global file without asking the API about it.
func saveTestKey(t *testing.T, key string) {
	t.Helper()
	if code, _, stderr := runAuthCmd(t, "", "typesafe", "--key", key, "--no-verify"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}
