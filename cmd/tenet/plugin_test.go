package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/importer"
	"github.com/zoidsh/tenet/internal/tenets"
)

// repoDir is resolved while the working directory is still the package's own,
// because most tests in this package chdir into a temporary repository.
var repoDir = func() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Join(dir, "..", "..")
}()

func TestSkillMatchesTheEmbeddedInstructions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoDir, "plugin", "skills", "tenet", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(data)
	if !strings.HasPrefix(skill, "---\n") {
		t.Fatalf("SKILL.md has no frontmatter:\n%s", skill)
	}
	_, body, found := strings.Cut(skill[len("---\n"):], "---\n\n")
	if !found {
		t.Fatalf("SKILL.md has no frontmatter closed by a blank line:\n%s", skill)
	}
	// Byte-identical rather than trimmed, because the two files are edited by
	// hand and a difference of any size is the drift this test is here for.
	if body != importer.AgentSkill {
		t.Errorf("SKILL.md has drifted from internal/importer/agent_skill.md:\n%s", body)
	}
	if !strings.Contains(skill, "name: tenet") {
		t.Errorf("SKILL.md is not named tenet:\n%s", skill)
	}
}

type pluginHooks struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type          string `json:"type"`
			If            string `json:"if"`
			Command       string `json:"command"`
			StatusMessage string `json:"statusMessage"`
		} `json:"hooks"`
	} `json:"hooks"`
}

func TestPluginHooksSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoDir, "plugin", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got pluginHooks
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("hooks.json does not match the documented schema: %v", err)
	}

	matchers, ok := got.Hooks["PreToolUse"]
	if !ok || len(matchers) != 1 {
		t.Fatalf("hooks are %#v", got.Hooks)
	}
	if matchers[0].Matcher != "Bash" {
		t.Errorf("matcher is %q", matchers[0].Matcher)
	}
	if len(matchers[0].Hooks) != 1 {
		t.Fatalf("PreToolUse runs %d hooks", len(matchers[0].Hooks))
	}
	hook := matchers[0].Hooks[0]
	if hook.Type != "command" {
		t.Errorf("hook type is %q", hook.Type)
	}
	if hook.If != "Bash(git commit *)" {
		t.Errorf("hook runs on %q", hook.If)
	}
	if hook.Command != `sh "${CLAUDE_PLUGIN_ROOT}"/hooks/pre-commit.sh` {
		t.Errorf("hook command is %q", hook.Command)
	}
}

var semver = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func TestPluginManifest(t *testing.T) {
	var manifest struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Author      struct {
			Name string `json:"name"`
		} `json:"author"`
	}
	readJSON(t, filepath.Join(repoDir, "plugin", ".claude-plugin", "plugin.json"), &manifest)
	if manifest.Name != "tenet" {
		t.Errorf("manifest is %#v", manifest)
	}
	// The tag is not compared here: the test has to pass in a shallow checkout
	// and on the bump commit, which is written before its tag exists.
	if !semver.MatchString(manifest.Version) || manifest.Version == "0.0.0" {
		t.Errorf("manifest version is %q", manifest.Version)
	}
	if manifest.Description == "" || manifest.Author.Name == "" {
		t.Errorf("manifest is %#v", manifest)
	}

	var marketplace struct {
		Name    string                `json:"name"`
		Owner   struct{ Name string } `json:"owner"`
		Plugins []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"plugins"`
	}
	readJSON(t, filepath.Join(repoDir, ".claude-plugin", "marketplace.json"), &marketplace)
	if len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Name != manifest.Name {
		t.Fatalf("marketplace is %#v", marketplace)
	}
	if marketplace.Plugins[0].Source != "./plugin" {
		t.Errorf("marketplace points at %q", marketplace.Plugins[0].Source)
	}
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

const findingsJSON = `{"findings":[{"file":"cache.go","line":42,"tenet":"comment-why","probability":0.91,"fail":0.8,"message":"A comment says why."}],"next":"fix the lines above"}`

// The tool calls Claude Code hands the hook on stdin, one that commits and one
// that does not.
const (
	commitInput = `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git commit -m \"Say why\"","description":"Commit the change"}}`
	otherInput  = `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./...","description":"Run the tests with git commit in the description"}}`
)

type fakeTenet struct {
	path   string
	called string
}

// fake is a directory holding a tenet that records that it ran, prints what
// the test wants and exits with the code the test wants, and nothing else, so
// that the hook's view of the world is only what the test put there. An empty
// body leaves the directory bare, which is a machine without tenet installed.
func fake(t *testing.T, body string) fakeTenet {
	t.Helper()
	dir := t.TempDir()
	got := fakeTenet{path: dir, called: filepath.Join(dir, "called")}
	// The hook reads the tool call with grep, so grep is the one thing besides
	// tenet that the bare PATH has to carry.
	grep, err := exec.LookPath("grep")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(grep, filepath.Join(dir, "grep")); err != nil {
		t.Fatal(err)
	}
	if body == "" {
		return got
	}
	script := "#!/bin/sh\n: > " + got.called + "\n" + body
	if err := os.WriteFile(filepath.Join(dir, "tenet"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return got
}

func (f fakeTenet) ran() bool {
	_, err := os.Stat(f.called)
	return err == nil
}

func runPreCommit(t *testing.T, tenet fakeTenet, stdin string, env ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	cmd := exec.Command("sh", filepath.Join(repoDir, "plugin", "hooks", "pre-commit.sh"))
	cmd.Dir = t.TempDir()
	cmd.Env = append([]string{"PATH=" + tenet.path}, env...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	var exit *exec.ExitError
	if err := cmd.Run(); err != nil && !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return cmd.ProcessState.ExitCode(), stdout.String(), stderr.String()
}

func findingTenet(t *testing.T) fakeTenet {
	t.Helper()
	return fake(t, "echo '"+findingsJSON+"'\nexit 1\n")
}

func TestPreCommitHookDeniesOnFindings(t *testing.T) {
	tenet := findingTenet(t)

	code, _, stderr := runPreCommit(t, tenet, commitInput)
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"comment-why", "cache.go", "fix the lines above"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not mention %q:\n%s", want, stderr)
		}
	}
}

func TestPreCommitHookIgnoresACallThatDoesNotCommit(t *testing.T) {
	tenet := findingTenet(t)

	code, _, stderr := runPreCommit(t, tenet, otherInput)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if tenet.ran() {
		t.Error("the lint ran on a tool call that does not commit")
	}
}

func TestPreCommitHookAllowsCleanRun(t *testing.T) {
	tenet := fake(t, "echo '{\"findings\":[],\"next\":\"\"}'\nexit 0\n")

	code, _, stderr := runPreCommit(t, tenet, commitInput)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !tenet.ran() {
		t.Error("the lint did not run on a commit")
	}
}

func TestPreCommitHookAllowsWhenSkipped(t *testing.T) {
	tenet := findingTenet(t)

	code, _, stderr := runPreCommit(t, tenet, commitInput, "TENET_SKIP=1")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if tenet.ran() {
		t.Error("the lint ran although TENET_SKIP was set")
	}
}

func TestPreCommitHookAllowsWhenTheRepositoryHasNoConfig(t *testing.T) {
	tenet := fake(t, "echo 'tenet: no .tenet/config.yml found' >&2\nexit 3\n") // tenet:ignore no-mocking the subject is the hook script, and a stub tenet on PATH is the only way to drive it through every exit code the real binary can return

	code, _, stderr := runPreCommit(t, tenet, commitInput)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, tenets.FileName) {
		t.Errorf("stderr is %q", stderr)
	}
	if !tenet.ran() {
		t.Error("the lint did not run on a commit")
	}
}

func TestPreCommitHookAllowsWhenBinaryIsMissing(t *testing.T) {
	// The subject is a shell script whose contract is what it does with the
	// tenet it finds on PATH, and the real binary would put a paid call in a
	// default test run. tenet:ignore-next-line no-mocking
	code, _, stderr := runPreCommit(t, fake(t, ""), commitInput)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if lines := strings.Split(strings.TrimSpace(stderr), "\n"); len(lines) != 1 || !strings.Contains(lines[0], "tenet") {
		t.Errorf("stderr is %q", stderr)
	}
}
