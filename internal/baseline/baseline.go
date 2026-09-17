// Package baseline records the findings a repository has decided to live
// with, so that a team can adopt tenetlint without fixing its whole history
// first and later runs block only what is new.
package baseline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zoidsh/tenetlint/internal/judge"
)

// Name is the file a run looks for in the repository root.
const Name = ".tenetlint-baseline.json"

// Version is the format, so that a later change retires the old file instead
// of misreading it.
const Version = 1

// Entry is one accepted finding. The file is repository relative, as the JSON
// report writes it, because the baseline outlives the directory a run started
// in.
type Entry struct {
	File  string `json:"file"`
	Tenet string `json:"tenet"`
	Hash  string `json:"hash"`
}

// File is a whole baseline.
type File struct {
	Version   int     `json:"version"`
	Generated string  `json:"generated"`
	Scope     Scope   `json:"scope"`
	Findings  []Entry `json:"findings"`
}

// The modes a run selects its code in, which are what Scope records.
const (
	ModePaths  = "paths"
	ModeStaged = "staged"
	ModeBase   = "base"
)

// Scope is what a run looked at. It is recorded because prune keeps only the
// entries a run still produces, and a run that never looked at a file
// produces nothing for it: without the scope, pruning from a narrower run
// would quietly accept every finding it could not see.
type Scope struct {
	Mode string `json:"mode"`

	// Paths are repository relative, so that the scope means the same thing
	// from any directory the run was started in.
	Paths []string `json:"paths,omitempty"`
	Base  string   `json:"base,omitempty"`
}

// Covers reports whether a run in this scope looked at everything a run in
// the other scope did. Modes are never compared with each other: what a diff
// against a ref holds is not something a list of paths can be measured
// against.
func (s Scope) Covers(other Scope) bool {
	if s.Mode != other.Mode {
		return false
	}
	switch s.Mode {
	case ModeStaged:
		return true
	case ModeBase:
		return s.Base == other.Base
	case ModePaths:
		for _, path := range other.Paths {
			if !s.holds(path) {
				return false
			}
		}
		return true
	}
	return false
}

func (s Scope) holds(path string) bool {
	for _, mine := range s.Paths {
		if mine == "." || mine == path || strings.HasPrefix(path, mine+"/") {
			return true
		}
	}
	return false
}

// String says what a scope is in the words its flags were written in.
func (s Scope) String() string {
	switch s.Mode {
	case ModeStaged:
		return "the staged changes"
	case ModeBase:
		return "the working tree against " + s.Base
	case ModePaths:
		return strings.Join(s.Paths, ", ")
	}
	return "nothing recorded"
}

// Entries are the findings as a baseline records them, in file, tenet and
// hash order.
func Entries(findings []judge.Finding) []Entry {
	entries := make([]Entry, 0, len(findings))
	for _, f := range findings {
		entries = append(entries, Entry{File: f.File, Tenet: f.Tenet, Hash: f.Hash})
	}
	sort.Slice(entries, func(a, b int) bool {
		x, y := entries[a], entries[b]
		if x.File != y.File {
			return x.File < y.File
		}
		if x.Tenet != y.Tenet {
			return x.Tenet < y.Tenet
		}
		return x.Hash < y.Hash
	})
	return entries
}

// Load reads a baseline. A missing file is an error the caller decides about,
// because the default path being absent is ordinary and a named one being
// absent is a typo.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Version != Version {
		return nil, fmt.Errorf("%s: baseline version %d, want %d; write it again with tenetlint baseline", path, f.Version, Version)
	}
	return &f, nil
}

// Save writes the entries and the scope they were found in, replacing
// whatever was there.
func Save(path string, scope Scope, entries []Entry, now time.Time) error {
	if entries == nil {
		entries = []Entry{}
	}
	data, err := json.MarshalIndent(File{
		Version:   Version,
		Generated: now.UTC().Format(time.RFC3339),
		Scope:     scope,
		Findings:  entries,
	}, "", "  ")
	if err != nil {
		return err
	}
	return replace(path, append(data, '\n'))
}

// replace writes the file in one step, through a temporary file beside it, so
// that an interrupted run leaves the baseline a repository is committing as it
// was rather than half written. It is a file to read and review, so it is
// readable to everyone, as the config it sits next to is.
func replace(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Split separates the findings a run should act on from the ones the baseline
// has already accepted. A nil baseline accepts nothing.
func (f *File) Split(findings []judge.Finding) (kept, baselined []judge.Finding) {
	if f == nil {
		return findings, nil
	}
	accepted := make(map[Entry]bool, len(f.Findings))
	for _, e := range f.Findings {
		accepted[e] = true
	}
	for _, finding := range findings {
		if accepted[Entry{File: finding.File, Tenet: finding.Tenet, Hash: finding.Hash}] {
			baselined = append(baselined, finding)
			continue
		}
		kept = append(kept, finding)
	}
	return kept, baselined
}

// Prune keeps the entries the current run still produces and reports how many
// it dropped. The caller has to have checked that the run covers the scope
// the baseline was written in.
func (f *File) Prune(current []Entry) (kept []Entry, dropped int) {
	produced := make(map[Entry]bool, len(current))
	for _, e := range current {
		produced[e] = true
	}
	for _, e := range f.Findings {
		if !produced[e] {
			dropped++
			continue
		}
		kept = append(kept, e)
	}
	return kept, dropped
}
