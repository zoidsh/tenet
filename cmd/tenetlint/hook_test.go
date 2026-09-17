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
	if !strings.Contains(out, "installed") || !strings.Contains(out, path) {
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

	if code, _, errOut := runCmd(t, "hook", "install"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(custom, "pre-commit")); err != nil {
		t.Errorf("hook not written to core.hooksPath: %v", err)
	}
}
