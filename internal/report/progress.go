package report

import (
	"fmt"
	"io"
	"strings"
)

// Progress is the line a lint leaves on the terminal while it waits for the
// model. It writes nothing anywhere but a terminal, so that a pipe, a hook
// reading stderr or a captured log never sees a half-line with no newline.
type Progress struct {
	w       io.Writer
	shown   string
	enabled bool
}

// NewProgress writes nothing until Start is called.
func NewProgress(w io.Writer, enabled bool) *Progress {
	return &Progress{w: w, enabled: enabled && isTerminal(w)}
}

// On reports whether anything will be written, so that a caller does no work
// to fill a line nobody is going to see.
func (p *Progress) On() bool { return p != nil && p.enabled }

// Start says what the run is about to ask for. A run with nothing left to ask
// is over before the line would be read.
func (p *Progress) Start(windows, cached int) {
	if !p.On() || cached >= windows {
		return
	}
	p.shown = fmt.Sprintf("linting %d windows (%d cached)…", windows, cached)
	_, _ = io.WriteString(p.w, p.shown)
}

// Clear takes the line back before anything is printed over it.
func (p *Progress) Clear() {
	if p == nil || p.shown == "" {
		return
	}
	_, _ = io.WriteString(p.w, "\r"+strings.Repeat(" ", len([]rune(p.shown)))+"\r")
	p.shown = ""
}
