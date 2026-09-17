package cache_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zoidsh/tenetlint/internal/cache"
)

func TestRoundTrip(t *testing.T) {
	c, err := cache.Open(filepath.Join(t.TempDir(), "nested"))
	if err != nil {
		t.Fatal(err)
	}
	key := cache.Key("L001 x := 1", "hash")

	if _, ok := c.Get(key); ok {
		t.Fatal("empty cache reported a hit")
	}

	c.PutVerdict(key, 0.83)
	e, ok := c.Get(key)
	if !ok || e.Prob != 0.83 {
		t.Fatalf("verdict round trip: %#v, %v", e, ok)
	}
	if e.Located() {
		t.Error("a verdict alone should not count as a location")
	}
	if e.At == 0 {
		t.Error("no timestamp recorded")
	}

	c.PutLocation(key, 0.83, "L017")
	e, ok = c.Get(key)
	if !ok || !e.Located() || e.Line != "L017" || e.Prob != 0.83 {
		t.Fatalf("location round trip: %#v", e)
	}
}

func TestKeyIsContentAddressed(t *testing.T) {
	base := cache.Key("text", "hash")
	if base == cache.Key("text!", "hash") {
		t.Error("changing the window kept the key")
	}
	if base == cache.Key("text", "hash!") {
		t.Error("changing the tenet kept the key")
	}
	if base != cache.Key("text", "hash") {
		t.Error("the key is not stable")
	}
}

func TestCorruptEntryIsAMiss(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := cache.Key("text", "hash")
	c.PutVerdict(key, 0.5)
	if err := os.WriteFile(filepath.Join(dir, key+".json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(key); ok {
		t.Error("a corrupt entry was read as a hit")
	}
}

func TestNilCacheIsAlwaysAMiss(t *testing.T) {
	var c *cache.Cache
	c.PutVerdict("key", 1)
	if _, ok := c.Get("key"); ok {
		t.Error("the bypassed cache reported a hit")
	}
}
