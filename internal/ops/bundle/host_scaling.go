package bundle

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// A Mate that changes a service's scale after its group's recipe is on main
// proposes that change on its own: a pull request that rewrites ONE host's
// verticalAutoscaling block in each tier file and leaves every other line of
// the file as main has it. These read and splice the block as text, by the
// composer's own layout — `- hostname:` items under services, the block a
// field of the item — so the diff a person reviews is the block alone.

// ScalingChange is one key of a host's vertical scale a proposal changes:
// From is main's value ("" when main has none), To the proposal's.
type ScalingChange struct {
	Key  string
	From string
	To   string
}

// SpliceHostScaling returns mainBody with host's verticalAutoscaling block
// replaced by composedBody's (inserted when main's entry has none). A host
// either body does not name, or a composed entry with no block, leaves
// mainBody as it is. A shape the splice does not read safely in main's
// entry — a flow mapping, a comment on or inside the block or at a column
// inside the entry, a second block — is refused with the reason, and main is
// never written with a second verticalAutoscaling key.
func SpliceHostScaling(mainBody, composedBody, host string) (string, error) {
	mainLines := strings.Split(mainBody, "\n")
	start, end := hostEntryRange(mainLines, host)
	if start < 0 {
		return mainBody, nil
	}
	block := hostScalingBlock(strings.Split(composedBody, "\n"), host)
	if len(block) == 0 {
		return mainBody, nil
	}
	if err := spliceSafe(mainLines, start, end, host); err != nil {
		return mainBody, err
	}
	fieldIndent := indentOf(mainLines[start]) + 2
	block = reindent(block, fieldIndent-indentOf(block[0]))

	from, to := scalingBlockRange(mainLines, start, end)
	if from < 0 {
		// No block yet: it goes after the entry's last line.
		from = start
		for i := start; i < end; i++ {
			if strings.TrimSpace(mainLines[i]) != "" {
				from = i
			}
		}
		from++
		to = from
	}
	out := make([]string, 0, len(mainLines)+len(block))
	out = append(out, mainLines[:from]...)
	out = append(out, block...)
	out = append(out, mainLines[to:]...)
	return strings.Join(out, "\n"), nil
}

// spliceSafe refuses an entry whose shape the splice does not read: the
// lines it would keep or replace must be exactly the block layout the
// composer writes.
func spliceSafe(lines []string, start, end int, host string) error {
	field := indentOf(lines[start]) + 2
	headers := 0
	inBlock := false
	lastContent := start
	for i := start + 1; i < end; i++ {
		if !isComment(lines[i]) && strings.TrimSpace(lines[i]) != "" {
			lastContent = i
		}
	}
	for i := start + 1; i <= lastContent; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			continue
		case isComment(line):
			if inBlock {
				return fmt.Errorf("%s's verticalAutoscaling holds a comment (line %d), which a rewrite of the block would drop — edit it by hand", host, i+1)
			}
			if indentOf(line) < field {
				return fmt.Errorf("%s's entry holds a comment at column %d (line %d), which the splice cannot place — edit it by hand", host, indentOf(line), i+1)
			}
			continue
		}
		if indentOf(line) == field {
			inBlock = false
			if key, rest, ok := strings.Cut(trimmed, ":"); ok && key == "verticalAutoscaling" {
				headers++
				rest = strings.TrimSpace(rest)
				switch {
				case strings.HasPrefix(rest, "{"):
					return fmt.Errorf("%s's verticalAutoscaling is a flow mapping (line %d), which the splice does not rewrite — edit it by hand", host, i+1)
				case strings.HasPrefix(rest, "#"):
					return fmt.Errorf("%s's verticalAutoscaling carries a comment on its header (line %d), which a rewrite would drop — edit it by hand", host, i+1)
				case rest != "":
					return fmt.Errorf("%s's verticalAutoscaling has an inline value (line %d) — edit it by hand", host, i+1)
				}
				inBlock = true
			}
			continue
		}
		if inBlock && strings.Contains(line, " #") {
			return fmt.Errorf("%s's verticalAutoscaling holds a comment (line %d), which a rewrite of the block would drop — edit it by hand", host, i+1)
		}
	}
	if headers > 1 {
		return fmt.Errorf("%s's entry has more than one verticalAutoscaling — edit it by hand", host)
	}
	return nil
}

func isComment(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "#") }

// HostScalingChanges names, key by key in key order, what replacing main's
// block for host with the composed one changes.
func HostScalingChanges(mainBody, composedBody, host string) []ScalingChange {
	composed := hostScalingBlock(strings.Split(composedBody, "\n"), host)
	if len(composed) == 0 {
		// No composed block is a scale nobody read, not one to remove.
		return nil
	}
	before := scalingValues(hostScalingBlock(strings.Split(mainBody, "\n"), host))
	after := scalingValues(composed)
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	var changes []ScalingChange
	for k := range keys {
		if before[k] != after[k] {
			changes = append(changes, ScalingChange{Key: k, From: before[k], To: after[k]})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Key < changes[j].Key })
	return changes
}

// TierNamesHost reports whether a tier file has an entry for host.
func TierNamesHost(body, host string) bool {
	start, _ := hostEntryRange(strings.Split(body, "\n"), host)
	return start >= 0
}

var hostnameLine = regexp.MustCompile(`^(\s*)- hostname:\s*(\S+)\s*$`)

// hostEntryRange is the [start, end) lines of host's services[] item; -1
// when the body names no such host.
func hostEntryRange(lines []string, host string) (int, int) {
	for i, line := range lines {
		m := hostnameLine.FindStringSubmatch(line)
		if m == nil || strings.Trim(m[2], `"'`) != host {
			continue
		}
		dash := len(m[1])
		// A comment never ends an entry: a hand-written one at column 0
		// inside it would otherwise cut the entry short (spliceSafe refuses
		// it). Comment lines right before the next item stay outside.
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) != "" && !isComment(lines[j]) && indentOf(lines[j]) <= dash {
				end = j
				break
			}
		}
		for end > i+1 && (isComment(lines[end-1]) && indentOf(lines[end-1]) <= dash || strings.TrimSpace(lines[end-1]) == "") {
			end--
		}
		return i, end
	}
	return -1, -1
}

// scalingBlockRange is the [from, to) lines of the verticalAutoscaling field
// inside the entry [start, end), trailing blank lines left out; -1 when the
// entry has none.
func scalingBlockRange(lines []string, start, end int) (int, int) {
	field := indentOf(lines[start]) + 2
	for i := start + 1; i < end; i++ {
		if indentOf(lines[i]) != field || strings.TrimSpace(lines[i]) != "verticalAutoscaling:" {
			continue
		}
		to := i + 1
		for j := i + 1; j < end; j++ {
			if strings.TrimSpace(lines[j]) == "" || isComment(lines[j]) {
				continue
			}
			if indentOf(lines[j]) <= field {
				break
			}
			to = j + 1
		}
		return i, to
	}
	return -1, -1
}

// hostScalingBlock is host's verticalAutoscaling block, header included.
func hostScalingBlock(lines []string, host string) []string {
	start, end := hostEntryRange(lines, host)
	if start < 0 {
		return nil
	}
	from, to := scalingBlockRange(lines, start, end)
	if from < 0 {
		return nil
	}
	return append([]string(nil), lines[from:to]...)
}

// scalingValues reads a block as YAML, each value normalized so 2, 2.0 and
// "2" are one value and a comment is none.
func scalingValues(block []string) map[string]string {
	values := map[string]string{}
	if len(block) == 0 {
		return values
	}
	var doc map[string]map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(reindent(block, -indentOf(block[0])), "\n")), &doc); err != nil {
		return values
	}
	for key, value := range doc["verticalAutoscaling"] {
		values[key] = normalizedScalingValue(value)
	}
	return values
}

// normalizedScalingValue is a value's canonical text: a number however it
// was written, a string as itself.
func normalizedScalingValue(v any) string {
	switch n := v.(type) {
	case int:
		return strconv.Itoa(n)
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
		return n
	}
	return fmt.Sprint(v)
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// reindent shifts every non-blank line by delta spaces.
func reindent(lines []string, delta int) []string {
	if delta == 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		switch {
		case strings.TrimSpace(line) == "":
			out[i] = line
		case delta > 0:
			out[i] = strings.Repeat(" ", delta) + line
		default:
			out[i] = line[min(-delta, indentOf(line)):]
		}
	}
	return out
}
