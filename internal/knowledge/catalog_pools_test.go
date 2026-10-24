// Tests for: Swarm pools and Zerops Box in the schema-derived catalog
// (internal/knowledge/catalog_view.go, versions_format.go). Spec:
// spec-workflows.md §8 PS-1 (they are neither app runtimes nor managed deps).
package knowledge

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

// poolsCatalog returns a catalog holding one app runtime (nodejs, both OS
// prefixes), the two pool types (bare + versioned spellings, as the live
// schema lists them), Zerops Box, and a managed base.
func poolsCatalog() *schema.Schemas {
	return &schema.Schemas{
		ImportYml: &schema.ImportYmlSchema{ServiceTypes: []string{
			"alpine/nodejs@22", "ubuntu/nodejs@22",
			"swarm", "swarm@1", "swarm-vm", "swarm-vm@1",
			"ubuntu/zbox@1",
			"postgresql:single@18",
		}},
		ZeropsYml: &schema.ZeropsYmlSchema{
			BuildBases: []string{"nodejs@22"},
			RunBases:   []string{"nodejs@22"},
		},
	}
}

func TestCatalogView_PoolsAndSandboxes_OwnGroup(t *testing.T) {
	t.Parallel()

	const (
		wantHeader = "Pools and sandboxes (infrastructure an app uses, not app runtimes; no alpine/ and ubuntu/ pair — write each type exactly as listed):"
		wantPool   = "- swarm@1 | swarm-vm@1 — Swarm pool: up to 10 containers its orchestrator hands out (leases, exec, snapshots)"
		wantBox    = "- ubuntu/zbox@1 — Zerops Box: hosts sandboxes (beta)"
	)
	renderers := map[string]func(*schema.Schemas) string{
		"FormatStackList":     FormatStackList,
		"FormatServiceStacks": FormatServiceStacks,
	}
	for name, render := range renderers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := render(poolsCatalog())
			for _, want := range []string{wantHeader, wantPool, wantBox} {
				if strings.Count(out, want) != 1 {
					t.Errorf("want exactly one %q in:\n%s", want, out)
				}
			}
			lines := strings.Split(out, "\n")
			header := -1
			for i, l := range lines {
				if l == wantHeader {
					header = i
				}
			}
			if header < 0 || header+2 >= len(lines) || lines[header+1] != wantPool || lines[header+2] != wantBox {
				t.Errorf("group must be the header followed by the pool line then the Box line:\n%s", out)
			}
		})
	}
}

// TestCatalogView_RuntimeBases_ExcludePools pins that neither pools nor Box are
// listed among the app runtime bases, that the OS legend therefore carries no
// "zbox" exception, and that the version check still knows both types.
func TestCatalogView_RuntimeBases_ExcludePools(t *testing.T) {
	t.Parallel()

	const (
		wantRuntimeList   = "Runtime: nodejs@22\n"
		wantRuntimeBriefs = "Runtime: nodejs@22 [B]\n"
		wantLegend        = "Identifiers: runtime = `<os>/<tech>@<ver>` (e.g. ubuntu/nodejs@22, alpine/nodejs@22) — every runtime below exists as alpine/ and ubuntu/. Managed = `<tech>:single@<ver>` or `<tech>:ha@<ver>`. A bare `<tech>@<ver>` is legacy (import resolves it to ubuntu, zerops.yaml to alpine) — always write the prefix."
	)
	tests := []struct {
		name        string
		render      func(*schema.Schemas) string
		wantRuntime string
	}{
		{"FormatStackList", FormatStackList, wantRuntimeList},
		{"FormatServiceStacks", FormatServiceStacks, wantRuntimeBriefs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := tt.render(poolsCatalog())
			if !strings.Contains(out, tt.wantRuntime) {
				t.Errorf("want runtime line %q (nodejs only) in:\n%s", tt.wantRuntime, out)
			}
			if !strings.Contains(out, wantLegend) {
				t.Errorf("want OS legend without an exception clause in:\n%s", out)
			}
			for line := range strings.SplitSeq(out, "\n") {
				if !strings.HasPrefix(line, "Runtime:") {
					continue
				}
				for _, bad := range []string{"swarm", "zbox"} {
					if strings.Contains(line, bad) {
						t.Errorf("runtime line %q lists pool/sandbox type %q", line, bad)
					}
				}
			}
		})
	}

	check := FormatVersionCheck("", []string{"swarm@1", "swarm-vm@1", "ubuntu/zbox@1"}, poolsCatalog())
	for _, want := range []string{"- ✓ `swarm@1`", "- ✓ `swarm-vm@1`", "- ✓ `zbox@1`"} {
		if !strings.Contains(check, want) {
			t.Errorf("version check lost %q:\n%s", want, check)
		}
	}
}

// TestThemes_Services_PoolSection_FactsPresent pins that services.md carries a
// "Pools and Box" section for an app that needs one, naming the verified
// platform facts (spec-workflows.md §8 PS, Dev pools platform facts). The
// literals are the facts as live-verified, not recomputed from the file.
func TestThemes_Services_PoolSection_FactsPresent(t *testing.T) {
	t.Parallel()

	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/services")
	if err != nil {
		t.Fatalf("Get services: %v", err)
	}
	section, ok := doc.H2Sections()["Pools and Box"]
	if !ok {
		t.Fatal("services.md missing H2 section 'Pools and Box'")
	}
	for _, fact := range []string{
		"swarm@1", "ubuntu/zbox@1",
		"minContainers", "maxContainers", "verticalAutoscaling",
		"zcli push", "run.base",
		"orch.", "http://orch.<pool>.zerops", "${<pool>_API_TOKEN}", "_API_TOKEN", "401",
		"/container/acquire", "leaseTtlSeconds", "X-Swarm-Lease", "release", "reset=true",
		"/container/{id}/exec", "NDJSON",
		"clean", "run.prepareCommands",
		"node-id-<n>.runtime.<pool>.zerops", "sshIsolation",
		"subdomain", "beta", "capacity",
		// Pool and Box credentials are wired by reference, never copied.
		"ADMIN_TOKEN", "X-Swarm-Admin-Token",
		"http://<box>:8080", "ZBOX_WORKSPACE_API_KEY", "${<box>_ZBOX_WORKSPACE_API_KEY}", "ZBOX_WORKSPACE_VOLUME_KEY",
		// Untrusted code: what a container reads, and where such code belongs.
		"envIsolation: none", "envIsolation: service", "<host>_KEY", "--secret",
	} {
		if !strings.Contains(section, fact) {
			t.Errorf("'Pools and Box' section missing fact %q", fact)
		}
	}
}

// TestThemes_Services_DevPool_MultiPart_FactsPresent pins that the services
// theme's "A part's dev pool" subsection carries the loop for one branch
// across several parts (spec-workflows.md §8 DP-17, DP-22…DP-24, DP-26): the
// acquire of every part's pool in one call, the sibling wiring with its
// fallback to the part's stage, the 0.0.0.0 bind, each part's own servers in a
// shared declaration, commit-then-sync with its outcomes, the branch's release
// and the rewire with the stale-wiring mark.
func TestThemes_Services_DevPool_MultiPart_FactsPresent(t *testing.T) {
	t.Parallel()

	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/services")
	if err != nil {
		t.Fatalf("Get services: %v", err)
	}
	section, ok := doc.H2Sections()["Pools and Box"]
	if !ok {
		t.Fatal("services.md missing H2 section 'Pools and Box'")
	}
	const heading = "### A part's dev pool"
	_, sub, found := strings.Cut(section, heading)
	if !found {
		t.Fatalf("'Pools and Box' section missing %q", heading)
	}
	sub, _, _ = strings.Cut(sub, "\n### ")
	for _, fact := range []string{
		"action=acquire pools=[", "reused: true",
		`"siblingEnv"`, "node-id-<n>.runtime.<pool>.zerops", "leftToContainerEnv", "stage", "0.0.0.0",
		`"service"`, "unique across the whole file",
		"action=sync lease=", "up-to-date", "fast-forwarded", "has-own-commits", "local-changes-in-the-way",
		"on-another-branch", "unreachable", "unwatchedServers",
		"action=release branch=", "force=true",
		"rewired", `wiring: "stale"`,
	} {
		if !strings.Contains(sub, fact) {
			t.Errorf("%q subsection missing fact %q", heading, fact)
		}
	}
}
