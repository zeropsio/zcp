package hq

import (
	"encoding/json"
	"testing"
)

// witness: internal/hq/changes.go | json:"checksGivenUp,omitempty" → json:"-"
func TestChange_CGUChange_DecodesChecksHandoff(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, body string
		givenUp    bool
	}{
		{"given up", `{"checksGivenUp":{"reason":"no-agent"}}`, true},
		{"absent", `{}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var change Change
			if err := json.Unmarshal([]byte(tt.body), &change); err != nil {
				t.Fatalf("decode change: %v", err)
			}
			if got := change.ChecksGivenUp != nil; got != tt.givenUp {
				t.Fatalf("checks given up = %v, want %v", got, tt.givenUp)
			}
		})
	}
}

// witness: internal/hq/self.go | json:"checksGivenUp,omitempty" → json:"-"
func TestMateState_CGUState_DecodesChangesWithAndWithoutHandoff(t *testing.T) {
	t.Parallel()
	type expectedChange struct {
		number  int
		givenUp bool
	}
	for _, tt := range []struct {
		name, changes string
		want          []expectedChange
	}{
		{"mixed", `[{"number":7,"checksGivenUp":{"reason":"no-agent"}},{"number":6}]`, []expectedChange{{7, true}, {6, false}}},
		{"ordinary", `[{"number":6}]`, []expectedChange{{6, false}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := `{"link":"linked","mate":{"rev":1,"projectId":"p1","appId":null,"repos":[],"chief":null,"changes":` + tt.changes + `}}`
			doc, err := decodeMateState([]byte(body), "p1")
			if err != nil {
				t.Fatalf("decode Mate state: %v", err)
			}
			if doc.Mate == nil || len(doc.Mate.Changes) != len(tt.want) {
				t.Fatalf("changes = %+v, want %d", doc.Mate, len(tt.want))
			}
			for i, want := range tt.want {
				change := doc.Mate.Changes[i]
				if got := change.ChecksGivenUp != nil; change.Number != want.number || got != want.givenUp {
					t.Errorf("change = #%d, checks given up %v; want #%d, %v", change.Number, got, want.number, want.givenUp)
				}
			}
		})
	}
}
