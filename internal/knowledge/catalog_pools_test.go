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
		wantBox    = "- ubuntu/zbox@1 — Zerops Box: microVM sandboxes that run code the app's users send, apart from the app (beta; projects in prg1 only)"
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
		// Box as the merged Box docs describe it (zerops-docs zbox/*.mdx;
		// spec-workflows.md §8 PS-10): one VM, its lifecycle and ports, the
		// API's basics and limits, what an imported Box lacks, the network
		// its sandboxes reach, the keys, and the live type and capacity facts.
		"zbox@1", "single Ubuntu VM", "not a container", "every resize", "Disk can only grow",
		"reload", "PARKED", "without their memory", "never restarts a Box",
		"8081", "enableSubdomainAccess", "http://<box>.zerops:8081", "`Host`",
		"/api/rest/", "GET /api/rest/ping", "Idempotency-Key", "waitSeconds", "networkMode", "/api/rest/swagger/openapi.yml", "workspaceFull",
		"60 s", "50 MiB", "10 min", "4 MiB",
		"starts empty", "no base and no domain", "`zerops_import` finishes the setup of each Box", "boxSetup", "zerops_box action=setup", "zbox setup",
		"rotates", "cannot be changed", "apply at once",
		"PROJECT_INTERNET", "the database included", "`INTERNET`", "`AIRGAPPED`", "allowedNetworkModes", "credentials proxy", "Dockerfiles you trust",
		"no Docker", "linux/amd64", "<host>.zerops",
		// Region: a Box exists only in prg1 (owner-stated, 2026-10-24); the
		// capacity message is how another region refuses it.
		"only in a project in region prg1", "not enough hardware capacity",
	} {
		if !strings.Contains(section, fact) {
			t.Errorf("'Pools and Box' section missing fact %q", fact)
		}
	}
}

// TestThemes_Services_DevPool_MultiPart_FactsPresent pins that the services
// theme's "A part's dev pool" subsection carries the loop for one branch
// across several parts (spec-workflows.md §8 DP-17, DP-22…DP-24, DP-26,
// DP-30, DP-31): the acquire of every part's pool in one call, with each
// lease's dev servers as the acquire left them and the status call for one
// still starting, the sibling wiring with its fallback to the part's stage,
// siblingPreviewEnv for a browser — the sibling's preview URL, resolved again
// at every join or leave and never hand-copied — the 0.0.0.0 bind, each
// part's own servers in a shared declaration, commit-then-sync with its
// outcomes and only runtime entries named to restart, since a declared server
// reloads by itself, the branch's release and the rewire with the
// stale-wiring mark.
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
		"`devServers`", "`starting` while it still compiles", "the preview server's `url`",
		`zerops_dev_server action=status hostname="<alias>" name="<server>"`,
		`"siblingEnv"`, "node-id-<n>.runtime.<pool>.zerops", "leftToContainerEnv", "stage", "0.0.0.0",
		"`siblingPreviewEnv`", `{"VITE_HQ_URL": "hqpool"}`, "resolved again whenever a lease of the branch joins or leaves", "never copy it by hand",
		`"service"`, "unique across the whole file",
		"action=sync lease=", "up-to-date", "fast-forwarded", "has-own-commits", "local-changes-in-the-way",
		"on-another-branch", "unreachable", "unwatchedServers",
		"only its runtime entries (`ctl run`)", "a declared server reloads by itself",
		"action=release branch=", "force=true",
		"rewired", `wiring: "stale"`,
	} {
		if !strings.Contains(sub, fact) {
			t.Errorf("%q subsection missing fact %q", heading, fact)
		}
	}
}

// TestThemes_Services_DevPool_WarmAndIdle_FactsPresent pins that the services
// theme's "A part's dev pool" subsection carries the warm container, idleness
// and releasing etiquette (spec-workflows.md §8 DP-21, DP-25, DP-27, DP-15;
// docs/spec-mate.md §0 rule 4): the import keeps minContainers at 0 because
// the platform's containers for the minimum cannot be reset; each pool keeps
// one warm container, kept or removed at release and topped up through the
// orchestrator's create, which status reports; status reads activity and
// unsaved work and marks a lease idle after 24 h without acting on it; and
// the agent asks the person before releasing a lease this conversation did
// not take, knowing what no release guard counts.
func TestThemes_Services_DevPool_WarmAndIdle_FactsPresent(t *testing.T) {
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
		// Import: the minimum stays 0; the warm container replaces it.
		"minContainers: 0", "cannot be reset", "warm container",
		// Warm: one per pool, kept or removed at release, topped up by the
		// orchestrator's create, reported by status.
		"one warm container", "kept as the pool's warm container", "removed", "outdated",
		"POST /container", "never through `minContainers`", "`warm`", "`target`", "`ready`", "`preparing`", "`last`",
		"containers-preparing",
		// Warm clone: the warm container already holds the repository at main,
		// so a branch acquire fetches only what changed; status names it.
		"already holds the pool's repository", "`warm.clone`",
		// Idleness: what status reads, when it marks a lease idle, what it names.
		"`activity`", "`lastChangeAt`", "`previewOpenConns`", "`unsaved`", "24 h", "`idle`", "`since`", "`next`",
		"in use", "never `force`", "a rename",
		// Releasing: nothing acts on idleness; ask before releasing another
		// conversation's lease; what no guard counts.
		"Nothing releases", "ask the person", "this conversation did not take",
		"Files git ignores", "stashes",
	} {
		if !strings.Contains(sub, fact) {
			t.Errorf("%q subsection missing fact %q", heading, fact)
		}
	}
}

// TestThemes_Services_DevPool_Park_FactsPresent pins that the services theme's
// "A part's dev pool" subsection carries park and unpark (spec-workflows.md §8
// DP-28): what a park keeps and ends, that nothing answers on a parked lease
// and which tools refuse it, what an unpark brings back, how a release treats
// a parked lease, and that idleness offers the park and never parks.
func TestThemes_Services_DevPool_Park_FactsPresent(t *testing.T) {
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
		// Park: what it keeps and ends, and that the slot stays taken.
		`zerops_lease action=park lease="<alias>"`, "`/var/www` and `$HOME`", "an Ubuntu base empties `/tmp`", "every process ends", "maximum",
		// Parked: nothing answers on it, what refuses it, the wiring.
		"`parked`", "`parkedAt`", "refuse it", "skips it",
		// Unpark: what comes back.
		`zerops_lease action=unpark lease="<alias>"`, "waits for SSH", "starts the supervisor when none runs",
		// Release: started first, stopped again when refused, forced as it stands.
		"starts it first", "stops it again", "as it stands, stopped",
		// Idleness offers it and never parks.
		"or park it", "Nothing parks a lease by itself",
	} {
		if !strings.Contains(sub, fact) {
			t.Errorf("%q subsection missing fact %q", heading, fact)
		}
	}
}

// TestThemes_Services_DevPool_WorkspaceDependencies_FactsPresent pins that the
// services theme's "A part's dev pool" subsection carries how a pool image
// bakes a package-manager workspace's dependencies (docs/spec-workflows.md §8
// DP-14; zerops-docs: the prepare runtime container holds only what
// build.addToRunPrepare copies, and its cache is keyed on those files): every
// member's manifest, the workspace definition and any patch directory copied
// with the lockfile before the install, keeping their paths; the whole
// workspace installed so the installed tree matches the lockfile, since a
// filtered one is read as stale and a later run or exec in a lease starts a
// full install; and a part's checks reaching past its own package closure.
func TestThemes_Services_DevPool_WorkspaceDependencies_FactsPresent(t *testing.T) {
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
	sub = strings.Join(strings.Fields(sub), " ")
	for _, fact := range []string{
		// The prepare container holds only what addToRunPrepare copies.
		"`run.prepareCommands` see only what `build.addToRunPrepare` copies",
		// A workspace install reads every member's manifest, the workspace
		// definition and any patch directory, beside the lockfile.
		"Workspace monorepo", "every member's manifest", "the workspace definition", "patch directory", "keeping their paths",
		// Concrete file names, as examples.
		"`pnpm-workspace.yaml`", "`patches/`", "`workspaces`", "`yarn.lock`", "`package-lock.json`",
		// Install the whole workspace; a filtered one reinstalls in the lease.
		"install the whole workspace", "matches the lockfile", "reads a filtered install as stale", "full install",
		"`./node_modules/.bin/<tool>`",
		// A part's checks can need more than its own closure.
		"a part's checks", "beyond its own package closure",
	} {
		if !strings.Contains(sub, fact) {
			t.Errorf("%q subsection missing fact %q", heading, fact)
		}
	}
}

// TestThemes_Services_DevPool_Memory_FactsPresent pins that the services
// theme's "A part's dev pool" subsection says a lease's dev servers and every
// check run in it share one container's RAM (measured live 2026-10-24: a dev
// server of 1.3–3.0 GB RSS plus a parallel typecheck overflowed a 4 GB pool
// container and the typecheck was killed with no message): size minRam for
// the dev server plus the heaviest check, run heavy checks one at a time and
// single-threaded, and read an ssh exit 255 with no output as the kill.
func TestThemes_Services_DevPool_Memory_FactsPresent(t *testing.T) {
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
	sub = strings.Join(strings.Fields(sub), " ")
	for _, fact := range []string{
		"share the container's RAM",
		"size `minRam` for the dev server plus the heaviest check",
		"one at a time", "single-threaded",
		"exit 255", "no output", "killed for memory",
	} {
		if !strings.Contains(sub, fact) {
			t.Errorf("%q subsection missing fact %q", heading, fact)
		}
	}
}

// TestThemes_Services_BoxFromTheApp_FactsPresent pins what an app needs to run
// code its users send in a Box (spec-workflows.md §8 PS-15), as verified
// against the workspace's OpenAPI document and live on 2026-10-24 (an app in
// the project calling http://<box>:8080 with the key wired by reference):
// where that code runs and why, the env wiring, the base digest a create
// needs, the create, exec and terminate request and answer shapes, one
// sandbox per run and its measured cost, a full workspace, and which limits
// apply where.
func TestThemes_Services_BoxFromTheApp_FactsPresent(t *testing.T) {
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
	const heading = "### Running code the app's users send"
	_, sub, found := strings.Cut(section, heading)
	if !found {
		t.Fatalf("'Pools and Box' section missing %q", heading)
	}
	sub, _, _ = strings.Cut(sub, "\n### ")
	for _, fact := range []string{
		// Where the code runs, and why not in the app's container.
		"never inside the app's own container", "share the container's kernel", "its own microVM",
		"a dependency of the runtime", "never a runtime target",
		// Env wiring by reference: the key never passes through the agent.
		"BOX_API_URL: http://<box>:8080", "BOX_API_KEY: ${<box>_ZBOX_WORKSPACE_API_KEY}", "run.envVariables",
		// The base digest, looked up, never the name.
		"GET /api/rest/base-list", "`default`", "`READY`", "`digest`", "404 baseNotFound",
		// Create.
		"POST /api/rest/sandbox", `"networkMode": "AIRGAPPED"`, `"integrations": []`, `"waitSeconds": 30`,
		"Idempotency-Key", "`400`", "`sandboxes[0].id`", "`RUNNING`", "memoryBytes",
		// Exec.
		"POST /api/rest/sandbox/{id}/exec", `"command"`, `"timeoutSeconds"`, `"workingDir"`,
		"no shell", "no stdin", "`exitCode`", "`stdout`", "`stderr`", "`timedOut`", "`stdoutTruncated`", "`exitCode` is null",
		"127", "4 MiB", "`/`",
		// Terminate.
		"DELETE /api/rest/sandbox/{id}", "`TERMINATED`", "not enforced yet", "finally",
		// One sandbox per run, and what it costs.
		"one sandbox per run", "about 0.3 s", "409 workspaceFull", "six",
		// Limits by path.
		"60 s", "public API URL", "70 s",
		"idempotencyKeyConflict", "6.18.53",
	} {
		if !strings.Contains(sub, fact) {
			t.Errorf("%q subsection missing fact %q", heading, fact)
		}
	}
}
