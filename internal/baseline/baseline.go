// Package baseline records the findings a repository has decided to live
// with, so that a team can adopt tenetlint without fixing its whole history
// first and later runs block only what is new.
package baseline

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
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
	Findings  []Entry `json:"findings"`
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

// Save writes the entries, replacing whatever was there.
func Save(path string, entries []Entry, now time.Time) error {
	if entries == nil {
		entries = []Entry{}
	}
	data, err := json.MarshalIndent(File{
		Version:   Version,
		Generated: now.UTC().Format(time.RFC3339),
		Findings:  entries,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
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
// it dropped.
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
