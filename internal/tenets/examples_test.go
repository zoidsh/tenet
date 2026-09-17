package tenets_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/tenets"
)

const withExamples = `version: 1
tenets:
  - id: comment-why
    tenet: A comment says why.
    include: ["**/*.go"]
    examples:
      - label: violation
        line: 2
        code: |
          func add(a, b int) int {
              // add a and b
              return a + b
          }
      - label: ok
        lang: python
        code: |
          # The API rejects a page size above 500.
          PAGE = 500
`

func TestParseInlineExamples(t *testing.T) {
	cfg, err := tenets.Parse([]byte(withExamples))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Tenets[0].Examples
	if len(got) != 2 {
		t.Fatalf("got %d examples", len(got))
	}
	if got[0].Label != tenets.LabelViolation || got[0].Line != 2 || got[0].Lang != "" {
		t.Errorf("first example is %#v", got[0])
	}
	if lines := got[0].Lines(); len(lines) != 4 || lines[1] != "    // add a and b" {
		t.Errorf("lines are %#v", lines)
	}
	if got[1].Label != tenets.LabelOK || got[1].Line != 0 || got[1].Lang != "python" {
		t.Errorf("second example is %#v", got[1])
	}
	if lines := got[1].Lines(); len(lines) != 2 {
		t.Errorf("a block scalar's closing newline became a line: %#v", lines)
	}
}

func TestExamplesFromASiblingFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "tenets.yml"), `version: 1
tenets:
  - id: comment-why
    tenet: A comment says why.
    examples_from: examples/comment-why.yml
    examples:
      - label: ok
        code: |
          x = 1
`)
	if err := os.MkdirAll(filepath.Join(dir, "examples"), 0o750); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "examples", "comment-why.yml"), `- label: violation
  line: 1
  code: |
    // add one
    x = x + 1
`)

	cfg, err := tenets.Load(filepath.Join(dir, "tenets.yml"))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Tenets[0].Examples
	if len(got) != 2 {
		t.Fatalf("got %d examples, want the inline one and the one from the file", len(got))
	}
	if got[0].Label != tenets.LabelOK || got[1].Label != tenets.LabelViolation || got[1].Line != 1 {
		t.Errorf("examples are %#v", got)
	}
}

func TestExamplesLeaveTheHashAlone(t *testing.T) {
	bare, err := tenets.Parse([]byte("version: 1\ntenets:\n  - id: comment-why\n    tenet: A comment says why.\n    include: [\"**/*.go\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := tenets.Parse([]byte(withExamples))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tenets[0].Hash() != bare.Tenets[0].Hash() {
		t.Error("adding examples changed the hash, which throws away the lint cache")
	}
}

func TestExampleValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			"missing label",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - code: \"y = 1\"\n",
			"label must be violation or ok",
		},
		{
			"unknown label",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: maybe\n        code: \"y = 1\"\n",
			`got "maybe"`,
		},
		{
			"missing code",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: ok\n        code: \"  \"\n",
			"code is required",
		},
		{
			"line past the code",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: violation\n        line: 4\n        code: \"y = 1\"\n",
			"line 4 is outside the 1 lines",
		},
		{
			"unknown field",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: ok\n        note: hi\n        code: \"y = 1\"\n",
			"note",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := tenets.Parse([]byte(c.yaml))
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func TestExamplesFromAFileAreValidated(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "tenets.yml"), "version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples_from: side.yml\n")
	write(t, filepath.Join(dir, "side.yml"), "- label: nope\n  code: \"y = 1\"\n")

	_, err := tenets.Load(filepath.Join(dir, "tenets.yml"))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "side.yml") || !strings.Contains(err.Error(), "label must be") {
		t.Errorf("error %q should name the file and the problem", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
