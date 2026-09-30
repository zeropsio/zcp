package bundle

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// A tier is written for people. The group repo is where a person reads what
// a stage or a production is made of and where they change it, so a tier is
// not yaml.Marshal of a map — alphabetical keys, four-space indentation, no
// word of explanation — but a document laid out the way the published
// recipes are: two spaces, every service opening on its hostname and its
// type and then the rest in the order a person asks about it, a blank line
// between services, and a header saying what the tier is and where it came
// from. yaml.v3 still writes every scalar, so a value is quoted exactly when
// YAML needs it to stay the string it is.

// yamlField is one key of a tier's mapping, written in the order given.
type yamlField struct {
	key string
	// value is a string, an int, a float64, a bool, or a nested []yamlField.
	value any
	// comment is written above the key, one `#` line per line.
	comment string
}

// yamlItem is one service: its fields, and a comment above the item.
type yamlItem struct {
	comment string
	fields  []yamlField
}

// tierDocument is one tier's whole import.yaml before it is written.
type tierDocument struct {
	// header is the comment the file opens with: paragraphs, each wrapped.
	header   []string
	project  []yamlField
	services []yamlItem
}

// serviceKeyOrder is the order a service's keys are written in: what the
// service is, when it is created, what builds it, how it is reached, what it
// stores, how it scales, and its secrets last.
var serviceKeyOrder = []string{
	"hostname", "type", "priority", "mode", "profile", "zeropsSetup", "buildFromGit",
	"enableSubdomainAccess", "objectStorageSize", "objectStoragePolicy",
	"minContainers", "maxContainers", "verticalAutoscaling", "envSecrets",
}

// verticalKeyOrder is verticalAutoscaling's: the CPU mode, then each resource
// as a min/max pair.
var verticalKeyOrder = []string{"cpuMode", "minCpu", "maxCpu", "minRam", "maxRam", "minDisk", "maxDisk"}

// orderedFields turns a composed entry into fields in the given order. A key
// the order does not name is written after the named ones, sorted — never
// dropped.
func orderedFields(entry map[string]any, order []string) []yamlField {
	fields := make([]yamlField, 0, len(entry))
	for _, key := range order {
		if value, ok := entry[key]; ok {
			fields = append(fields, yamlField{key: key, value: fieldValue(key, value)})
		}
	}
	var rest []string
	for key := range entry {
		if !slices.Contains(order, key) {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	for _, key := range rest {
		fields = append(fields, yamlField{key: key, value: fieldValue(key, entry[key])})
	}
	return fields
}

// fieldValue normalizes a composed value for the emitter: a nested mapping
// becomes fields in its own order.
func fieldValue(key string, value any) any {
	switch v := value.(type) {
	case map[string]any:
		if key == "verticalAutoscaling" {
			return orderedFields(v, verticalKeyOrder)
		}
		return orderedFields(v, nil)
	case map[string]string:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fields := make([]yamlField, 0, len(v))
		for _, k := range keys {
			fields = append(fields, yamlField{key: k, value: v[k]})
		}
		return fields
	default:
		return value
	}
}

// render writes the document: the preprocessor's line first when a value
// needs it, the header, the project, and the services a blank line apart.
func (d tierDocument) render() (string, error) {
	var b strings.Builder
	if d.usesPreprocessor() {
		b.WriteString(preprocessorHeader)
	}
	for i, paragraph := range d.header {
		if i > 0 {
			b.WriteString("#\n")
		}
		writeCommentLines(&b, paragraph, "")
	}
	if len(d.header) > 0 {
		b.WriteString("\n")
	}

	project, err := encodeNode(fieldsNode([]yamlField{{key: "project", value: d.project}}))
	if err != nil {
		return "", fmt.Errorf("project: %w", err)
	}
	b.WriteString(project)

	b.WriteString("\nservices:\n")
	for i, item := range d.services {
		if i > 0 {
			b.WriteString("\n")
		}
		node := fieldsNode(item.fields)
		node.HeadComment = item.comment
		out, err := encodeNode(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{node}})
		if err != nil {
			return "", fmt.Errorf("service %d: %w", i, err)
		}
		b.WriteString(indentLines(out, "  "))
	}
	return b.String(), nil
}

// usesPreprocessor reports whether any value carries a `<@…>` directive,
// which the platform expands only under the preprocessor's first line.
func (d tierDocument) usesPreprocessor() bool {
	if fieldsUsePreprocessor(d.project) {
		return true
	}
	for _, item := range d.services {
		if fieldsUsePreprocessor(item.fields) {
			return true
		}
	}
	return false
}

func fieldsUsePreprocessor(fields []yamlField) bool {
	for _, f := range fields {
		switch v := f.value.(type) {
		case string:
			if strings.Contains(v, "<@") && strings.Contains(v, ")>") {
				return true
			}
		case []yamlField:
			if fieldsUsePreprocessor(v) {
				return true
			}
		}
	}
	return false
}

// fieldsNode builds a mapping node from fields, each key carrying its comment.
func fieldsNode(fields []yamlField) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, f := range fields {
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: f.key, HeadComment: f.comment}
		node.Content = append(node.Content, key, valueNode(f.value))
	}
	return node
}

// valueNode builds a scalar or a nested mapping. Every string is tagged a
// string, so "123", "true" or "" stay strings where YAML would read a number,
// a boolean or a null.
func valueNode(value any) *yaml.Node {
	switch v := value.(type) {
	case []yamlField:
		return fieldsNode(v)
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)}
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1e15 {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(v), 10)}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(v, 'f', -1, 64)}
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fmt.Sprint(v)}
	}
}

// encodeNode writes a node with two-space indentation.
func encodeNode(node *yaml.Node) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}
	return buf.String(), nil
}

// indentLines shifts every non-empty line by indent. An empty line stays
// empty, so a blank line inside a block scalar keeps meaning the same.
func indentLines(text, indent string) string {
	lines := strings.SplitAfter(text, "\n")
	var b strings.Builder
	for _, line := range lines {
		if strings.TrimRight(line, "\n") != "" {
			b.WriteString(indent)
		}
		b.WriteString(line)
	}
	return b.String()
}

// commentWidth is where a comment wraps, the published recipes' own width.
const commentWidth = 80

// writeCommentLines writes text as `#` lines at indent, wrapped at word
// boundaries.
func writeCommentLines(b *strings.Builder, text, indent string) {
	width := max(commentWidth-len(indent)-2, 20)
	for _, line := range wrapWords(text, width) {
		if line == "" {
			fmt.Fprintf(b, "%s#\n", indent)
			continue
		}
		fmt.Fprintf(b, "%s# %s\n", indent, line)
	}
}

// wrapWords wraps text at word boundaries; an explicit newline starts a new
// line.
func wrapWords(text string, width int) []string {
	var out []string
	for paragraph := range strings.SplitSeq(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if len(line)+1+len(word) > width {
				out = append(out, line)
				line = word
				continue
			}
			line += " " + word
		}
		out = append(out, line)
	}
	return out
}
