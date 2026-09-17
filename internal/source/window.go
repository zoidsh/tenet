package source

import (
	"path/filepath"
	"strings"
)

// Window limits, both chosen to stay inside what one call can carry: the
// location question offers a label per line plus none, and the model takes at
// most 255 labels. The byte cap also leaves the questions room within the
// request's token budget, which the window's lines otherwise eat alone.
const (
	MaxWindowLines = 254
	MaxWindowBytes = 40 * 1024
	cutbackLines   = 40
)

// File is one file to lint, with its directives already stripped.
type File struct {
	// Path is relative to the repository root, slash separated.
	Path  string
	Lines []string

	// reportable holds the lines a finding may be raised on; nil means every
	// line is.
	reportable map[int]bool
	Sup        *Suppressions
}

// Reportable reports whether a finding on this line would be shown.
func (f *File) Reportable(line int) bool {
	if f.reportable == nil {
		return line >= 1 && line <= len(f.Lines)
	}
	return f.reportable[line]
}

// Language names the file's language for the model, falling back to text.
func (f *File) Language() string { return LanguageForPath(f.Path) }

// LanguageForPath names the language a path is written in, falling back to
// text. A glob is a path for this purpose, so that a tenet's include pattern
// names the language of its examples.
func LanguageForPath(path string) string {
	if lang, ok := languages[strings.ToLower(filepath.Ext(path))]; ok {
		return lang
	}
	return "text"
}

// ExtensionFor is the extension a file of the named language is written with,
// empty for a language tenetlint does not know. It is spelled out rather than
// read backwards out of the extension table, which holds several extensions
// per language and no opinion on which one to write.
func ExtensionFor(lang string) string {
	return extensions[lang]
}

var extensions = map[string]string{
	"go":         ".go",
	"typescript": ".ts",
	"javascript": ".js",
	"python":     ".py",
	"rust":       ".rs",
	"java":       ".java",
	"kotlin":     ".kt",
	"swift":      ".swift",
	"ruby":       ".rb",
	"php":        ".php",
	"c":          ".c",
	"cpp":        ".cpp",
	"csharp":     ".cs",
	"shell":      ".sh",
	"sql":        ".sql",
	"yaml":       ".yml",
	"json":       ".json",
	"markdown":   ".md",
	"html":       ".html",
	"css":        ".css",
	"text":       ".txt",
	"rst":        ".rst",
	"asciidoc":   ".adoc",
}

// Window is a slice of a file small enough to ask about in one call.
type Window struct {
	File  *File
	First int
	Lines []string
}

// Path is the window's file path.
func (w *Window) Path() string { return w.File.Path }

// Line maps a one-based line id within the window to its file line.
func (w *Window) Line(id int) int { return w.First + id - 1 }

// HasReportable reports whether a finding in this window could be shown at
// all, which is what decides whether it is worth a call.
func (w *Window) HasReportable() bool {
	for i := range w.Lines {
		if w.File.Reportable(w.First + i) {
			return true
		}
	}
	return false
}

// Windows splits a file into windows, cutting at a blank line near the end of
// a window when there is one so that a window rarely ends mid-construct.
func (f *File) Windows() []*Window {
	var windows []*Window
	for start := 0; start < len(f.Lines); {
		end := windowEnd(f.Lines, start)
		windows = append(windows, &Window{File: f, First: start + 1, Lines: f.Lines[start:end]})
		start = end
	}
	return windows
}

func windowEnd(lines []string, start int) int {
	end := min(start+MaxWindowLines, len(lines))

	size := 0
	for i := start; i < end; i++ {
		size += len(lines[i]) + 1
		if size > MaxWindowBytes && i > start {
			end = i
			break
		}
	}
	if end == len(lines) {
		return end
	}
	for i := end - 1; i >= end-cutbackLines && i > start; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			return i + 1
		}
	}
	return end
}

var languages = map[string]string{
	".go":    "go",
	".ts":    "typescript",
	".tsx":   "typescript",
	".js":    "javascript",
	".jsx":   "javascript",
	".mjs":   "javascript",
	".cjs":   "javascript",
	".py":    "python",
	".rs":    "rust",
	".java":  "java",
	".kt":    "kotlin",
	".kts":   "kotlin",
	".swift": "swift",
	".rb":    "ruby",
	".php":   "php",
	".c":     "c",
	".h":     "c",
	".cc":    "cpp",
	".cpp":   "cpp",
	".cxx":   "cpp",
	".hpp":   "cpp",
	".cs":    "csharp",
	".sh":    "shell",
	".bash":  "shell",
	".zsh":   "shell",
	".sql":   "sql",
	".yaml":  "yaml",
	".yml":   "yaml",
	".json":  "json",
	".md":       "markdown",
	".mdx":      "markdown",
	".markdown": "markdown",
	".txt":      "text",
	".rst":      "rst",
	".adoc":     "asciidoc",
	".html":     "html",
	".htm":   "html",
	".css":   "css",
	".scss":  "css",
}
