package ops

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// The supervisor's two lines go into the user's zerops.yaml as a splice into
// the bytes the user wrote (orchestrator decision A, 2026-10-15): re-encoding
// the parsed document would drop blank lines, re-indent lists and rewrite
// comments and flow maps. The splice is checked by decoding both files — the
// result must equal the original with exactly the supervisor's values set —
// so a shape the splicer misreads is refused, never written.

// supervisorLines are the run.prepareCommands item and the run.start a
// supervised dev setup carries.
type supervisorLines struct{ prepare, start string }

// splice replaces bytes [from, to) of line (0-based) with text, and drops the
// lines after it through dropThrough when that is past line.
type splice struct {
	line, from, to int
	text           string
	dropThrough    int
}

// yamlSource is a zerops.yaml's lines and the positions of its nodes.
type yamlSource struct {
	lines []string
	eol   string
	// starts is every node's 1-based start line, sorted.
	starts []int
}

func newYAMLSource(data []byte, doc *yaml.Node) *yamlSource {
	text := string(data)
	src := &yamlSource{lines: strings.Split(text, "\n"), eol: "\n"}
	if strings.Contains(text, "\r\n") {
		src.eol = "\r\n"
		for i := range src.lines {
			src.lines[i] = strings.TrimSuffix(src.lines[i], "\r")
		}
	}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind != yaml.DocumentNode {
			src.starts = append(src.starts, n.Line)
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(doc)
	slices.Sort(src.starts)
	return src
}

// lastLine is the 1-based line a node's own text ends on: the line before the
// next node outside it, less trailing blank and comment-only lines.
func (s *yamlSource) lastLine(n *yaml.Node) int {
	deepest := subtreeLastStart(n)
	end := len(s.lines)
	if i, _ := slices.BinarySearch(s.starts, deepest+1); i < len(s.starts) {
		end = s.starts[i] - 1
	}
	for end > deepest {
		trimmed := strings.TrimSpace(s.lines[end-1])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		end--
	}
	return end
}

// subtreeLastStart is the largest start line within n.
func subtreeLastStart(n *yaml.Node) int {
	last := n.Line
	for _, c := range n.Content {
		last = max(last, subtreeLastStart(c))
	}
	return last
}

// offset is the byte offset of a node's 1-based column on its line.
func (s *yamlSource) offset(n *yaml.Node) int {
	line := s.lines[n.Line-1]
	off := 0
	for range n.Column - 1 {
		if off >= len(line) {
			break
		}
		_, size := utf8.DecodeRuneInString(line[off:])
		off += size
	}
	return off
}

// scalarEnd is the byte offset just past a scalar that sits on one line, -1
// when it does not (a block scalar or one continued on the next line).
func (s *yamlSource) scalarEnd(n *yaml.Node, flow bool) int {
	if n.Kind != yaml.ScalarNode || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return -1
	}
	if !flow && s.lastLine(n) != n.Line {
		return -1
	}
	line, start := s.lines[n.Line-1], s.offset(n)
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		for i := start + 1; i < len(line); i++ {
			switch line[i] {
			case '\\':
				i++
			case '"':
				return i + 1
			}
		}
		return -1
	case n.Style&yaml.SingleQuotedStyle != 0:
		for i := start + 1; i < len(line); i++ {
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				return i + 1
			}
		}
		return -1
	}
	end := len(line)
	for i := start; i < len(line); i++ {
		if line[i] == '#' && i > start && (line[i-1] == ' ' || line[i-1] == '\t') {
			end = i
			break
		}
		if flow && strings.IndexByte(",]}", line[i]) >= 0 {
			end = i
			break
		}
	}
	return start + len(strings.TrimRight(line[start:end], " \t"))
}

// apply returns the source with every splice made; splices at the same
// position land in the order given.
func (s *yamlSource) apply(splices []splice) []byte {
	lines := slices.Clone(s.lines)
	splices = slices.Clone(splices)
	slices.Reverse(splices)
	slices.SortStableFunc(splices, func(a, b splice) int {
		if a.line != b.line {
			return b.line - a.line
		}
		return b.from - a.from
	})
	for _, sp := range splices {
		line := lines[sp.line]
		lines[sp.line] = line[:sp.from] + sp.text + line[sp.to:]
		if sp.dropThrough > sp.line {
			lines = slices.Delete(lines, sp.line+1, sp.dropThrough+1)
		}
	}
	return []byte(strings.Join(lines, s.eol))
}

// scalarText is s as a YAML scalar in a block (flow false) or flow context:
// plain when it reads back unchanged, single-quoted otherwise.
func scalarText(s string, flow bool) string {
	wrapped := "k: " + s
	if flow {
		wrapped = "k: [" + s + "]"
	}
	var probe map[string]any
	if yaml.Unmarshal([]byte(wrapped), &probe) == nil {
		got := probe["k"]
		if flow {
			if list, ok := got.([]any); ok && len(list) == 1 {
				got = list[0]
			}
		}
		if got == s && !strings.ContainsAny(s, "\n\r") {
			return s
		}
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// supervisorSplices are the splices that put setup's run under the supervisor
// with the install line line; entry is the setup's mapping, run its run
// mapping (nil when absent). None when both lines are there.
func supervisorSplices(src *yamlSource, entry, run *yaml.Node, setup string, want supervisorLines) ([]splice, error) {
	if run == nil {
		if entry.Style&yaml.FlowStyle != 0 || len(entry.Content) == 0 {
			return nil, fmt.Errorf("zerops.yaml setup %q: its mapping is written inline; add a run: block with start: %s by hand", setup, want.start)
		}
		indent := strings.Repeat(" ", entry.Content[0].Column-1)
		end := src.lastLine(entry)
		text := strings.Join([]string{
			indent + "run:",
			indent + "  prepareCommands:",
			indent + "    - " + scalarText(want.prepare, false),
			indent + "  start: " + scalarText(want.start, false),
		}, src.eol)
		return []splice{{line: end - 1, from: len(src.lines[end-1]), to: len(src.lines[end-1]), text: src.eol + text}}, nil
	}
	flow := run.Style&yaml.FlowStyle != 0
	var added []string
	var out []splice
	prepare := mappingValue(run, "prepareCommands")
	switch {
	case prepare == nil:
		if flow {
			added = append(added, "prepareCommands: ["+scalarText(want.prepare, true)+"]")
		} else {
			added = append(added, "prepareCommands:", "  - "+scalarText(want.prepare, false))
		}
	case prepare.Kind != yaml.SequenceNode:
		return nil, fmt.Errorf("zerops.yaml setup %q: run.prepareCommands is not a list", setup)
	default:
		sp, err := prepareSplice(src, prepare, setup, want.prepare)
		if err != nil {
			return nil, err
		}
		out = append(out, sp...)
	}
	start := mappingValue(run, "start")
	switch {
	case start == nil:
		added = append(added, "start: "+scalarText(want.start, flow))
	case start.Kind == yaml.ScalarNode && strings.TrimSpace(start.Value) == want.start:
	default:
		sp, err := startSplice(src, run, start, flow, setup, want.start)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	if len(added) == 0 {
		return out, nil
	}
	if flow {
		open := src.offset(run)
		text := strings.Join(added, ", ")
		if len(run.Content) > 0 {
			text += ", "
		}
		return append(out, splice{line: run.Line - 1, from: open + 1, to: open + 1, text: text}), nil
	}
	if len(run.Content) == 0 {
		return nil, fmt.Errorf("zerops.yaml setup %q: run is empty; add start: %s by hand", setup, want.start)
	}
	indent := strings.Repeat(" ", run.Content[0].Column-1)
	end := src.lastLine(run)
	for i := range added {
		added[i] = indent + added[i]
	}
	return append(out, splice{line: end - 1, from: len(src.lines[end-1]), to: len(src.lines[end-1]), text: src.eol + strings.Join(added, src.eol)}), nil
}

// prepareSplice puts line into the run.prepareCommands list: in place of an
// older zcp-devsup line, else after the last item. None when it is there.
func prepareSplice(src *yamlSource, prepare *yaml.Node, setup, line string) ([]splice, error) {
	flow := prepare.Style&yaml.FlowStyle != 0
	found := false
	var out []splice
	for _, item := range prepare.Content {
		if item.Kind != yaml.ScalarNode || !strings.Contains(item.Value, "/"+DevsupAsset) {
			continue
		}
		found = true
		if item.Value == line {
			continue
		}
		end := src.scalarEnd(item, flow)
		if end < 0 {
			return nil, fmt.Errorf("zerops.yaml setup %q: the zcp-devsup line in run.prepareCommands spans several lines; replace it by hand with: %s", setup, line)
		}
		out = append(out, splice{line: item.Line - 1, from: src.offset(item), to: end, text: scalarText(line, flow)})
	}
	if found {
		return out, nil
	}
	if flow {
		if len(prepare.Content) == 0 {
			open := src.offset(prepare)
			return []splice{{line: prepare.Line - 1, from: open + 1, to: open + 1, text: scalarText(line, true)}}, nil
		}
		last := prepare.Content[len(prepare.Content)-1]
		end := src.scalarEnd(last, true)
		if end < 0 {
			return nil, fmt.Errorf("zerops.yaml setup %q: run.prepareCommands ends in a value zcp cannot place a line after; add %q to it by hand", setup, line)
		}
		return []splice{{line: last.Line - 1, from: end, to: end, text: ", " + scalarText(line, true)}}, nil
	}
	end := src.lastLine(prepare)
	text := strings.Repeat(" ", prepare.Column-1) + "- " + scalarText(line, false)
	return []splice{{line: end - 1, from: len(src.lines[end-1]), to: len(src.lines[end-1]), text: src.eol + text}}, nil
}

// startSplice replaces run.start's value with the supervisor's line.
func startSplice(src *yamlSource, run, start *yaml.Node, flow bool, setup, line string) (splice, error) {
	if end := src.scalarEnd(start, flow); end >= 0 {
		return splice{line: start.Line - 1, from: src.offset(start), to: end, text: scalarText(line, flow)}, nil
	}
	if flow {
		return splice{}, fmt.Errorf("zerops.yaml setup %q: run.start spans several lines; set it to %s by hand", setup, line)
	}
	var key *yaml.Node
	for i := 0; i+1 < len(run.Content); i += 2 {
		if run.Content[i+1] == start {
			key = run.Content[i]
		}
	}
	keyLine := src.lines[key.Line-1]
	return splice{line: key.Line - 1, from: src.offset(key), to: len(keyLine), text: "start: " + scalarText(line, false), dropThrough: src.lastLine(start) - 1}, nil
}

// spliceVerified reports whether out decodes to data with exactly setup's
// supervisor values set — the splice changed nothing else; an empty
// want.start leaves run.start as written.
func spliceVerified(data, out []byte, setup string, want supervisorLines) error {
	var before, after any
	if err := yaml.Unmarshal(data, &before); err != nil {
		return fmt.Errorf("zerops.yaml: %w", err)
	}
	if err := yaml.Unmarshal(out, &after); err != nil {
		return fmt.Errorf("zerops.yaml after adding the supervisor lines: %w", err)
	}
	root, _ := before.(map[string]any)
	list, _ := root["zerops"].([]any)
	for _, e := range list {
		entry, _ := e.(map[string]any)
		if entry == nil || fmt.Sprint(entry["setup"]) != setup {
			continue
		}
		run, _ := entry["run"].(map[string]any)
		if run == nil {
			run = map[string]any{}
			entry["run"] = run
		}
		prepare, _ := run["prepareCommands"].([]any)
		placed := false
		for i, item := range prepare {
			if s, ok := item.(string); ok && strings.Contains(s, "/"+DevsupAsset) {
				prepare[i], placed = want.prepare, true
			}
		}
		if !placed {
			prepare = append(prepare, want.prepare)
		}
		run["prepareCommands"] = prepare
		if want.start != "" {
			run["start"] = want.start
		}
	}
	if !reflect.DeepEqual(before, after) {
		return errors.New("zcp cannot add the supervisor lines to this zerops.yaml without changing what else it says")
	}
	return nil
}
