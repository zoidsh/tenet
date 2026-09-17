package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/importer"
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
	_, body, found := strings.Cut(skill[len("---\n"):], "---\n")
	if !found {
		t.Fatalf("SKILL.md has no frontmatter:\n%s", skill)
	}
	if strings.TrimSpace(body) != strings.TrimSpace(importer.AgentSkill) {
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
	if hook.Command != `"${CLAUDE_PLUGIN_ROOT}"/hooks/pre-commit.sh` {
		t.Errorf("hook command is %q", hook.Command)
	}
}

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
	if manifest.Name != "tenetlint" || manifest.Version != "0.0.0" {
		t.Errorf("manifest is %#v", manifest)
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

// fakePath is a directory holding a tenet that prints what the test wants and
// exits with the code the test wants, and nothing else, so that the hook's
// view of the world is only what the test put there.
func fakePath(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if script == "" {
		return dir
	}
	path := filepath.Join(dir, "tenet")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runPreCommit(t *testing.T, path string, env ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	cmd := exec.Command("sh", filepath.Join(repoDir, "plugin", "hooks", "pre-commit.sh"))
	cmd.Dir = t.TempDir()
	cmd.Env = append([]string{"PATH=" + path}, env...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	var exit *exec.ExitError
	if err := cmd.Run(); err != nil && !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return cmd.ProcessState.ExitCode(), stdout.String(), stderr.String()
}

func TestPreCommitHookDeniesOnFindings(t *testing.T) {
	path := fakePath(t, "#!/bin/sh\necho '"+findingsJSON+"'\nexit 1\n")

	code, _, stderr := runPreCommit(t, path)
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"comment-why", "cache.go", "fix the lines above"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not mention %q:\n%s", want, stderr)
		}
	}
}

func TestPreCommitHookAllowsCleanRun(t *testing.T) {
	path := fakePath(t, "#!/bin/sh\necho '{\"findings\":[],\"next\":\"\"}'\nexit 0\n")

	code, _, stderr := runPreCommit(t, path)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestPreCommitHookAllowsWhenSkipped(t *testing.T) {
	path := fakePath(t, "#!/bin/sh\necho '"+findingsJSON+"'\nexit 1\n")

	code, _, stderr := runPreCommit(t, path, "TENETLINT_SKIP=1")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestPreCommitHookAllowsWhenBinaryIsMissing(t *testing.T) {
	code, _, stderr := runPreCommit(t, fakePath(t, ""))
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if lines := strings.Split(strings.TrimSpace(stderr), "\n"); len(lines) != 1 || !strings.Contains(lines[0], "tenet") {
		t.Errorf("stderr is %q", stderr)
	}
}
