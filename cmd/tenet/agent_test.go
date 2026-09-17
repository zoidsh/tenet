package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/importer"
	"github.com/zoidsh/tenetlint/internal/report"
)

// agentRepo is a repository with nothing in it, because writing the
// instructions reads neither a rule file nor the API.
func agentRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)
	return dir
}

func TestInitAgentCursor(t *testing.T) {
	dir := agentRepo(t)

	code, stdout, stderr := runInitCmd(t, "--agent", "cursor")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, ".cursor/rules/tenet.mdc") {
		t.Errorf("stdout is %q", stdout)
	}
	got := read(t, filepath.Join(dir, ".cursor", "rules", "tenet.mdc"))
	if !strings.HasPrefix(got, "---\n") || !strings.Contains(got, "alwaysApply: true") {
		t.Errorf("the rule has no frontmatter that always applies:\n%s", got)
	}
	if !strings.Contains(got, importer.AgentSkill) {
		t.Errorf("the rule does not carry the instructions:\n%s", got)
	}
}

func TestInitAgentCreatesSection(t *testing.T) {
	dir := agentRepo(t)

	code, _, stderr := runInitCmd(t, "--agent", "agents")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := read(t, filepath.Join(dir, "AGENTS.md"))
	if got != importer.AgentHeading+"\n\n"+importer.AgentSkill {
		t.Errorf("AGENTS.md is:\n%s", got)
	}
}

func TestInitAgentAppendsToAnExistingFile(t *testing.T) {
	dir := agentRepo(t)
	writeFile(t, dir, "CLAUDE.md", "# Rules\n\n## Comments\n\nSay why.\n")

	code, _, stderr := runInitCmd(t, "--agent", "claude")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := read(t, filepath.Join(dir, "CLAUDE.md"))
	if !strings.HasPrefix(got, "# Rules\n\n## Comments\n\nSay why.\n\n"+importer.AgentHeading) {
		t.Errorf("CLAUDE.md is:\n%s", got)
	}
	if !strings.HasSuffix(got, importer.AgentSkill) {
		t.Errorf("CLAUDE.md is:\n%s", got)
	}
}

func TestInitAgentReplacesTheSectionItWrote(t *testing.T) {
	dir := agentRepo(t)
	writeFile(t, dir, "AGENTS.md", "# Rules\n\n"+importer.AgentHeading+"\n\nSomething stale.\n\n### A deeper heading\n\nAlso stale.\n\n## Tests\n\nRun them.\n")

	code, stdout, stderr := runInitCmd(t, "--agent", "agents")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "replaced ") {
		t.Errorf("stdout is %q", stdout)
	}
	got := read(t, filepath.Join(dir, "AGENTS.md"))
	for _, gone := range []string{"Something stale.", "A deeper heading", "Also stale."} {
		if strings.Contains(got, gone) {
			t.Errorf("AGENTS.md still carries %q:\n%s", gone, got)
		}
	}
	if strings.Count(got, importer.AgentHeading+"\n") != 1 {
		t.Errorf("AGENTS.md carries the section twice:\n%s", got)
	}
	if !strings.HasPrefix(got, "# Rules\n\n") || !strings.HasSuffix(got, "## Tests\n\nRun them.\n") {
		t.Errorf("AGENTS.md lost what was around the section:\n%s", got)
	}
	if !strings.Contains(got, importer.AgentSkill) {
		t.Errorf("AGENTS.md does not carry the instructions:\n%s", got)
	}
}

func TestInitAgentReplacesACursorRule(t *testing.T) {
	dir := agentRepo(t)

	if code, _, stderr := runInitCmd(t, "--agent", "cursor"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	code, stdout, stderr := runInitCmd(t, "--agent", "cursor")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "replaced ") {
		t.Errorf("stdout is %q", stdout)
	}
	if got := read(t, filepath.Join(dir, ".cursor", "rules", "tenet.mdc")); !strings.Contains(got, importer.AgentSkill) {
		t.Errorf("the rule is:\n%s", got)
	}
}

func TestInitAgentDryRunWritesNothing(t *testing.T) {
	dir := agentRepo(t)
	writeFile(t, dir, "AGENTS.md", "# Rules\n\n"+importer.AgentHeading+"\n\nStale.\n")

	code, stdout, stderr := runInitCmd(t, "--dry-run", "--agent", "cursor", "--agent", "agents")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "would write the tenet instructions in .cursor/rules/tenet.mdc") {
		t.Errorf("stdout is %q", stdout)
	}
	if !strings.Contains(stdout, "would replace the tenet instructions in AGENTS.md") {
		t.Errorf("stdout is %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor")); !os.IsNotExist(err) {
		t.Errorf("the cursor rule was written anyway: %v", err)
	}
	if got := read(t, filepath.Join(dir, "AGENTS.md")); !strings.Contains(got, "Stale.") {
		t.Errorf("AGENTS.md was rewritten:\n%s", got)
	}
}

func TestInitAgentRefusesTheDraftingFlags(t *testing.T) {
	for _, flag := range []string{"--from", "--preset", "--config"} {
		t.Run(flag, func(t *testing.T) {
			dir := agentRepo(t)

			code, _, stderr := runInitCmd(t, "--agent", "claude", flag, "something")
			if code != report.ExitError {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if !strings.Contains(stderr, flag) {
				t.Errorf("stderr does not name %s: %q", flag, stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
				t.Errorf("CLAUDE.md was written anyway: %v", err)
			}
		})
	}
	t.Run("--force", func(t *testing.T) {
		agentRepo(t)

		code, _, stderr := runInitCmd(t, "--agent", "claude", "--force")
		if code != report.ExitError || !strings.Contains(stderr, "--force") {
			t.Fatalf("exit %d: %s", code, stderr)
		}
	})
}

func TestInitAgentRepeats(t *testing.T) {
	dir := agentRepo(t)

	code, _, stderr := runInitCmd(t, "--agent", "cursor", "--agent", "agents")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, name := range []string{filepath.Join(".cursor", "rules", "tenet.mdc"), "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestInitAgentUnknownWritesNothing(t *testing.T) {
	dir := agentRepo(t)

	code, _, stderr := runInitCmd(t, "--agent", "agents", "--agent", "copilot")
	if code != report.ExitError {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "copilot") {
		t.Errorf("stderr is %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.md was written anyway: %v", err)
	}
}

func TestInitAgentLeavesTheConfigAlone(t *testing.T) {
	dir := agentRepo(t)
	writeFile(t, dir, "tenets.yml", "version: 1\npresets: [agent-hygiene]\n")

	code, _, stderr := runInitCmd(t, "--agent", "claude")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := read(t, filepath.Join(dir, "tenets.yml")); got != "version: 1\npresets: [agent-hygiene]\n" {
		t.Errorf("tenets.yml is:\n%s", got)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
