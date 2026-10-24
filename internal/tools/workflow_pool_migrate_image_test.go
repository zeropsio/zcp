package tools

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolMigrateImageReadMarker starts the image step's read of the commit.
const poolMigrateImageReadMarker = "'zcp-image 1'"

// poolMigrateSetupCommittedAt is when the fixture commit last changed its
// zerops.yaml: 08:00 UTC.
const poolMigrateSetupCommittedAt = "2026-10-24T10:00:00+02:00"

const poolMigrateDevsupLine = `sudo sh -c 'u=https://github.com/zeropsio/zcp/releases/download/v1.2.3/zcp-devsup-linux-amd64; f=/usr/local/bin/zcp-devsup; curl -fsSL "$u" -o "$f" && chmod 0755 "$f"'`

// poolMigrateReadyYAML is a zerops.yaml whose pool setup passes every image
// check for a pool of three containers and a previewPortBase of 4000.
func poolMigrateReadyYAML() string {
	return `zerops:
  - setup: appdev
    build:
      base: nodejs@22
      deployFiles: ./
    run:
      base: nodejs@22
      prepareCommands:
        - ` + poolMigrateDevsupLine + `
      start: zcp-devsup run
  - setup: apppool
    build:
      base: nodejs@22
      addToRunPrepare:
        - package.json
        - package-lock.json
    run:
      base: nodejs@22
      ports:
        - port: 4001
          httpSupport: true
        - port: 4002
          httpSupport: true
        - port: 4003
          httpSupport: true
      prepareCommands:
        - ` + poolMigrateDevsupLine + `
        - cp package.json package-lock.json /var/www/ && cd /var/www && npm ci
      start: zcp-devsup run
`
}

const poolMigrateReadyDeclaration = `{"version": 1, "previewPortBase": 4000, "servers": [{"name": "web", "port": 3000, "command": "npm run dev"}]}`

// poolMigrateImageReadFor is what the image step's read prints for a commit
// carrying config as zerops.yaml and the declaration ("" for none).
func poolMigrateImageReadFor(config, declaration string) string {
	var b strings.Builder
	b.WriteString("zcp-image 1\n")
	if config != "" {
		fmt.Fprintf(&b, "F %d zerops.yaml\n%s\n", len(config), config)
	}
	if declaration != "" {
		fmt.Fprintf(&b, "F %d .zcp/dev-servers.json\n%s\n", len(declaration), declaration)
	}
	if config != "" {
		b.WriteString("C " + poolMigrateSetupCommittedAt + "\n")
	}
	b.WriteString("zcp-image-end\n")
	return b.String()
}

// poolImageFixture is a pair whose pool exists, is settled and holds the dev
// service's env, so the flow is past the env step; mutate shapes it before
// the mock is built.
func poolImageFixture(t *testing.T, mutate func(*poolMigrateFixture)) poolMigrateFixture {
	t.Helper()
	f := poolEnvFixture()
	f.env["svc-pool"] = append(poolMigratePoolOwnEnv(),
		platform.ServiceEnvVar{ID: "p-url", Key: "APP_URL", Content: "${zeropsSubdomain}", Type: platform.ServiceEnvUser},
		platform.ServiceEnvVar{ID: "p-secret", Key: "STRIPE_SECRET", Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true},
	)
	if mutate != nil {
		mutate(&f)
	}
	f.build(t)
	if got := ops.SeedPoolDefaultRepo(f.stateDir, "apppool", f.meta.RemoteURL); got.State != ops.PoolDefaultRepoSeeded {
		t.Fatalf("seed: %+v", got)
	}
	return f
}

// poolImageRunning makes the fixture's pool run an app version created at
// created.
func poolImageRunning(created string) func(*poolMigrateFixture) {
	return func(f *poolMigrateFixture) {
		for i := range f.services {
			if f.services[i].Name == "apppool" {
				f.services[i].ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-pool", Created: created, Source: "CLI", Built: true}
			}
		}
	}
}

func poolImageBlock(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	image, ok := out["image"].(map[string]any)
	if !ok {
		t.Fatalf("no image block in %v", out)
	}
	return image
}

func poolImageChecks(image map[string]any, field string) []string {
	list, _ := image[field].([]any)
	checks := make([]string, 0, len(list))
	for _, item := range list {
		checks = append(checks, item.(map[string]any)["check"].(string))
	}
	slices.Sort(checks)
	return checks
}

func poolImageStepStates(out map[string]any) map[string]string {
	states := map[string]string{}
	steps, _ := out["steps"].([]any)
	for _, s := range steps {
		m := s.(map[string]any)
		states[m["step"].(string)] = m["state"].(string)
	}
	return states
}

func TestPoolMigrateImageStep_ReadySetup_NamesTheShaDeploy(t *testing.T) {
	t.Parallel()
	f := poolImageFixture(t, nil)
	out, mock := f.run(t)

	if out["status"] != "step" || out["step"] != "image" {
		t.Fatalf("status/step = %v/%v, want step/image", out["status"], out["step"])
	}
	next, _ := out["next"].(map[string]any)
	args, _ := next["args"].(map[string]any)
	want := map[string]any{"sourceService": "appdev", "targetService": "apppool", "setup": "apppool", "sha": poolMigrateHead}
	if next["tool"] != "zerops_deploy" || !reflect.DeepEqual(args, want) {
		t.Fatalf("next = %v, want zerops_deploy %v", next, want)
	}
	if why, _ := next["why"].(string); !strings.Contains(why, "1111111111") {
		t.Errorf("why = %q, want it to name the commit", why)
	}
	image := poolImageBlock(t, out)
	if image["commit"] != poolMigrateHead || image["setup"] != "apppool" || image["file"] != "zerops.yaml" ||
		image["previewPortBase"] != float64(4000) || image["maxContainers"] != float64(3) || image["done"] != false {
		t.Errorf("image = %v", image)
	}
	if _, has := image["missing"]; has {
		t.Errorf("a ready setup names nothing missing: %v", image["missing"])
	}
	if g, _ := out["guidance"].(string); !strings.Contains(g, "### Deploy a part's pool setup to its pool") {
		t.Error("guidance does not carry the image step's atom")
	}
	if states := poolImageStepStates(out); states["env"] != "done" || states["image"] != "current" || states["move"] != "pending" {
		t.Errorf("steps = %v", states)
	}
	cmds := f.ssh.recorded()
	if len(cmds) != 2 || cmds[1] != "appdev: "+ops.PoolMigrateImageCommand("/var/www", poolMigrateHead) {
		t.Fatalf("ssh commands = %v, want the census and one read of the commit", cmds)
	}
	for method, n := range mock.CallCounts {
		if n > 0 && !poolMigrateReadOnlyCalls[method] {
			t.Errorf("the image step called %s %d times", method, n)
		}
	}
}

func TestPoolMigrateImageStep_SetupNotReady_NamesNoDeploy(t *testing.T) {
	t.Parallel()
	f := poolImageFixture(t, func(f *poolMigrateFixture) {
		f.ssh.image = poolMigrateImageReadFor(strings.Replace(poolMigrateReadyYAML(), "        - cp package.json package-lock.json /var/www/ && cd /var/www && npm ci\n      start: zcp-devsup run", "        - cp package.json package-lock.json /var/www/ && cd /var/www && npm ci\n      start: npm run dev", 1), "")
		f.ssh.output = poolMigrateCensusFor("main", "origin", "main", "", "TS 1 .M N... 100644 100644 100644 aaaa bbbb zerops.yaml")
	})
	out, _ := f.run(t)

	if out["step"] != "image" {
		t.Fatalf("step = %v, want image", out["step"])
	}
	if _, has := out["next"]; has {
		t.Fatalf("a setup that is not ready names no deploy: %v", out["next"])
	}
	image := poolImageBlock(t, out)
	if got := poolImageChecks(image, "missing"); !reflect.DeepEqual(got, []string{ops.PoolMigrateImagePreviewBase, ops.PoolMigrateImageStart}) {
		t.Fatalf("missing = %v", got)
	}
	why, _ := out["nextUnavailable"].(string)
	for _, want := range []string{"1111111111", "image.missing", "zerops.yaml"} {
		if !strings.Contains(why, want) {
			t.Errorf("nextUnavailable %q does not name %q", why, want)
		}
	}
	if !reflect.DeepEqual(image["uncommitted"], []any{"zerops.yaml"}) {
		t.Errorf("uncommitted = %v", image["uncommitted"])
	}
}

// TestPoolMigrateImageStep_DependencyFindings_DeployStillNamed pins that the
// dependency rule speaks as findings beside the deploy, never holding it.
func TestPoolMigrateImageStep_DependencyFindings_DeployStillNamed(t *testing.T) {
	t.Parallel()
	f := poolImageFixture(t, func(f *poolMigrateFixture) {
		notBaked := strings.Replace(poolMigrateReadyYAML(), "        - cp package.json package-lock.json /var/www/ && cd /var/www && npm ci\n", "", 1)
		shipped := strings.Replace(notBaked, "      addToRunPrepare:\n        - package.json\n        - package-lock.json\n", "      buildCommands:\n        - npm ci\n      deployFiles: ./\n", 1)
		f.ssh.image = poolMigrateImageReadFor(shipped, poolMigrateReadyDeclaration)
	})
	out, _ := f.run(t)

	if next, _ := out["next"].(map[string]any); next["tool"] != "zerops_deploy" {
		t.Fatalf("next = %v, want the deploy named despite the findings", out["next"])
	}
	image := poolImageBlock(t, out)
	if got := poolImageChecks(image, "findings"); !reflect.DeepEqual(got, []string{ops.PoolMigrateImageDepsBaked, ops.PoolMigrateImageDepsLockfile}) {
		t.Fatalf("findings = %v", got)
	}
	findings, _ := image["findings"].([]any)
	var baked string
	for _, item := range findings {
		if m := item.(map[string]any); m["check"] == ops.PoolMigrateImageDepsBaked {
			baked, _ = m["message"].(string)
		}
	}
	if !strings.Contains(baked, "wiped by every reset") {
		t.Errorf("deps-baked = %q, want it to say what deployFiles ships is wiped by every reset", baked)
	}
}

func TestPoolMigrateImageStep_PoolRunsTheCommitsSetup_StepIsMove(t *testing.T) {
	t.Parallel()
	f := poolImageFixture(t, poolImageRunning("2026-10-24T09:00:00Z"))
	out, _ := f.run(t)

	if out["step"] != "move" {
		t.Fatalf("step = %v, want move once the pool runs an image of the commit's setup; image %v", out["step"], out["image"])
	}
	if _, has := out["next"]; has {
		t.Fatalf("the move step is not run by this zcp: %v", out["next"])
	}
	if why, _ := out["nextUnavailable"].(string); why == "" {
		t.Fatal("the move step must say why no next call is named")
	}
	image := poolImageBlock(t, out)
	deployed, _ := image["deployed"].(map[string]any)
	if image["done"] != true || deployed["current"] != true || deployed["configCommittedAt"] != poolMigrateSetupCommittedAt {
		t.Fatalf("image = %v", image)
	}
	if states := poolImageStepStates(out); states["image"] != "done" || states["move"] != "current" {
		t.Errorf("steps = %v", states)
	}
}

func TestPoolMigrateImageStep_PoolRunsAnOlderImage_NamesTheDeployAgain(t *testing.T) {
	t.Parallel()
	f := poolImageFixture(t, poolImageRunning("2026-10-24T07:00:00Z"))
	out, _ := f.run(t)

	if out["step"] != "image" {
		t.Fatalf("step = %v, want image: the pool runs an image older than the commit's setup", out["step"])
	}
	if next, _ := out["next"].(map[string]any); next["tool"] != "zerops_deploy" {
		t.Fatalf("next = %v, want the deploy named again", out["next"])
	}
	deployed, _ := poolImageBlock(t, out)["deployed"].(map[string]any)
	if deployed["current"] != false || deployed["created"] != "2026-10-24T07:00:00Z" {
		t.Fatalf("deployed = %v", deployed)
	}
}

func TestPoolMigrateImageStep_BuildInFlight_Waits(t *testing.T) {
	t.Parallel()
	f := poolImageFixture(t, func(f *poolMigrateFixture) {
		f.processes = []platform.Process{{
			ID: "proc-build", ActionName: "stack.build", Status: "RUNNING",
			ServiceStacks: []platform.ServiceStackRef{{ID: "svc-pool", Name: "apppool"}},
			Created:       "2026-10-24T09:00:00Z",
			AppVersion:    &platform.ProcessAppVersion{Status: platform.BuildStatusBuilding},
		}}
	})
	out, _ := f.run(t)

	if out["step"] != "image" {
		t.Fatalf("step = %v, want image while the pool's build runs", out["step"])
	}
	next, _ := out["next"].(map[string]any)
	args, _ := next["args"].(map[string]any)
	if next["tool"] != "zerops_process" || args["action"] != poolMigrateProcessWait || args["service"] != "apppool" {
		t.Fatalf("next = %v, want zerops_process action=wait service=apppool", next)
	}
	if cmds := f.ssh.recorded(); len(cmds) != 1 {
		t.Fatalf("ssh commands = %v, want only the census while the build runs", cmds)
	}
}

// TestPoolMigrateImageStep_CommitUnreadable_NamesNoDeploy pins that a read of
// the commit that fails keeps the step — the env step's answer included —
// and names no deploy, saying why with git's stderr.
func TestPoolMigrateImageStep_CommitUnreadable_NamesNoDeploy(t *testing.T) {
	t.Parallel()
	f := poolImageFixture(t, func(f *poolMigrateFixture) {
		f.ssh.imageErr = &platform.SSHExecError{Hostname: "appdev", Output: "fatal: loose object is corrupt", Err: fmt.Errorf("exit status 128")}
	})
	for _, action := range []string{"", "pool-env"} {
		f.input.Action = action
		out, _ := f.call(t, map[string]func(context.Context, poolMigrateDeps, WorkflowInput) (*mcp.CallToolResult, any, error){"": poolMigrateRun, "pool-env": poolMigrateEnvRun}[action])
		if out["status"] != "step" || out["step"] != "image" {
			t.Fatalf("action %q: status/step = %v/%v, want step/image", action, out["status"], out["step"])
		}
		if _, has := out["next"]; has {
			t.Fatalf("action %q: a commit not read names no deploy: %v", action, out["next"])
		}
		if why, _ := out["nextUnavailable"].(string); !strings.Contains(why, "fatal: loose object is corrupt") || !strings.Contains(why, "1111111111") {
			t.Errorf("action %q: nextUnavailable %q does not name git's stderr and the commit", action, why)
		}
	}
}

func TestPoolMigrateImageStep_ReadSideWritesNothing(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*poolMigrateFixture){
		"deploy named": nil,
		"done":         poolImageRunning("2026-10-24T09:00:00Z"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := poolImageFixture(t, mutate)
			before := snapshotTree(t, f.stateDir)
			out, mock := f.run(t)
			if out["status"] != "step" {
				t.Fatalf("status = %v", out["status"])
			}
			if after := snapshotTree(t, f.stateDir); !reflect.DeepEqual(before, after) {
				t.Fatalf("the image step changed the state dir:\nbefore %v\nafter  %v", before, after)
			}
			for method, n := range mock.CallCounts {
				if n > 0 && !poolMigrateReadOnlyCalls[method] {
					t.Errorf("the image step called %s %d times", method, n)
				}
			}
			want := []string{"appdev: " + ops.PoolMigrateCensusCommand("/var/www"), "appdev: " + ops.PoolMigrateImageCommand("/var/www", poolMigrateHead)}
			if cmds := f.ssh.recorded(); !reflect.DeepEqual(cmds, want) {
				t.Errorf("ssh commands = %v, want the census and the read of the commit", cmds)
			}
			if meta, err := workflow.FindServiceMeta(f.stateDir, "appdev"); err != nil || meta == nil || meta.Hostname != "appdev" {
				t.Fatalf("meta = %+v, %v", meta, err)
			}
		})
	}
}

// TestPoolMigrateImageStep_EnvAction_LandsOnTheDeploy pins that the env
// step's own answer, once the pool holds the dev's env, names the image
// step's deploy.
func TestPoolMigrateImageStep_EnvAction_LandsOnTheDeploy(t *testing.T) {
	t.Parallel()
	f := poolEnvFixture()
	f.input.Action = "pool-env"
	out, _ := f.runEnv(t)
	if out["action"] != "pool-env" || out["step"] != "image" {
		t.Fatalf("action/step = %v/%v", out["action"], out["step"])
	}
	if next, _ := out["next"].(map[string]any); next["tool"] != "zerops_deploy" {
		t.Fatalf("next = %v, want the image step's deploy", out["next"])
	}
}
