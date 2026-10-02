package bundle

import (
	"regexp"
	"sort"
	"strings"
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
// mainBody as it is.
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

// HostScalingChanges names, key by key in key order, what replacing main's
// block for host with the composed one changes.
func HostScalingChanges(mainBody, composedBody, host string) []ScalingChange {
	before := scalingValues(hostScalingBlock(strings.Split(mainBody, "\n"), host))
	after := scalingValues(hostScalingBlock(strings.Split(composedBody, "\n"), host))
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
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) != "" && indentOf(lines[j]) <= dash {
				return i, j
			}
		}
		return i, len(lines)
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
			if strings.TrimSpace(lines[j]) == "" {
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

// scalingValues reads a block's `key: value` lines.
func scalingValues(block []string) map[string]string {
	values := map[string]string{}
	for _, line := range block[min(1, len(block)):] {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && key != "" {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values
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
