// The examples in the config below are labelled violations of comment-why, so
// the comment that restates the code is the fixture and not a slip.
// tenet:ignore-file comment-why
package tenets_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/tenets"
)

const withExamples = `version: 1
tenets:
  - id: comment-why
    tenet: A comment says why.
    include: ["**/*.go"]
    examples:
      - label: violation
        lines: 2
        code: |
          func add(a, b int) int {
              // add a and b
              return a + b
          }
      - label: violation
        lines: [1, 3]
        code: |
          func add(a, b int) int {
              // add a and b
              return a + b
          }
      - label: ok
        lang: python
        note: The tenet takes no position on a constant's name.
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
	if len(got) != 3 {
		t.Fatalf("got %d examples", len(got))
	}
	// A bare line is the span that holds only itself.
	if got[0].Label != tenets.LabelViolation || got[0].Lines != (tenets.LineRange{First: 2, Last: 2}) || got[0].Lang != "" {
		t.Errorf("first example is %#v", got[0])
	}
	if lines := got[0].CodeLines(); len(lines) != 4 || lines[1] != "    // add a and b" {
		t.Errorf("lines are %#v", lines)
	}
	if got[1].Lines != (tenets.LineRange{First: 1, Last: 3}) {
		t.Errorf("second example is %#v", got[1])
	}
	if got[2].Label != tenets.LabelOK || got[2].Lines.Set() || got[2].Lang != "python" || got[2].Note == "" {
		t.Errorf("third example is %#v", got[2])
	}
	if lines := got[2].CodeLines(); len(lines) != 2 {
		t.Errorf("a block scalar's closing newline became a line: %#v", lines)
	}
}

func TestLineRangeContains(t *testing.T) {
	span := tenets.LineRange{First: 4, Last: 6}
	for line, want := range map[int]bool{0: false, 3: false, 4: true, 5: true, 6: true, 7: false} {
		if got := span.Contains(line); got != want {
			t.Errorf("4 to 6 contains %d = %v", line, got)
		}
	}
	if (tenets.LineRange{}).Contains(0) {
		t.Error("an example that named no lines contains the line nothing was named on")
	}
}

func TestExamplesFromASiblingFile(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, tenets.FileName)
	write(t, config, `version: 1
tenets:
  - id: comment-why
    tenet: A comment says why.
    examples_from: examples/comment-why.yml
    examples:
      - label: ok
        code: |
          x = 1
`)
	write(t, filepath.Join(filepath.Dir(config), "examples", "comment-why.yml"), `- label: violation
  lines: 1
  code: |
    // add one
    x = x + 1
`)

	cfg, err := tenets.Load(config)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Tenets[0].Examples
	if len(got) != 2 {
		t.Fatalf("got %d examples, want the inline one and the one from the file", len(got))
	}
	if got[0].Label != tenets.LabelOK || got[1].Label != tenets.LabelViolation || !got[1].Lines.Contains(1) {
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
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: violation\n        lines: 4\n        code: \"y = 1\"\n",
			"line 4 is outside the 1 lines",
		},
		{
			"span past the code",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: violation\n        lines: [1, 3]\n        code: \"y = 1\\nz = 2\"\n",
			"line 3 is outside the 2 lines",
		},
		{
			"span ending before it starts",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: violation\n        lines: [3, 1]\n        code: \"y = 1\"\n",
			"3 to 1 ends before it starts",
		},
		{
			"line counted from zero",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: violation\n        lines: 0\n        code: \"y = 1\"\n",
			"counted from 1",
		},
		{
			"too many lines",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: violation\n        lines: [1, 2, 3]\n        code: \"y = 1\"\n",
			"one or two lines, got 3",
		},
		{
			"lines that are not lines",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: violation\n        lines: here\n        code: \"y = 1\"\n",
			"must be a line or a pair of lines",
		},
		{
			"the old line key",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: violation\n        line: 1\n        code: \"y = 1\"\n",
			"line",
		},
		{
			"unknown field",
			"version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples:\n      - label: ok\n        comment: hi\n        code: \"y = 1\"\n",
			"comment",
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
	config := filepath.Join(dir, tenets.FileName)
	write(t, config, "version: 1\ntenets:\n  - id: a\n    tenet: x\n    examples_from: side.yml\n")
	write(t, filepath.Join(filepath.Dir(config), "side.yml"), "- label: nope\n  code: \"y = 1\"\n")

	_, err := tenets.Load(config)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "side.yml") || !strings.Contains(err.Error(), "label must be") {
		t.Errorf("error %q should name the file and the problem", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
