package auth_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/auth"
	"github.com/zoidsh/tenet/internal/provider"
)

const (
	envKey     = "env-key-aaaaaaaaaaaaaaaaaaaa"
	projectKey = "project-key-bbbbbbbbbbbbbbbb"
	globalKey  = "global-key-cccccccccccccccc"
)

func typesafe(t *testing.T) provider.Provider {
	t.Helper()
	p, err := provider.Lookup(provider.Default)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// repo is a git repository to run in, with the config home pointed somewhere
// empty so that the machine's own key is never what a test reads.
func repo(t *testing.T) (dir, config string) {
	t.Helper()
	dir, config = t.TempDir(), t.TempDir()
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Chdir(dir)
	return dir, config
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
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

func TestResolveTakesTheEnvironmentFirstAndTheGlobalFileLast(t *testing.T) {
	dir, config := repo(t)
	p := typesafe(t)
	write(t, filepath.Join(dir, auth.Dir, auth.FileName), "typesafe="+projectKey+"\n")
	write(t, filepath.Join(config, "tenet", auth.FileName), "typesafe="+globalKey+"\n")
	t.Setenv(p.Env, envKey)

	for _, want := range []struct {
		key, source string
		drop        func()
	}{
		{envKey, auth.SourceEnv, func() { t.Setenv(p.Env, "") }},
		{projectKey, auth.SourceProject, func() {
			if err := os.Remove(filepath.Join(dir, auth.Dir, auth.FileName)); err != nil {
				t.Fatal(err)
			}
		}},
		{globalKey, auth.SourceGlobal, func() {
			if err := os.Remove(filepath.Join(config, "tenet", auth.FileName)); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		key, source, err := auth.Resolve(p)
		if err != nil {
			t.Fatal(err)
		}
		if key != want.key || source != want.source {
			t.Fatalf("resolved %q from %q, want %q from %q", key, source, want.key, want.source)
		}
		want.drop()
	}

	key, source, err := auth.Resolve(p)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" || source != "" {
		t.Errorf("resolved %q from %q with nothing left to read", key, source)
	}
}

// One provider's key is not another's, so that a hosted service and TypeSafe
// can both be signed in to at once.
func TestResolveIsPerProvider(t *testing.T) {
	_, config := repo(t)
	write(t, filepath.Join(config, "tenet", auth.FileName), "tenet="+globalKey+"\n")

	key, _, err := auth.Resolve(typesafe(t))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		t.Errorf("typesafe read %q from another provider's line", key)
	}
}

// The path the documentation names is the path every platform uses, so that
// a Mac is not told to look somewhere the README has never heard of.
func TestGlobalPathIsUnderDotConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)

	got, err := auth.GlobalPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "tenet", auth.FileName); got != want {
		t.Errorf("the global file is %q, want %q", got, want)
	}
}

func TestGlobalPathFollowsTheConfigHomeWhenItIsSet(t *testing.T) {
	config := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", config)

	got, err := auth.GlobalPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(config, "tenet", auth.FileName); got != want {
		t.Errorf("the global file is %q, want %q", got, want)
	}
}

func TestSaveWritesAPrivateFile(t *testing.T) {
	_, config := repo(t)

	saved, err := auth.Save(typesafe(t), globalKey, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(config, "tenet", auth.FileName); saved.Path != want {
		t.Errorf("saved to %q, want %q", saved.Path, want)
	}
	if got := read(t, saved.Path); got != "typesafe="+globalKey+"\n" {
		t.Errorf("the file holds %q", got)
	}
	info, err := os.Stat(saved.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the file is %v", info.Mode().Perm())
	}
	dir, err := os.Stat(filepath.Dir(saved.Path))
	if err != nil {
		t.Fatal(err)
	}
	if dir.Mode().Perm() != 0o700 {
		t.Errorf("the directory is %v", dir.Mode().Perm())
	}
}

func TestSaveProjectIgnoresTheCredentialsOnce(t *testing.T) {
	dir, _ := repo(t)
	p := typesafe(t)

	saved, err := auth.Save(p, projectKey, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, auth.Dir, auth.FileName); saved.Path != want {
		t.Errorf("saved to %q, want %q", saved.Path, want)
	}
	gitignore := filepath.Join(dir, ".gitignore")
	if saved.Gitignore != gitignore {
		t.Errorf("appended to %q, want %q", saved.Gitignore, gitignore)
	}
	entry := auth.Dir + "/" + auth.FileName
	if got := read(t, gitignore); got != entry+"\n" {
		t.Errorf(".gitignore holds %q", got)
	}

	again, err := auth.Save(p, globalKey, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Gitignore != "" {
		t.Errorf("a second save appended to %q", again.Gitignore)
	}
	if got := strings.Count(read(t, gitignore), entry); got != 1 {
		t.Errorf(".gitignore names the credentials %d times", got)
	}
	if got := read(t, again.Path); got != "typesafe="+globalKey+"\n" {
		t.Errorf("the file holds %q", got)
	}
}

// A repository that ignores the directory already needs no line of its own.
func TestSaveProjectLeavesAnExistingIgnoreAlone(t *testing.T) {
	dir, _ := repo(t)
	write(t, filepath.Join(dir, ".gitignore"), auth.Dir+"/\n")

	saved, err := auth.Save(typesafe(t), projectKey, false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Gitignore != "" {
		t.Errorf("appended to %q", saved.Gitignore)
	}
	if got := read(t, filepath.Join(dir, ".gitignore")); got != auth.Dir+"/\n" {
		t.Errorf(".gitignore holds %q", got)
	}
}

// A newer tenet may have saved a provider this one does not know, and
// rewriting the file must not drop that key.
func TestSaveKeepsALineItDoesNotUnderstand(t *testing.T) {
	_, config := repo(t)
	path := filepath.Join(config, "tenet", auth.FileName)
	write(t, path, "future=some-other-key\n")

	if _, err := auth.Save(typesafe(t), globalKey, true); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "future=some-other-key\ntypesafe="+globalKey+"\n" {
		t.Errorf("the file holds %q", got)
	}
}

// A hand-edited file that is not a credentials file is worth saying so about,
// rather than reading as though no key had ever been saved.
func TestResolveRefusesALineItCannotRead(t *testing.T) {
	_, config := repo(t)
	path := filepath.Join(config, "tenet", auth.FileName)
	write(t, path, "# a comment\n\ntypesafe "+globalKey+"\n")

	_, _, err := auth.Resolve(typesafe(t))
	if err == nil {
		t.Fatal("the file was read anyway")
	}
	if !strings.Contains(err.Error(), path+":3") || !strings.Contains(err.Error(), "provider=key") {
		t.Errorf("error is %q", err)
	}
}

func TestSaveRefusesAProviderThatIsNotThereYet(t *testing.T) {
	repo(t)
	hosted, err := provider.Lookup("tenet")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := auth.Save(hosted, globalKey, true); err == nil || !strings.Contains(err.Error(), "not available yet") {
		t.Fatalf("error is %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	for _, c := range []struct{ name, key, want string }{
		{"empty", "", "empty"},
		{"a space inside", "key with spaces aaaaaaaaaa", "spaces"},
		{"a newline inside", "key\naaaaaaaaaaaaaaaaaaaaaaa", "spaces"},
		{"too short", "short-key", "at least 20"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := auth.Validate(c.key)
			if err == nil {
				t.Fatal("the key was accepted")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error is %q, which does not say %q", err, c.want)
			}
		})
	}
	if err := auth.Validate(globalKey); err != nil {
		t.Errorf("a good key was rejected: %v", err)
	}
}

func TestSaveValidatesBeforeWriting(t *testing.T) {
	_, config := repo(t)

	if _, err := auth.Save(typesafe(t), "short", true); err == nil {
		t.Fatal("a short key was saved")
	}
	if _, err := os.Stat(filepath.Join(config, "tenet", auth.FileName)); !os.IsNotExist(err) {
		t.Errorf("the file was written anyway: %v", err)
	}
}

func TestRemoveTakesTheKeyAndLeavesTheOthers(t *testing.T) {
	_, config := repo(t)
	path := filepath.Join(config, "tenet", auth.FileName)
	write(t, path, "future=some-other-key\ntypesafe="+globalKey+"\n")
	p := typesafe(t)

	got, removed, err := auth.Remove(p, auth.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if got != path || !removed {
		t.Fatalf("removed %v from %q", removed, got)
	}
	if left := read(t, path); left != "future=some-other-key\n" {
		t.Errorf("the file holds %q", left)
	}

	_, removed, err = auth.Remove(p, auth.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("a second remove found a key")
	}
}

// A file that holds no key at all is a file worth not leaving behind.
func TestRemoveTakesTheFileWithTheLastKey(t *testing.T) {
	dir, _ := repo(t)
	p := typesafe(t)
	if _, err := auth.Save(p, projectKey, false); err != nil {
		t.Fatal(err)
	}

	path, removed, err := auth.Remove(p, auth.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if !removed || path != filepath.Join(dir, auth.Dir, auth.FileName) {
		t.Fatalf("removed %v from %q", removed, path)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the empty file is still there: %v", err)
	}
}

func TestProjectScopeNeedsARepository(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	if _, err := auth.Save(typesafe(t), globalKey, false); err == nil {
		t.Fatal("a key was saved outside a repository")
	}
	path, err := auth.ProjectPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Errorf("the project file outside a repository is %q", path)
	}
}

// The file a run reads is the nearest one at or above the working directory,
// so that a repository with one in a subdirectory is read from there.
func TestResolveWalksUpToTheRepositoryRoot(t *testing.T) {
	dir, _ := repo(t)
	write(t, filepath.Join(dir, auth.Dir, auth.FileName), "typesafe="+projectKey+"\n")
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	key, source, err := auth.Resolve(typesafe(t))
	if err != nil {
		t.Fatal(err)
	}
	if key != projectKey || source != auth.SourceProject {
		t.Errorf("resolved %q from %q", key, source)
	}
}
