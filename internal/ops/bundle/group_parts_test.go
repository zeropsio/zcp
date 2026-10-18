package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// handEditedStage is a Stage tier on main that people have edited: a renamed
// runtime, a comment of their own, a variable they added. Only the targeted
// service may change in it.
const handEditedStage = `# The Stage tier, edited by the group.

project:
  name: mono stage
  vault:
    APP_URL: https://app-${zeropsSubdomainHost}-3000.prg1.zerops.app
    FEATURE_FLAG: "on" # added by Eva

services:
  - hostname: db
    type: postgresql@17
    priority: 10

  # the storefront; renamed from app by hand
  - hostname: shop
    type: nodejs@22
    priority: 1
    zeropsSetup: web
    buildFromGit: https://hq.example.com/git/app-1/appdev
    minContainers: 1

  - hostname: mailpit
    type: alpine@3.21
    priority: 5
`

// composedWorker is the composer's tier holding the worker part.
const composedWorker = `services:
  - hostname: db
    type: postgresql@17
    priority: 10

  - hostname: worker
    type: nodejs@22
    priority: 1
    zeropsSetup: worker
    buildFromGit: https://hq.example.com/git/app-1/appdev
    minContainers: 1
    vault:
      QUEUE: jobs
`

// Adding or removing a part proposes exactly "add/remove service X" to
// existing tiers and leaves every other line, hand edits included,
// unchanged.
func TestTierService_AddOrRemove_OnlyThatService(t *testing.T) {
	t.Parallel()
	withWorker := strings.Replace(handEditedStage, `    minContainers: 1

  - hostname: mailpit`, `    minContainers: 1

  - hostname: worker
    type: nodejs@22
    priority: 1
    zeropsSetup: worker
    buildFromGit: https://hq.example.com/git/app-1/appdev
    minContainers: 1
    vault:
      QUEUE: jobs

  - hostname: mailpit`, 1)
	withoutShop := strings.Replace(handEditedStage, `  # the storefront; renamed from app by hand
  - hostname: shop
    type: nodejs@22
    priority: 1
    zeropsSetup: web
    buildFromGit: https://hq.example.com/git/app-1/appdev
    minContainers: 1

`, "", 1)

	got, err := AddTierService(handEditedStage, composedWorker, "worker", "shop")
	if err != nil {
		t.Fatalf("AddTierService: %v", err)
	}
	if got != withWorker {
		t.Errorf("adding worker after shop:\n--- got\n%s\n--- want\n%s", got, withWorker)
	}
	if again, _ := AddTierService(got, composedWorker, "worker", "shop"); again != got {
		t.Errorf("adding worker a second time changed the tier:\n%s", again)
	}

	got, err = RemoveTierService(handEditedStage, "shop")
	if err != nil {
		t.Fatalf("RemoveTierService: %v", err)
	}
	if got != withoutShop {
		t.Errorf("removing shop:\n--- got\n%s\n--- want\n%s", got, withoutShop)
	}
	if back, err := RemoveTierService(withWorker, "worker"); err != nil || back != handEditedStage {
		t.Errorf("removing the added worker = %v\n%s\nwant main as it was", err, back)
	}
}

// A service added with no sibling of its repository to follow goes last in
// the tier's services; a tier the splice cannot read is refused, never
// written.
func TestTierService_NoAnchorOrUnreadable(t *testing.T) {
	t.Parallel()
	got, err := AddTierService(handEditedStage, composedWorker, "worker", "")
	if err != nil {
		t.Fatalf("AddTierService: %v", err)
	}
	if !strings.HasSuffix(got, "    priority: 5\n\n  - hostname: worker\n    type: nodejs@22\n    priority: 1\n    zeropsSetup: worker\n    buildFromGit: https://hq.example.com/git/app-1/appdev\n    minContainers: 1\n    vault:\n      QUEUE: jobs\n") {
		t.Errorf("worker is not appended last:\n%s", got)
	}
	flow := "project:\n  name: x\nservices: [{hostname: shop, type: nodejs@22}]\n"
	if _, err := AddTierService(flow, composedWorker, "worker", ""); err == nil {
		t.Error("a flow-style services list was written to")
	}
	if _, err := RemoveTierService(flow, "shop"); err == nil {
		t.Error("a service the splice cannot find was reported removed")
	}
}

// A tier the composer wrote — preprocessor secrets, comments and all — is
// read back by the hostname, setup and canonical repository of every
// runtime it builds from a repository with a setup; a managed service never.
func TestHeldRuntimes_ReadsTheComposersOwnTiers(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile(filepath.Join("testdata", "group_recipe", "medusa-small-production.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, held := range HeldRuntimes(string(body)) {
		got[held.Hostname] = held.SetupName + " " + held.RepoURL
	}
	want := map[string]string{
		"medusa":    "medusaprod https://hq.example.com/git/app-1/medusadev",
		"nextstore": "nextstoreprod https://hq.example.com/git/app-1/nextstoredev",
	}
	for host, w := range want {
		if got[host] != w {
			t.Errorf("held %s = %q, want %q (all: %v)", host, got[host], w, got)
		}
	}
	if _, managed := got["db"]; managed {
		t.Errorf("a managed service was read as held: %v", got)
	}
}

// composedCache is the composer's tier holding a cache the Mate added.
const composedCache = `services:
  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10

  - hostname: db
    type: postgresql@17
    priority: 10
`

// A managed service the Mate's project gained goes into a tier on main where
// the platform creates it: among the services of its priority, before the
// first that ranks below it, every other line — a person's edits included —
// as main has it.
func TestTierService_AddInRank_ManagedFirst(t *testing.T) {
	t.Parallel()
	cacheEntry := "  - hostname: cache\n    type: valkey@7.2\n    mode: NON_HA\n    priority: 10\n"
	runtimesOnly := "project:\n  name: mono stage\n\nservices:\n  # the storefront\n  - hostname: shop\n    type: nodejs@22\n    priority: 1\n"
	managedOnly := "services:\n  - hostname: db\n    type: postgresql@17\n    priority: 10\n"
	tests := []struct {
		name, main, want string
	}{
		{
			name: "a cache added beside a database goes after it, before the runtimes",
			main: handEditedStage,
			want: strings.Replace(handEditedStage, "    priority: 10\n\n  # the storefront", "    priority: 10\n\n"+cacheEntry+"\n  # the storefront", 1),
		},
		{
			name: "a cache added to a tier of runtimes alone goes first, above a person's comment",
			main: runtimesOnly,
			want: strings.Replace(runtimesOnly, "services:\n", "services:\n"+cacheEntry+"\n", 1),
		},
		{
			name: "a cache added to a tier whose every service ranks with it goes last",
			main: managedOnly,
			want: managedOnly + "\n" + cacheEntry,
		},
		{
			name: "a tier that already lists the service is left as it is",
			main: managedOnly + "\n" + cacheEntry,
			want: managedOnly + "\n" + cacheEntry,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := AddTierServiceInRank(tt.main, composedCache, "cache")
			if err != nil {
				t.Fatalf("AddTierServiceInRank: %v", err)
			}
			if got != tt.want {
				t.Errorf("--- got\n%s\n--- want\n%s", got, tt.want)
			}
			if tt.main == tt.want {
				return
			}
			if back, err := RemoveTierService(got, "cache"); err != nil || back != tt.main {
				t.Errorf("taking the cache out again = %v\n%s\nwant main as it was", err, back)
			}
		})
	}

	flow := "project:\n  name: x\nservices: [{hostname: shop, type: nodejs@22}]\n"
	if _, err := AddTierServiceInRank(flow, composedCache, "cache"); err == nil {
		t.Error("a flow-style services list was written to")
	}
	if _, err := AddTierServiceInRank(managedOnly, composedCache, "search"); err == nil {
		t.Error("a service the composed tier does not name was added")
	}
}
