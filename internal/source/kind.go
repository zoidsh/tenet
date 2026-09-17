package source

import (
	"path/filepath"
	"strings"
)

// The kinds of file tenetlint tells apart, because a rule about code and a
// rule about prose cannot be asked of the same framing.
const (
	KindCode  = "code"
	KindProse = "prose"
	KindData  = "data"
)

var proseExts = map[string]bool{
	".md":       true,
	".mdx":      true,
	".markdown": true,
	".rst":      true,
	".txt":      true,
	".adoc":     true,
}

var dataExts = map[string]bool{
	".json": true,
	".yaml": true,
	".yml":  true,
	".toml": true,
	".csv":  true,
	".xml":  true,
	".ini":  true,
	".lock": true,
}

var proseDirs = map[string]bool{
	"docs":    true,
	"locales": true,
	"i18n":    true,
}

// Files that are prose whatever they are written as, including with no
// extension at all.
var proseNames = map[string]bool{
	"readme":       true,
	"changelog":    true,
	"contributing": true,
	"license":      true,
}

// Kind names what a path holds. The name and extension are read before the
// directory, so that a data file keeps its kind under docs/.
func Kind(path string) string {
	path = filepath.ToSlash(path)
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(base))
	name := strings.TrimSuffix(base, ext)

	switch {
	case proseExts[ext], proseNames[name]:
		return KindProse
	case dataExts[ext]:
		return KindData
	}
	for _, dir := range strings.Split(filepath.ToSlash(filepath.Dir(path)), "/") {
		if proseDirs[dir] {
			return KindProse
		}
	}
	return KindCode
}

// Kind names what this file holds for the model.
func (f *File) Kind() string { return Kind(f.Path) }
