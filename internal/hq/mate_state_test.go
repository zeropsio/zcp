//go:build unix

package hq

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// witness: internal/hq/mate_state.go | doc.Link, doc.Reason = MateLinkNotLinked, "no_server" → doc.Link, doc.Reason = MateLinkLinked, ""
func TestMateState_UnheldLock_ReportsNoServer(t *testing.T) {
	t.Parallel()
	for _, link := range []string{"linked", "not-linked"} {
		t.Run(link, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "state.json")
			body := `{"link":"` + link + `","mate":{"rev":1,"projectId":"p1","appId":null,"repos":[],"changes":[],"chief":null}}`
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			doc, err := NewFileMateStateSource(path, "p1").Read()
			if err != nil || doc.Link != "not-linked" || doc.Reason != "no_server" {
				t.Fatalf("standing = %q/%q, error %v; want not-linked/no_server", doc.Link, doc.Reason, err)
			}
		})
	}
}

// witness: internal/hq/mate_state.go | if state.ProjectID == "" || (projectID != "" && state.ProjectID != projectID) { → if state.ProjectID == "" || (projectID != "" && state.ProjectID != projectID) || len(required) != 6 {
func TestMateState_UnknownKey_Decodes(t *testing.T) {
	t.Parallel()
	for _, extra := range []string{`"future":true`, `"future":{"one":1}`} {
		t.Run(extra, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "state.json")
			body := `{"link":"linked","mate":{"rev":1,"projectId":"p1","appId":null,"repos":[],"changes":[],"chief":null,` + extra + `}}`
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			doc, err := NewFileMateStateSource(path, "p1").Read()
			if err != nil || doc.Mate == nil || doc.Mate.ProjectID != "p1" {
				t.Fatalf("state = %+v, error %v for %s", doc.Mate, err, strings.TrimSpace(extra))
			}
		})
	}
}

// witness: internal/hq/self.go | return c.State == ChangeMerged || c.CarriedBy() > 0 → return c.State == ChangeMerged
func TestClient_Self_Changes_DecodeLandedWith(t *testing.T) {
	t.Parallel()
	const state = `{"link":"linked","mate":{"rev":2,"projectId":"p-mate","appId":"a1","repos":[],"chief":null,"changes":[` +
		`{"repo":"appdev","number":38,"state":"merged","branch":"signup"},` +
		`{"repo":"appdev","number":37,"state":"closed","landedWith":38,"branch":"login"},` +
		`{"repo":"appdev","number":36,"state":"closed","branch":"dropped"},` +
		`{"repo":"appdev","number":35,"state":"open","landedWith":38,"branch":"open"}]}}`
	doc, err := decodeMateState([]byte(state), "p-mate")
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct {
		carriedBy int
		onMain    bool
	}{{0, true}, {38, true}, {0, false}, {0, false}} {
		c := doc.Mate.Changes[i]
		if c.CarriedBy() != want.carriedBy || c.OnMain() != want.onMain {
			t.Errorf("#%d: carried by %d, on main %v; want %d, %v", c.Number, c.CarriedBy(), c.OnMain(), want.carriedBy, want.onMain)
		}
	}
}
