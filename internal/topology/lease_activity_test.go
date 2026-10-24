package topology

import (
	"encoding/json"
	"testing"
)

// TestLeaseActivity_JSON_AbsentFactIsNeverZero pins the wire shape of a held
// lease's activity and unsaved work (docs/spec-workflows.md §8 DP-21): a fact
// zcp did not read is absent, never a zero a client would read as "nothing
// happened"; a fact it read as zero is present.
func TestLeaseActivity_JSON_AbsentFactIsNeverZero(t *testing.T) {
	t.Parallel()
	zero := 0
	for _, tt := range []struct {
		name string
		v    any
		want string
	}{
		{
			name: "unread activity keeps acquiredAt and says why",
			v:    LeaseActivity{AcquiredAt: "2026-10-23T12:00:00Z", Note: "zcp could not reach the container over ssh"},
			want: `{"acquiredAt":"2026-10-23T12:00:00Z","note":"zcp could not reach the container over ssh"}`,
		},
		{
			name: "read checkout, preview not observed",
			v:    LeaseActivity{LastChangeAt: "2026-10-23T13:00:00Z", AcquiredAt: "2026-10-23T12:00:00Z", ReadAt: "2026-10-23T14:00:00Z"},
			want: `{"lastChangeAt":"2026-10-23T13:00:00Z","acquiredAt":"2026-10-23T12:00:00Z","readAt":"2026-10-23T14:00:00Z"}`,
		},
		{
			name: "preview observed with no open connection",
			v:    LeaseActivity{PreviewLastConnAt: "2026-10-23T13:30:00Z", PreviewOpenConns: &zero},
			want: `{"previewLastConnAt":"2026-10-23T13:30:00Z","previewOpenConns":0}`,
		},
		{
			name: "clean checkout reads as zeros, not as absent",
			v:    LeaseUnsaved{},
			want: `{"fileCount":0,"unpushedCommits":0}`,
		},
		{
			name: "unsaved work names its files and counts",
			v:    LeaseUnsaved{Files: []string{" M src/App.tsx", "?? notes.txt"}, FileCount: 2, UnpushedCommits: 1},
			want: `{"files":[" M src/App.tsx","?? notes.txt"],"fileCount":2,"unpushedCommits":1}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tt.want {
				t.Errorf("json = %s, want %s", raw, tt.want)
			}
		})
	}
}
