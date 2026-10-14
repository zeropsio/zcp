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
