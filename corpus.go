// Package tenetlint carries the rules and presets that ship inside the
// binary. It lives in the repository root because go:embed reads only the
// directory its file is in and below, and the corpus is what a reader of the
// repository should find first.
package tenetlint

import "embed"

// Corpus holds rules/<id>/{rule.yml,examples.yml,README.md} and
// presets/<name>.yml. Only internal/tenets reads it.
//
//go:embed rules presets
var Corpus embed.FS
