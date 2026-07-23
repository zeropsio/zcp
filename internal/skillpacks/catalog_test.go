package skillpacks

import (
	"os"
	"regexp"
	"sort"
	"testing"
)

func TestLookup_KnownAndUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		id     string
		wantOK bool
	}{
		{name: "matt-pocock-skills is known", id: "matt-pocock-skills", wantOK: true},
		{name: "superpowers is known", id: "superpowers", wantOK: true},
		{name: "andrej-karpathy-skills is known", id: "andrej-karpathy-skills", wantOK: true},
		{name: "anthropic-skills is known", id: "anthropic-skills", wantOK: true},
		{name: "unknown id", id: "not-a-real-pack", wantOK: false},
		{name: "gstack is deliberately excluded (56MB whole-repo, not a skills collection)", id: "gstack", wantOK: false},
		{name: "empty id", id: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pack, ok := Lookup(tt.id)
			if ok != tt.wantOK {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tt.id, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if pack.ID != tt.id {
				t.Errorf("pack.ID = %q, want %q", pack.ID, tt.id)
			}
			if pack.Ref == "" {
				t.Errorf("pack.Ref is empty for %q", tt.id)
			}
			if pack.Title == "" || pack.Description == "" {
				t.Errorf("pack %q missing title/description", tt.id)
			}
		})
	}
}

func TestValidIDs_SortedAndMatchesCatalog(t *testing.T) {
	t.Parallel()

	ids := ValidIDs()
	if len(ids) != 4 {
		t.Fatalf("len(ValidIDs()) = %d, want 4", len(ids))
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("ValidIDs() = %v, want sorted", ids)
	}
	for _, id := range ids {
		if _, ok := Lookup(id); !ok {
			t.Errorf("ValidIDs() contains %q, but Lookup(%q) failed", id, id)
		}
	}
}

// TestCatalogIDs_MatchWelcomeExtensionAllowlist is the registry contract
// test: internal/content/templates/vscode-bootstrap-welcome.js keeps its
// own PACKS allowlist (a parallel JS enum, since the extension never reads
// Go source), and a source-layout change to one side without the other is
// exactly the class of drift that caused the Matt Pocock discovery failure
// this whole redesign responds to. This test only READS the JS file — it
// never modifies template content.
func TestCatalogIDs_MatchWelcomeExtensionAllowlist(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../content/templates/vscode-bootstrap-welcome.js")
	if err != nil {
		t.Fatalf("read welcome template: %v", err)
	}

	re := regexp.MustCompile(`(?s)const PACKS = \[(.*?)\];`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatal("could not find `const PACKS = [...]` in vscode-bootstrap-welcome.js — has the allowlist been renamed?")
	}
	idRe := regexp.MustCompile(`"([a-z0-9-]+)"`)
	idMatches := idRe.FindAllStringSubmatch(string(m[1]), -1)
	if len(idMatches) == 0 {
		t.Fatal("PACKS array parsed as empty")
	}
	jsIDs := make([]string, 0, len(idMatches))
	for _, im := range idMatches {
		jsIDs = append(jsIDs, im[1])
	}
	sort.Strings(jsIDs)

	goIDs := ValidIDs()
	if len(jsIDs) != len(goIDs) {
		t.Fatalf("welcome.js PACKS = %v, Go catalog = %v — length mismatch", jsIDs, goIDs)
	}
	for i := range goIDs {
		if jsIDs[i] != goIDs[i] {
			t.Errorf("welcome.js PACKS = %v, Go catalog ValidIDs() = %v — must match exactly", jsIDs, goIDs)
			break
		}
	}
}
