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
			body := `{"link":"` + link + `","mate":{"projectId":"p1","appId":null,"repos":[],"changes":[],"chief":null}}`
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

// witness: internal/hq/mate_state.go | if state.ProjectID == "" || (projectID != "" && state.ProjectID != projectID) { → if state.ProjectID == "" || (projectID != "" && state.ProjectID != projectID) || len(required) != 5 {
func TestMateState_UnknownKey_Decodes(t *testing.T) {
	t.Parallel()
	for _, extra := range []string{`"future":true`, `"future":{"one":1}`} {
		t.Run(extra, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "state.json")
			body := `{"link":"linked","mate":{"projectId":"p1","appId":null,"repos":[],"changes":[],"chief":null,` + extra + `}}`
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
