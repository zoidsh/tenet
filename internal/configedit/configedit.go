// Package configedit rewrites a config file in place, leaving everything it
// was not asked to change as the person who wrote it left it: the comments,
// the order of the keys and the blank lines between the entries.
package configedit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// File is a parsed config held as nodes rather than as the typed config,
// because a typed decode and re-encode throws away everything YAML does not
// carry as data and a person's config is mostly the parts it would lose.
type File struct {
	doc  yaml.Node
	root *yaml.Node

	// endsInNewline is whether the file on disk did, so that a sync does not
	// show up in a diff as a change to its last line.
	endsInNewline bool
}

// Parse reads a config for editing. A file written with CRLF endings is read
// as if it had LF ones, because the encoder writes LF whatever it was given
// and the blank lines are counted off the same text the parser saw.
func Parse(data []byte) (*File, error) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	f := &File{endsInNewline: len(data) == 0 || bytes.HasSuffix(data, []byte("\n"))}
	if err := yaml.Unmarshal(data, &f.doc); err != nil {
		return nil, err
	}
	if len(f.doc.Content) != 1 || f.doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the config is not a mapping of settings")
	}
	f.root = f.doc.Content[0]
	keepBlankLines(&f.doc, strings.Split(string(data), "\n"))
	return f, nil
}

// Bytes is the file as it should now be written.
func (f *File) Bytes() ([]byte, error) {
	var b bytes.Buffer
	encoder := yaml.NewEncoder(&b)
	encoder.SetIndent(2)
	if err := encoder.Encode(&f.doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	out := unindentBlanks(b.Bytes())
	if !f.endsInNewline {
		out = bytes.TrimRight(out, "\n")
	}
	return out, nil
}

// Write replaces a config with the given bytes through a temporary file, so
// that a run killed part way through leaves the config it was editing whole.
// The file keeps the permissions it had: a config is committed, and a repository
// that made its own group-readable should not find that undone by a sync.
func Write(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// value is the node a top-level key holds, nil when the config has no such
// key.
func (f *File) value(key string) *yaml.Node {
	for i := 0; i+1 < len(f.root.Content); i += 2 {
		if f.root.Content[i].Value == key {
			return f.root.Content[i+1]
		}
	}
	return nil
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// set points a mapping's key at a string, adding the key when the mapping has
// none. The style is cleared so that a sentence needing quotes gets them and
// one that does not loses them.
func set(mapping *yaml.Node, key, value string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			node := mapping.Content[i+1]
			node.Kind, node.Tag, node.Style, node.Value = yaml.ScalarNode, "!!str", 0, value
			node.Content = nil
			return
		}
	}
	mapping.Content = append(mapping.Content, scalar(key), scalar(value))
}

func text(mapping *yaml.Node, key string) string {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1].Value
		}
	}
	return ""
}
