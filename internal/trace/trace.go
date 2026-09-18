// Package trace writes down what a run sent to the model and what came back,
// as one file per window and a run.json for the run itself.
//
// The files hold the source text of everything judged, so the directory is as
// secret as the code is: it is emptied at the start of every traced run and
// carries a .gitignore of its own, so that nothing is committed and nothing
// survives into a later run to be read as if it belonged to it.
package trace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/judge"
)

// DirName is the trace directory tenet keeps beside its config.
const DirName = "trace"

// RunFile names the run's own file, which a directory always has, even when a
// receipt excused the run and there was no window to write.
const RunFile = "run.json"

// Marker is the first line of the .gitignore a trace directory carries. It is
// what tells a directory tenet wrote, and may empty again, from one somebody
// pointed --trace-dir at by mistake.
const Marker = "# written by tenet: emptied at the start of every traced run"

const gitignore = Marker + "\n*\n"

// minPad keeps the sequence three digits wide however small the run is, so
// that a trace of ten windows and one of a thousand read the same way.
const minPad = 3

// linePad is how wide a line number is written. The halves of one window
// share its sequence number, so their line ranges are all a listing has to
// order them by, and an unpadded 9 sorts after an unpadded 10.
const linePad = 5

// Writer fills one trace directory. Windows are judged concurrently, so it
// takes the writes one at a time.
type Writer struct {
	dir string
	pad int
	mu  sync.Mutex
}

// Open empties the directory, marks it as tenet's and returns a writer for the
// given number of windows. It refuses a directory that holds something tenet
// did not write, because the flag that names one is a typo away from naming a
// directory of somebody's own.
func Open(dir string, windows int) (*Writer, error) {
	if err := empty(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		return nil, err
	}
	return &Writer{dir: dir, pad: max(minPad, len(strconv.Itoa(windows)))}, nil
}

// Dir is where the trace is being written, for a caller that has to say so.
func (w *Writer) Dir() string { return w.dir }

func empty(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) > 0 && !ours(dir) {
		return fmt.Errorf("%s holds files tenet did not write, so it will not be emptied: name an empty directory or one of tenet's own", dir)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func ours(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(data), Marker+"\n")
}

type windowJSON struct {
	File       string                  `json:"file"`
	Lines      [2]int                  `json:"lines"`
	Tenets     []string                `json:"tenets"`
	State      string                  `json:"state"`
	Questions  map[string]jev.Question `json:"questions"`
	Answers    map[string]jev.Answer   `json:"answers"`
	Cached     bool                    `json:"cached"`
	Model      string                  `json:"model"`
	Tokens     int                     `json:"tokens"`
	CostUSD    float64                 `json:"cost_usd"`
	DurationMS int64                   `json:"duration_ms"`
}

// Window writes one window's exchange with the model.
func (w *Writer) Window(t judge.Trace) error {
	out := windowJSON{
		File:       t.File,
		Lines:      [2]int{t.First, t.Last},
		Tenets:     t.Tenets,
		State:      t.State,
		Questions:  t.Questions,
		Answers:    t.Answers,
		Cached:     t.Cached,
		Model:      t.Model,
		Tokens:     t.InputTokens,
		CostUSD:    jev.RoundCost(t.CostUSD),
		DurationMS: t.Duration.Milliseconds(),
	}
	if out.Tenets == nil {
		out.Tenets = []string{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return write(filepath.Join(w.dir, w.name(t)), out)
}

// name sorts a listing of the directory into the order the windows were
// dispatched in, and says which lines of which file each file holds.
func (w *Writer) name(t judge.Trace) string {
	return fmt.Sprintf("%0*d-%s-%0*d-%0*d.json", w.pad, t.Seq, slug(t.File), linePad, t.First, linePad, t.Last)
}

var slugged = strings.NewReplacer("/", "-", `\`, "-")

func slug(path string) string { return slugged.Replace(path) }

// Run is what the run as a whole was, so that a trace read later says which
// tree the windows in it came out of.
type Run struct {
	Command []string
	Config  string
	Model   string
	Head    string
	Tree    string
	Stats   judge.Stats

	// Excused says a receipt answered the run: nothing was collected, asked or
	// judged, which is why the directory holds no window.
	Excused bool
}

type runJSON struct {
	Command []string  `json:"command"`
	Config  string    `json:"config"`
	Model   string    `json:"model"`
	Head    string    `json:"head,omitempty"`
	Tree    string    `json:"tree,omitempty"`
	Excused bool      `json:"excused,omitempty"`
	Stats   statsJSON `json:"stats"`
}

type statsJSON struct {
	Files       int     `json:"files"`
	Windows     int     `json:"windows"`
	Calls       int     `json:"calls"`
	CacheHits   int     `json:"cache_hits"`
	InputTokens int     `json:"input_tokens"`
	CostUSD     float64 `json:"cost_usd"`
	DurationMS  int64   `json:"duration_ms"`
}

// Run writes the run's own file.
func (w *Writer) Run(r Run) error {
	out := runJSON{
		Command: r.Command,
		Config:  r.Config,
		Model:   r.Model,
		Head:    r.Head,
		Tree:    r.Tree,
		Excused: r.Excused,
		Stats: statsJSON{
			Files:       r.Stats.Files,
			Windows:     r.Stats.Windows,
			Calls:       r.Stats.Calls,
			CacheHits:   r.Stats.CacheHits,
			InputTokens: r.Stats.InputTokens,
			CostUSD:     jev.RoundCost(r.Stats.CostUSD),
			DurationMS:  r.Stats.Duration.Milliseconds(),
		},
	}
	if out.Command == nil {
		out.Command = []string{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return write(filepath.Join(w.dir, RunFile), out)
}

func write(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	// A trace carries source text, which is unreadable with every < and & of
	// it escaped for a web page it is never going into.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
