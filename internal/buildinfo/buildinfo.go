// Package buildinfo carries values stamped into the binary at link time.
package buildinfo

// Stamped by goreleaser through -ldflags -X, so it must stay a package-level
// string variable with this exact name.
var version = "dev"

// Version reports the release this binary was built from.
func Version() string { return version }
