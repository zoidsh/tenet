// Package cache remembers what the model answered about a window, so that an
// unchanged window is never paid for twice.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Version is part of the directory name so that a change to the entry format
// retires the old entries instead of misreading them.
const Version = "v1"

// Entry is one question's answer about one piece of text. Line is empty until
// a location has been asked for, which only happens once the verdict passes.
// Kind and KindProb hold the chosen label of a second, categorising question,
// which the importer's sort asks alongside its probability and the judge does
// not ask at all.
type Entry struct {
	Prob     float64 `json:"p"`
	Line     string  `json:"line"`
	Kind     string  `json:"kind,omitempty"`
	KindProb float64 `json:"kind_p,omitempty"`
	At       int64   `json:"at"`
}

// Located reports whether the entry also holds the answer to the location
// question.
func (e Entry) Located() bool { return e.Line != "" }

// Sorted reports whether the entry holds a categorising answer.
func (e Entry) Sorted() bool { return e.Kind != "" }

// Cache is a directory of answers, keyed by what was asked.
type Cache struct {
	dir    string
	now    func() time.Time
	noRead bool
}

// WriteOnly makes every read a miss while the writes go on. It is what asking
// for a fresh answer means: the run after it should not be cold as well.
func (c *Cache) WriteOnly() {
	c.noRead = true
}

// Open prepares the cache directory, defaulting to the user's cache home.
func Open(dir string) (*Cache, error) {
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(base, "tenetlint", Version)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Cache{dir: dir, now: time.Now}, nil
}

// Key identifies a question: the exact text the model is shown and the tenet
// it is asked about.
func Key(windowText, tenetHash string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(windowText))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(tenetHash))
	return hex.EncodeToString(h.Sum(nil))
}

// Get reads an entry. A missing or unreadable entry is a miss, never an error:
// the answer can always be asked for again.
func (c *Cache) Get(key string) (Entry, bool) {
	if c == nil || c.noRead {
		return Entry{}, false
	}
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return Entry{}, false // tenet:ignore no-fallback
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, false
	}
	return e, true
}

// PutVerdict records a probability, dropping any location held for the key.
func (c *Cache) PutVerdict(key string, prob float64) {
	c.put(key, Entry{Prob: prob})
}

// PutLocation records the line a passing verdict was placed on.
func (c *Cache) PutLocation(key string, prob float64, line string) {
	c.put(key, Entry{Prob: prob, Line: line})
}

// PutSort records a label with its probability and a second probability about
// the same text.
func (c *Cache) PutSort(key, kind string, kindProb, prob float64) {
	c.put(key, Entry{Prob: prob, Kind: kind, KindProb: kindProb})
}

// put writes an entry, ignoring failures: a cache that cannot be written only
// costs another call. The write goes to a temporary file first, so that a run
// killed mid-write, or two runs writing the same key at once, cannot leave a
// half-written entry behind.
func (c *Cache) put(key string, e Entry) {
	if c == nil {
		return
	}
	e.At = c.now().Unix()
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	temp, err := os.CreateTemp(c.dir, key+".*")
	if err != nil {
		return
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return
	}
	if err := os.Chmod(temp.Name(), 0o600); err != nil {
		_ = os.Remove(temp.Name())
		return
	}
	if err := os.Rename(temp.Name(), c.path(key)); err != nil {
		_ = os.Remove(temp.Name())
	}
}

func (c *Cache) path(key string) string {
	return filepath.Join(c.dir, key+".json")
}
