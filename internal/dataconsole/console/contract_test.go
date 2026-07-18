package console

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

func TestDataConsoleContract_ServiceResponseShape_DriftGuard(t *testing.T) {
	t.Parallel()
	got := marshalContractJSON(t, servicesContractFixture())
	want, err := os.ReadFile("testdata/services_contract.json")
	if err != nil {
		t.Fatalf("read service contract fixture: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("service contract fixture drifted.\nGot:\n%s\nWant:\n%s", got, want)
	}
}

func TestDataConsoleContract_WebUIContractJS_DriftGuard(t *testing.T) {
	t.Parallel()
	got := []byte(generatedContractJS(t))
	want, err := os.ReadFile("webui/dist/contract.js")
	if err != nil {
		t.Fatalf("read webui contract.js: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("webui/dist/contract.js drifted from Go action IDs.\nGot:\n%s\nWant:\n%s", got, want)
	}
}

func TestDataConsoleContract_SPAActionReferences_AreGoOwned(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("webui/dist/*.js")
	if err != nil {
		t.Fatalf("glob webui dist js: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("webui/dist/*.js is empty; SPA action drift guard has no sources")
	}
	owned := map[string]bool{}
	for _, id := range provider.AllActionIDs() {
		owned[string(id)] = true
	}

	found := false
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		refs := actionReferences(src)
		if len(refs) > 0 {
			found = true
		}
		for _, ref := range refs {
			if !owned[ref] {
				t.Fatalf("%s references ACTION.%s, which is not a Go-owned action id (%v)", file, ref, provider.AllActionIDs())
			}
		}
	}
	if !found {
		t.Fatal("webui/dist/*.js does not reference ACTION.*; affordances are not using the generated action contract")
	}
}

func servicesContractFixture() servicesContract {
	return servicesContract{
		Project: ProjectRef{ID: "p-contract", Name: "Contract Project"},
		Services: []ServiceView{{
			Hostname: "db",
			Type:     "postgresql:single@18",
			Family:   provider.FamilyTabular,
			Support:  provider.SupportFull,
			Actions:  provider.ServiceActions(provider.FamilyTabular, provider.SupportFull, true),
			Status:   "ACTIVE",
		}},
		AllowWrites: true,
	}
}

type servicesContract struct {
	Project     ProjectRef    `json:"project"`
	Services    []ServiceView `json:"services"`
	AllowWrites bool          `json:"allowWrites"`
}

func marshalContractJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal contract: %v", err)
	}
	return append(out, '\n')
}

func generatedContractJS(t *testing.T) string {
	t.Helper()
	payload := struct {
		ActionIDs []provider.ActionID `json:"actionIDs"`
	}{
		ActionIDs: provider.AllActionIDs(),
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal webui contract: %v", err)
	}
	return "\"use strict\";\nwindow.DataConsoleContract = Object.freeze(" + string(b) + ");\n"
}

func actionReferences(src []byte) []string {
	re := regexp.MustCompile(`\bACTION\.([A-Za-z][A-Za-z0-9_]*)`)
	seen := map[string]bool{}
	for _, m := range re.FindAllSubmatch(src, -1) {
		seen[string(m[1])] = true
	}
	out := make([]string, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}
