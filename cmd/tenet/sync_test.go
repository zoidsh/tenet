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

	"github.com/zoidsh/tenet/internal/importer"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/tenets"
)

const (
	keptRule    = "A comment says why the code exists, not what it does."
	addedRule   = "Never write a comment that repeats the code."
	oldRule     = "A comment on a struct field says what invariant it carries."
	newRule     = "A comment on a struct field says which invariant it carries."
	droppedRule = "A comment in a test names the case it covers."
)

// syncRules is the file as it stands after somebody added a rule, reworded
// another and deleted a third.
const syncRules = `# Rules

## Comments

` + keptRule + `

- ` + addedRule + `
- ` + newRule + `
`

const syncConfig = `version: 1
model: jev-1.13.0

# The built-in rules we lean on.
rules:
  # CLAUDE.md: ` + keptRule + `
  - comment-why

tenets:
  # Tim wrote this line by hand.
  - id: struct-field-invariant
    tenet: ` + oldRule + `
    source: 'CLAUDE.md: ` + oldRule + `'
  - id: test-comment-names-case
    tenet: ` + droppedRule + `
    source: 'CLAUDE.md: ` + droppedRule + `'
`

// syncServer stands in for the API. It sorts every sentence about comments as
// a code rule, and pairs two sentences when both are about a struct field,
// which is the one rewording the test file holds.
func syncServer(t *testing.T) *httptest.Server {
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
			switch {
			case strings.HasPrefix(name, "same:"):
				prob := 0.03
				if strings.Count(q.Instructions, "struct field") == 2 {
					prob = 0.96
				}
				answers[name] = jev.Answer{Type: jev.KindNoul, Noul: prob}
			case strings.HasPrefix(name, "kind:"):
				kind, p := importer.KindProcess, 0.77
				if strings.Contains(q.Instructions, "comment") {
					kind, p = importer.KindCodeRule, 0.93
				}
				answers[name] = jev.Answer{
					Type:          jev.KindChoice,
					Probabilities: map[string]float64{kind: p, importer.KindOther: 1 - p},
				}
			default:
				noul := 0.12
				if strings.Contains(q.Instructions, "comment") {
					noul = 0.88
				}
				answers[name] = jev.Answer{Type: jev.KindNoul, Noul: noul}
			}
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

func syncRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "CLAUDE.md", syncRules)
	writeConfigFile(t, dir, syncConfig)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")
	t.Setenv(jev.BaseURLEnv, syncServer(t).URL)
	return dir
}

func runSyncCmd(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"sync", "--no-cache"}, args...))
	return execute(root), stdout.String(), stderr.String()
}

func readConfig(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, tenets.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSyncEndToEnd(t *testing.T) {
	dir := syncRepo(t)

	code, stdout, stderr := runSyncCmd(t)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{
		"added",
		"never-write-comment-repeats-code",
		"changed",
		"struct-field-invariant",
		"re-run tenet check struct-field-invariant",
		"stale",
		"test-comment-names-case",
		"1 added, 1 changed, 0 restated, 1 stale, written to " + tenets.FileName,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout does not mention %q:\n%s", want, stdout)
		}
	}

	got := readConfig(t, dir)
	for _, want := range []string{
		"# Tim wrote this line by hand.",
		"  # CLAUDE.md: " + keptRule,
		"  - comment-why",
		"    tenet: " + newRule,
		"    source: 'CLAUDE.md: " + newRule + "'",
		"  - id: never-write-comment-repeats-code",
		"    tenet: " + addedRule,
		"  - id: test-comment-names-case",
		"    tenet: " + droppedRule,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the config does not hold %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, oldRule) {
		t.Errorf("the reworded rule is still the old one:\n%s", got)
	}
	if !strings.Contains(got, "model: jev-1.13.0\n\n# The built-in rules we lean on.") {
		t.Errorf("the blank line and the comment above rules: are gone:\n%s", got)
	}
	cfg, err := tenets.Load(filepath.Join(dir, tenets.FileName))
	if err != nil {
		t.Fatalf("the synced config does not load: %v", err)
	}
	if len(cfg.Tenets) != 4 {
		t.Errorf("the config resolves to %d tenets", len(cfg.Tenets))
	}
}

// Running it twice changes nothing the second time, because everything the
// rule files say is now in the config.
func TestSyncIsSettledAfterOneRun(t *testing.T) {
	dir := syncRepo(t)
	if code, _, stderr := runSyncCmd(t); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	first := readConfig(t, dir)

	code, stdout, stderr := runSyncCmd(t)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "0 added, 0 changed, 0 restated, 1 stale, nothing to write") {
		t.Errorf("the second run is %s", stdout)
	}
	if got := readConfig(t, dir); got != first {
		t.Errorf("the second run rewrote the config:\n%s\nwas:\n%s", got, first)
	}
}

func TestSyncDryRunWritesNothing(t *testing.T) {
	dir := syncRepo(t)

	code, stdout, stderr := runSyncCmd(t, "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "nothing written (--dry-run)") {
		t.Errorf("stdout is %s", stdout)
	}
	if got := readConfig(t, dir); got != syncConfig {
		t.Errorf("a dry run wrote the config:\n%s", got)
	}
}

type jsonSync struct {
	Added []struct {
		Tenet  string   `json:"tenet"`
		Source string   `json:"source"`
		Kind   []string `json:"kind"`
	} `json:"added"`
	Changed []struct {
		Tenet       string `json:"tenet"`
		Rule        string `json:"rule"`
		Old         string `json:"old"`
		New         string `json:"new"`
		TextUpdated bool   `json:"text_updated"`
	} `json:"changed"`
	Stale []struct {
		Tenet  string `json:"tenet"`
		Source string `json:"source"`
	} `json:"stale"`
	Restated []struct {
		Tenet  string `json:"tenet"`
		Source string `json:"source"`
	} `json:"restated"`
	Candidates []struct {
		File  string `json:"file"`
		Text  string `json:"text"`
		Tenet string `json:"tenet"`
	} `json:"candidates"`
	Written *string `json:"written"`
	DryRun  bool    `json:"dry_run"`
	Stats   struct {
		Candidates int `json:"candidates"`
		Calls      int `json:"calls"`
	} `json:"stats"`
}

func TestSyncJSON(t *testing.T) {
	syncRepo(t)

	code, stdout, stderr := runSyncCmd(t, "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var got jsonSync
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout)
	}
	if len(got.Added) != 1 || got.Added[0].Tenet != "never-write-comment-repeats-code" {
		t.Fatalf("added %#v", got.Added)
	}
	if got.Added[0].Source != importer.SourceLine("CLAUDE.md", addedRule) {
		t.Errorf("source is %q", got.Added[0].Source)
	}
	if len(got.Added[0].Kind) != 1 || got.Added[0].Kind[0] != "code" {
		t.Errorf("kind is %#v", got.Added[0].Kind)
	}
	if len(got.Changed) != 1 {
		t.Fatalf("changed %#v", got.Changed)
	}
	if c := got.Changed[0]; c.Tenet != "struct-field-invariant" || c.Old != oldRule || c.New != newRule || !c.TextUpdated {
		t.Errorf("changed %#v", c)
	}
	if len(got.Stale) != 1 || got.Stale[0].Tenet != "test-comment-names-case" {
		t.Errorf("stale %#v", got.Stale)
	}
	// The rejected sentences are reported as init reports them, and every
	// accepted one carries the id it has in the config.
	if len(got.Candidates) != 3 {
		t.Fatalf("candidates %#v", got.Candidates)
	}
	for _, c := range got.Candidates {
		if c.Text == addedRule && c.Tenet != "never-write-comment-repeats-code" {
			t.Errorf("the added candidate carries no id: %#v", c)
		}
		if c.Text == newRule && c.Tenet == "" {
			t.Errorf("a candidate the config already holds carries no id: %#v", c)
		}
	}
	if got.Written == nil || *got.Written != tenets.FileName || got.DryRun {
		t.Errorf("written %v, dry run %v", got.Written, got.DryRun)
	}
	// One call sorts the sentences and one pairs the rewording.
	if got.Stats.Candidates != 3 || got.Stats.Calls != 2 {
		t.Errorf("stats %#v", got.Stats)
	}
}

func TestSyncWithoutAConfig(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "CLAUDE.md", syncRules)
	t.Chdir(dir)
	t.Setenv(jev.APIKeyEnv, "test-key")

	code, _, stderr := runSyncCmd(t)
	if code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
	if !strings.Contains(stderr, "tenet init") {
		t.Errorf("stderr is %q", stderr)
	}
}

func TestSyncRefusesABrokenConfig(t *testing.T) {
	dir := syncRepo(t)
	writeConfigFile(t, dir, "version: 2\n")

	code, _, stderr := runSyncCmd(t)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "version") {
		t.Errorf("stderr is %q", stderr)
	}
	if got := readConfig(t, dir); got != "version: 2\n" {
		t.Errorf("the broken config was written to:\n%s", got)
	}
}

// The flags that draft a file from scratch have no meaning for a file that is
// already there, so sync does not take them.
func TestSyncRefusesTheInitOnlyFlags(t *testing.T) {
	for _, flag := range []string{"--preset=agent-hygiene", "--agent=claude", "--force"} {
		t.Run(flag, func(t *testing.T) {
			syncRepo(t)
			code, _, stderr := runSyncCmd(t, flag)
			if code != 2 {
				t.Fatalf("exit %d, want 2", code)
			}
			name, _, _ := strings.Cut(strings.TrimPrefix(flag, "--"), "=")
			if !strings.Contains(stderr, name) {
				t.Errorf("stderr does not name %s: %q", name, stderr)
			}
		})
	}
}

// A sentence the config already holds lends the candidate its own id instead
// of spending the slug, so a new draft whose words slug the same way is named
// plainly rather than handed a -2 nothing asked for.
func TestSyncDoesNotSpendASlugOnASentenceItAlreadyHolds(t *testing.T) {
	dir := syncRepo(t)
	// Both sentences slug to never-write-comment-repeats-code; the config
	// holds the first under an id of its own.
	writeFile(t, dir, "CLAUDE.md", "## Comments\n\n"+addedRule+"\n\n- Never write, a comment that repeats the code!\n")
	writeConfigFile(t, dir, "version: 1\ntenets:\n  - id: no-restating\n    tenet: "+addedRule+"\n    source: 'CLAUDE.md: "+addedRule+"'\n")

	code, stdout, stderr := runSyncCmd(t, "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var got jsonSync
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v in %s", err, stdout)
	}
	if len(got.Added) != 1 {
		t.Fatalf("added %#v", got.Added)
	}
	if got.Added[0].Tenet != "never-write-comment-repeats-code" {
		t.Errorf("the draft is %q, want the plain slug", got.Added[0].Tenet)
	}
	for _, c := range got.Candidates {
		if c.Text == addedRule && c.Tenet != "no-restating" {
			t.Errorf("the covered candidate is %q, want the id of the tenet holding it", c.Tenet)
		}
	}
}

func TestSyncFromANamedFile(t *testing.T) {
	dir := syncRepo(t)
	writeFile(t, dir, "other.md", "- Never write a comment nobody asked for.\n")

	code, stdout, stderr := runSyncCmd(t, "--from", "other.md")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "never-write-comment-nobody-asked") {
		t.Errorf("stdout is %s", stdout)
	}
	if !strings.Contains(readConfig(t, dir), "source: 'other.md: Never write a comment nobody asked for.'") {
		t.Errorf("the config is:\n%s", readConfig(t, dir))
	}
	// CLAUDE.md was never opened, so nothing drafted from it is missing.
	if !strings.Contains(stdout, "0 changed, 0 restated, 0 stale") {
		t.Errorf("a run narrowed to one file called the other file's rules stale:\n%s", stdout)
	}
	for _, gone := range []string{"struct-field-invariant", "test-comment-names-case"} {
		if strings.Contains(stdout, gone) {
			t.Errorf("stdout names %s, which came from a file it did not read:\n%s", gone, stdout)
		}
	}
}

// A source drafted before the sentence was carried in it is resolved against
// the file as it is now and restated, without anything being called changed.
func TestSyncRestatesAnOldSourceLine(t *testing.T) {
	dir := syncRepo(t)
	writeConfigFile(t, dir, "version: 1\ntenets:\n  - id: says-why\n    tenet: "+keptRule+"\n    source: CLAUDE.md:5\n")

	code, stdout, stderr := runSyncCmd(t)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "0 changed, 1 restated") {
		t.Errorf("stdout is %s", stdout)
	}
	if !strings.Contains(stdout, "restated  says-why") {
		t.Errorf("the restated line is missing:\n%s", stdout)
	}
	if want := "source: 'CLAUDE.md: " + keptRule + "'"; !strings.Contains(readConfig(t, dir), want) {
		t.Errorf("the config does not hold %q:\n%s", want, readConfig(t, dir))
	}
}
