package bundle

import (
	"slices"
	"strings"
	"testing"
)

const mainTier = `project:
  name: acme-stage
services:
  - hostname: db
    type: postgresql:single@18
    priority: 10
    verticalAutoscaling:
      minRam: 0.5
  - hostname: search
    type: meilisearch:single@1.44
    priority: 10
    verticalAutoscaling:
      cpuMode: SHARED
      minRam: 1
      maxRam: 48

  - hostname: mailpit
    type: alpine@3.20
    buildFromGit: https://github.com/zeropsio/recipe-mailpit
`

const composedTier = `project:
  name: acme-stage
services:
  - hostname: db
    type: postgresql:single@18
    priority: 10
    verticalAutoscaling:
      minRam: 4
  - hostname: search
    type: meilisearch:single@1.44
    priority: 10
    verticalAutoscaling:
      cpuMode: SHARED
      minRam: 2
      maxRam: 48
      minFreeRamGB: 0.5
  - hostname: mailpit
    type: alpine@3.20
    buildFromGit: https://github.com/zeropsio/recipe-mailpit
    verticalAutoscaling:
      minRam: 0.25
`

// TestSpliceHostScaling: a scaling proposal changes one host's
// verticalAutoscaling block in a tier file and nothing else — not the other
// hosts' blocks (db differs too here), not the file's layout.
func TestSpliceHostScaling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		main        string
		host        string
		wantChanged bool
		wantLines   []string // lines the result must carry
	}{
		{"replaces the host's block", mainTier, "search", true, []string{"      minRam: 2", "      minFreeRamGB: 0.5", "      minRam: 0.5"}},
		{"adds a block the host lacks", mainTier, "mailpit", true, []string{"    verticalAutoscaling:", "      minRam: 0.25"}},
		{"a host the tier does not name", mainTier, "cache", false, nil},
		{"a block already as composed", composedTier, "search", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := SpliceHostScaling(tt.main, composedTier, tt.host)
			if err != nil {
				t.Fatalf("SpliceHostScaling: %v", err)
			}
			if changed := got != tt.main; changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v:\n%s", changed, tt.wantChanged, got)
			}
			for _, line := range tt.wantLines {
				if !slices.Contains(strings.Split(got, "\n"), line) {
					t.Errorf("result lacks %q:\n%s", line, got)
				}
			}
			// Outside the host's entry, every line is main's, in main's order.
			if tt.wantChanged {
				outside := func(body string) []string {
					lines := strings.Split(body, "\n")
					start, end := hostEntryRange(lines, tt.host)
					return append(slices.Clone(lines[:start]), lines[end:]...)
				}
				if !slices.Equal(outside(got), outside(tt.main)) {
					t.Errorf("lines outside %s's entry changed:\n%s", tt.host, got)
				}
			}
		})
	}
}

// TestHostScalingChanges names what a proposal would change, key by key, old
// and new, for the steer line.
func TestHostScalingChanges(t *testing.T) {
	t.Parallel()
	got := HostScalingChanges(mainTier, composedTier, "search")
	want := []ScalingChange{{Key: "minFreeRamGB", To: "0.5"}, {Key: "minRam", From: "1", To: "2"}}
	if !slices.Equal(got, want) {
		t.Errorf("HostScalingChanges = %+v, want %+v", got, want)
	}
	if got := HostScalingChanges(composedTier, composedTier, "search"); len(got) != 0 {
		t.Errorf("an unchanged block reports %+v", got)
	}
	// A composed entry with no block is a scale nobody read: no change.
	if got := HostScalingChanges(mainTier, "services:\n  - hostname: search\n    type: meilisearch:single@1.44\n", "search"); len(got) != 0 {
		t.Errorf("a composed entry with no block reports %+v", got)
	}
}

// TestSpliceHostScaling_RefusesWhatItCannotSpliceSafely: a tier file
// someone edited by hand can hold shapes the splice does not read — a flow
// mapping, a comment after the header, a comment at column 0 inside the
// entry or the block. Splicing past one could write a second
// verticalAutoscaling key; the splice refuses, naming why, and never does.
func TestSpliceHostScaling_RefusesWhatItCannotSpliceSafely(t *testing.T) {
	t.Parallel()
	entry := func(lines ...string) string {
		return "services:\n  - hostname: search\n    type: meilisearch:single@1.44\n" + strings.Join(lines, "\n") + "\n  - hostname: db\n    type: postgresql:single@18\n"
	}
	tests := []struct {
		name string
		main string
		want string
	}{
		{"a flow mapping", entry("    verticalAutoscaling: {minRam: 1}"), "flow"},
		{"a comment after the header", entry("    verticalAutoscaling: # sized by hand", "      minRam: 1"), "comment"},
		{"a column-0 comment inside the entry", entry("# kept small on purpose", "    verticalAutoscaling:", "      minRam: 1"), "comment"},
		{"a column-0 comment inside the block", entry("    verticalAutoscaling:", "      minRam: 1", "# no more than this", "      maxRam: 48"), "comment"},
		{"two blocks already", entry("    verticalAutoscaling:", "      minRam: 1", "    verticalAutoscaling:", "      minRam: 2"), "more than one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := SpliceHostScaling(tt.main, composedTier, "search")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("SpliceHostScaling = %v, want a refusal naming %q; result:\n%s", err, tt.want, got)
			}
			if got != tt.main {
				t.Errorf("a refused splice changed the file:\n%s", got)
			}
		})
	}
}

// TestHostScalingChanges_ComparesValues: 2, 2.0 and "2" are one value, and a
// comment is not one; only a value that differs is a change.
func TestHostScalingChanges_ComparesValues(t *testing.T) {
	t.Parallel()
	block := func(lines ...string) string {
		return "services:\n  - hostname: search\n    type: meilisearch:single@1.44\n    verticalAutoscaling:\n" + strings.Join(lines, "\n") + "\n"
	}
	tests := []struct {
		name string
		main string
		want []ScalingChange
	}{
		{"2.0 is 2", block("      cpuMode: SHARED", "      minRam: 2.0", "      maxRam: 48", "      minFreeRamGB: 0.50"), nil},
		{`"2" is 2`, block(`      cpuMode: "SHARED"`, `      minRam: "2"`, "      maxRam: 48", "      minFreeRamGB: 0.5"), nil},
		{"a trailing comment is no value", block("      cpuMode: SHARED", "      minRam: 2 # raised", "      maxRam: 48", "      minFreeRamGB: 0.5"), nil},
		{"a value that differs", block("      cpuMode: SHARED", "      minRam: 1.0", "      maxRam: 48", "      minFreeRamGB: 0.5"), []ScalingChange{{Key: "minRam", From: "1", To: "2"}}},
	}
	composed := block("      cpuMode: SHARED", "      minRam: 2", "      maxRam: 48", "      minFreeRamGB: 0.5")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := HostScalingChanges(tt.main, composed, "search"); !slices.Equal(got, tt.want) {
				t.Errorf("HostScalingChanges = %+v, want %+v", got, tt.want)
			}
		})
	}
}
