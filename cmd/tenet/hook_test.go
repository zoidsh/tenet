package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	return execute(root), stdout.String(), stderr.String()
}

func TestHookInstallAndUninstall(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)
	path := filepath.Join(dir, ".git", "hooks", "pre-commit")

	code, out, errOut := runCmd(t, "hook", "install")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// The path is printed once, as the repository names it.
	if out != "installed .git/hooks/pre-commit\ninstalled .git/hooks/commit-msg\n" {
		t.Errorf("stdout is %q", out)
	}
	script, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), marker) {
		t.Errorf("hook is %q", script)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), binary) {
		t.Errorf("hook does not run the binary by its absolute path: %q", script)
	}
	if !strings.Contains(string(script), SkipEnv) {
		t.Errorf("hook has no %s escape hatch: %q", SkipEnv, script)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o100 == 0 {
		t.Errorf("hook is not executable: %s", info.Mode())
	}

	if code, _, _ = runCmd(t, "hook", "install"); code != 0 {
		t.Errorf("reinstalling over our own hook exited %d", code)
	}

	// The script exits before it execs anything, so running it here cannot
	// re-enter the test binary.
	skip := exec.Command("sh", path)
	skip.Env = append(os.Environ(), SkipEnv+"=1")
	if out, err := skip.CombinedOutput(); err != nil {
		t.Errorf("the hook did not honour %s: %v\n%s", SkipEnv, err, out)
	}

	code, out, _ = runCmd(t, "hook", "uninstall")
	if code != 0 {
		t.Fatalf("uninstall exited %d", code)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("stdout is %q", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the hook survived: %v", err)
	}
}

func TestHookInstallsBothHooks(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	if code, _, errOut := runCmd(t, "hook", "install"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	paths := map[string]string{}
	for _, name := range []string{"pre-commit", "commit-msg"} {
		path := filepath.Join(dir, ".git", "hooks", name)
		paths[name] = path
		script, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, want := range []string{marker, SkipEnv, binary} {
			if !strings.Contains(string(script), want) {
				t.Errorf("the %s hook does not mention %s: %q", name, want, script)
			}
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0o100 == 0 {
			t.Errorf("the %s hook is not executable: %s", name, info.Mode())
		}
	}
	script, err := os.ReadFile(paths["commit-msg"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), `--commit-msg "$1"`) {
		t.Errorf("the commit-msg hook does not lint the message git hands it: %q", script)
	}

	if code, _, errOut := runCmd(t, "hook", "uninstall"); code != 0 {
		t.Fatalf("uninstall exited %d: %s", code, errOut)
	}
	for name, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("the %s hook survived: %v", name, err)
		}
	}
}

// A commit-msg hook somebody else wrote is refused on its own terms, and the
// pre-commit hook is not written either: half a pair is worse than none.
func TestHookRefusesAForeignCommitMsgHook(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)
	foreign := "#!/bin/sh\necho mine\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", "commit-msg"), []byte(foreign), 0o700); err != nil {
		t.Fatal(err)
	}

	code, _, errOut := runCmd(t, "hook", "install")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut, "--force") || !strings.Contains(errOut, "commit-msg") {
		t.Errorf("stderr is %q", errOut)
	}
	if kept, _ := os.ReadFile(filepath.Join(dir, ".git", "hooks", "commit-msg")); string(kept) != foreign {
		t.Errorf("the foreign hook was touched: %q", kept)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "pre-commit")); !os.IsNotExist(err) {
		t.Errorf("the pre-commit hook was installed anyway: %v", err)
	}

	if code, _, errOut = runCmd(t, "hook", "install", "--force"); code != 0 {
		t.Fatalf("forced install exited %d: %s", code, errOut)
	}
	for _, name := range []string{"pre-commit", "commit-msg"} {
		script, err := os.ReadFile(filepath.Join(dir, ".git", "hooks", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(script), marker) {
			t.Errorf("the %s hook is %q", name, script)
		}
	}
}

func TestHookRefusesAForeignHook(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)
	path := filepath.Join(dir, ".git", "hooks", "pre-commit")
	foreign := "#!/bin/sh\necho mine\n"
	if err := os.WriteFile(path, []byte(foreign), 0o700); err != nil {
		t.Fatal(err)
	}

	code, _, errOut := runCmd(t, "hook", "install")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut, "--force") {
		t.Errorf("stderr is %q", errOut)
	}
	if kept, _ := os.ReadFile(path); string(kept) != foreign {
		t.Errorf("the foreign hook was touched: %q", kept)
	}

	code, out, _ := runCmd(t, "hook", "uninstall")
	if code != 0 {
		t.Fatalf("uninstall exited %d", code)
	}
	if !strings.Contains(out, "left") {
		t.Errorf("stdout is %q", out)
	}
	if kept, _ := os.ReadFile(path); string(kept) != foreign {
		t.Errorf("uninstall removed a foreign hook")
	}

	if code, _, errOut = runCmd(t, "hook", "install", "--force"); code != 0 {
		t.Fatalf("forced install exited %d: %s", code, errOut)
	}
	replaced, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(replaced), marker) {
		t.Errorf("hook is %q", replaced)
	}
}

func TestHookRespectsHooksPath(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	custom := filepath.Join(dir, "githooks")
	git(t, dir, "config", "core.hooksPath", custom)
	t.Chdir(dir)

	// core.hooksPath is itself a refusal, so only a forced install gets as far
	// as writing anywhere.
	if code, _, errOut := runCmd(t, "hook", "install", "--force"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(custom, "pre-commit")); err != nil {
		t.Errorf("hook not written to core.hooksPath: %v", err)
	}
}

// A hooks directory git was pointed at is somebody else's arrangement, whether
// or not it holds a hook yet.
func TestHookRefusesAManagedHooksPath(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "core.hooksPath", filepath.Join(dir, "githooks"))
	t.Chdir(dir)

	code, _, errOut := runCmd(t, "hook", "install")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	for _, want := range []string{"core.hooksPath", "--commit-msg", "--force"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr does not mention %s: %q", want, errOut)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "githooks", "pre-commit")); !os.IsNotExist(err) {
		t.Errorf("a hook was written anyway: %v", err)
	}
}

// A hook of any name, not only the two tenet writes, says something already
// runs hooks here.
func TestHookRefusesAnUnrelatedHook(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)
	prePush := filepath.Join(dir, ".git", "hooks", "pre-push")
	foreign := "#!/bin/sh\necho mine\n"
	if err := os.WriteFile(prePush, []byte(foreign), 0o700); err != nil {
		t.Fatal(err)
	}

	code, _, errOut := runCmd(t, "hook", "install")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	for _, want := range []string{"pre-push", "--force"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr does not mention %s: %q", want, errOut)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "pre-commit")); !os.IsNotExist(err) {
		t.Errorf("a hook was written anyway: %v", err)
	}

	if code, _, errOut = runCmd(t, "hook", "install", "--force"); code != 0 {
		t.Fatalf("forced install exited %d: %s", code, errOut)
	}
	for _, name := range []string{"pre-commit", "commit-msg"} {
		script, err := os.ReadFile(filepath.Join(dir, ".git", "hooks", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(script), marker) {
			t.Errorf("the %s hook is %q", name, script)
		}
	}
	if kept, _ := os.ReadFile(prePush); string(kept) != foreign {
		t.Errorf("--force touched a hook that is not tenet's: %q", kept)
	}
}

// git ships the samples with every repository, so they say nothing about who
// manages its hooks.
func TestHookInstallsBesideTheSamples(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	t.Chdir(dir)
	sample := filepath.Join(dir, ".git", "hooks", "pre-commit.sample")
	if err := os.WriteFile(sample, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	if code, _, errOut := runCmd(t, "hook", "install"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	script, err := os.ReadFile(filepath.Join(dir, ".git", "hooks", "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), marker) {
		t.Errorf("hook is %q", script)
	}
}
