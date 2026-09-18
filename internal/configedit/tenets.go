package configedit

import (
	"gopkg.in/yaml.v3"
)

// Tenet is one entry of the tenets: sequence, with the two fields sync
// rewrites when a rule file rewords the sentence behind it.
type Tenet struct {
	ID     string
	Tenet  string
	Source string

	mapping *yaml.Node
}

// Tenets are the tenets the file writes out itself, in the order it writes
// them. Presets and built-in rules are named rather than written, so they are
// not here.
func (f *File) Tenets() []*Tenet {
	seq := f.value("tenets")
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	var out []*Tenet
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		out = append(out, &Tenet{
			ID:      text(item, "id"),
			Tenet:   text(item, "tenet"),
			Source:  text(item, "source"),
			mapping: item,
		})
	}
	return out
}

// SetSource is safe to call on a calibrated tenet: the source is provenance,
// and what a tenet is judged by hashes its id, its sentence and its criteria.
func (t *Tenet) SetSource(source string) {
	t.Source = source
	set(t.mapping, "source", source)
}

// SetTenet changes what the tenet is judged by, which retires every cached
// answer and every baselined finding held against the old wording.
func (t *Tenet) SetTenet(tenet string) {
	t.Tenet = tenet
	set(t.mapping, "tenet", tenet)
}

// Draft is a tenet to append. The fields are written in this order, the order
// tenet init drafts them in, so that a config grown by sync reads like one
// drafted in a single run.
type Draft struct {
	ID     string   `yaml:"id"`
	Tenet  string   `yaml:"tenet"`
	Kind   []string `yaml:"kind,omitempty,flow"`
	Source string   `yaml:"source"`
}

// Append adds drafts to the end of the tenets: sequence, making the key when
// the config names no tenets of its own.
func (f *File) Append(drafts ...Draft) error {
	if len(drafts) == 0 {
		return nil
	}
	seq, err := f.tenetSequence()
	if err != nil {
		return err
	}
	for _, draft := range drafts {
		var node yaml.Node
		if err := node.Encode(draft); err != nil {
			return err
		}
		seq.Content = append(seq.Content, &node)
	}
	return nil
}

// tenetSequence is the sequence to append to. A config with no tenets: key
// gains one; a `tenets: []` or a `tenets:` holding nothing becomes a block
// sequence, because an entry written out in flow style is unreadable.
func (f *File) tenetSequence() (*yaml.Node, error) {
	node := f.value("tenets")
	if node == nil {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		f.root.Content = append(f.root.Content, scalar("tenets"), seq)
		return seq, nil
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		node.Kind, node.Tag, node.Style, node.Value = yaml.SequenceNode, "!!seq", 0, ""
		return node, nil
	}
	if node.Kind != yaml.SequenceNode {
		return nil, errTenetsNotASequence
	}
	if len(node.Content) == 0 {
		node.Style = 0
	}
	return node, nil
}
