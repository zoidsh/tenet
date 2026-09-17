package source

import (
	"fmt"
	"strings"
	"testing"
)

func lines(n int, text string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = text
	}
	return out
}

func TestWindowsHardCut(t *testing.T) {
	f := &File{Path: "a.go", Lines: lines(300, "x := 1")}
	ws := f.Windows()
	if len(ws) != 2 {
		t.Fatalf("got %d windows", len(ws))
	}
	if len(ws[0].Lines) != MaxWindowLines || ws[0].First != 1 {
		t.Errorf("first window: %d lines from %d", len(ws[0].Lines), ws[0].First)
	}
	if len(ws[1].Lines) != 46 || ws[1].First != 255 {
		t.Errorf("second window: %d lines from %d", len(ws[1].Lines), ws[1].First)
	}
	if got := ws[1].Line(1); got != 255 {
		t.Errorf("window line 1 maps to file line %d", got)
	}
}

func TestWindowsCutAtBlankLine(t *testing.T) {
	body := lines(300, "x := 1")
	body[229] = ""
	body[238] = "   "
	f := &File{Path: "a.go", Lines: body}
	ws := f.Windows()
	if len(ws[0].Lines) != 239 {
		t.Fatalf("first window has %d lines, want the cut at the last blank line in range", len(ws[0].Lines))
	}
	if ws[1].First != 240 {
		t.Errorf("second window starts at %d", ws[1].First)
	}
}

func TestWindowsBlankLineOutOfReachIsIgnored(t *testing.T) {
	body := lines(300, "x := 1")
	body[100] = ""
	f := &File{Path: "a.go", Lines: body}
	if got := len(f.Windows()[0].Lines); got != MaxWindowLines {
		t.Errorf("first window has %d lines, want a hard cut", got)
	}
}

func TestWindowsByteCap(t *testing.T) {
	long := strings.Repeat("a", 1000)
	f := &File{Path: "a.go", Lines: lines(200, long)}
	ws := f.Windows()
	for i, w := range ws {
		size := 0
		for _, l := range w.Lines {
			size += len(l) + 1
		}
		if size > MaxWindowBytes {
			t.Errorf("window %d is %d bytes", i, size)
		}
	}
	if len(ws) < 5 {
		t.Errorf("got %d windows, want the byte cap to have cut earlier than the line cap", len(ws))
	}
}

func TestWindowsReportable(t *testing.T) {
	f := &File{Path: "a.go", Lines: lines(300, "x := 1"), reportable: map[int]bool{270: true}}
	var kept []*Window
	for _, w := range f.Windows() {
		if w.HasReportable() {
			kept = append(kept, w)
		}
	}
	if len(kept) != 1 || kept[0].First != 255 {
		t.Fatalf("kept %d windows", len(kept))
	}
}

func TestLanguage(t *testing.T) {
	for ext, want := range map[string]string{
		".go": "go", ".ts": "typescript", ".py": "python", ".rs": "rust",
		".sh": "shell", ".yml": "yaml", ".md": "markdown", ".txt": "text", "": "text",
	} {
		f := &File{Path: fmt.Sprintf("dir/file%s", ext)}
		if got := f.Language(); got != want {
			t.Errorf("%q is %q, want %q", ext, got, want)
		}
	}
}
