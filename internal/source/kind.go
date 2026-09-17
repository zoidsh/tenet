package source

import (
	"path/filepath"
	"strings"
)

// A rule about code and a rule about prose cannot be asked of the same
// framing.
const (
	KindCode   = "code"
	KindProse  = "prose"
	KindData   = "data"
	KindCommit = "commit"
	KindPR     = "pr"
)

// IsKind reports whether a name is one of the kinds. An example's lang is
// allowed to name one, to choose among the kinds its tenet declares, so a
// caller reading a lang has to tell that apart from a language.
func IsKind(name string) bool {
	switch name {
	case KindCode, KindProse, KindData, KindCommit, KindPR:
		return true
	}
	return false
}

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

// Lock files that carry no extension to read them by.
var dataNames = map[string]bool{
	"go.sum":      true,
	"go.work.sum": true,
}

// A directory of documents, whose contents an extension may still overrule.
var proseDirs = map[string]bool{
	"docs": true,
}

// Directories that hold translated strings, which are prose whatever they are
// serialised as.
var translationDirs = map[string]bool{
	"locales": true,
	"i18n":    true,
}

// Prose whatever they are written as, including with no extension at all.
var proseNames = map[string]bool{
	"readme":       true,
	"changelog":    true,
	"contributing": true,
	"license":      true,
}

// Kind reads a data extension before the directory, so that a data file keeps
// its kind under docs/, and a translation directory before either, because
// what those hold is prose however it is serialised.
func Kind(path string) string {
	path = filepath.ToSlash(path)
	if path == CommitMsgPath {
		return KindCommit
	}
	if path == PRTextPath {
		return KindPR
	}
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(base))
	name := strings.TrimSuffix(base, ext)
	dirs := strings.Split(filepath.ToSlash(filepath.Dir(path)), "/")

	if proseExts[ext] || proseNames[name] {
		return KindProse
	}
	for _, dir := range dirs {
		if translationDirs[dir] {
			return KindProse
		}
	}
	if dataExts[ext] || dataNames[base] {
		return KindData
	}
	for _, dir := range dirs {
		if proseDirs[dir] {
			return KindProse
		}
	}
	return KindCode
}

// Kind is the framing the model is shown this file under.
func (f *File) Kind() string { return Kind(f.Path) }

// KindForLanguage is the kind of file a language is written in, which is how
// a snippet that never came from disk is framed. A language tenetlint does
// not know is code, the kind a rule is most often about.
func KindForLanguage(lang string) string {
	if ext := ExtensionFor(lang); ext != "" {
		return Kind("example" + ext)
	}
	return KindCode
}
