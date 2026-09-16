//go:build e2e

// Tests for: e2e — the auth backbone's A1/A2 against a live Gitea + broker.
//
// Runs INSIDE a Mate's `zcp` container: the three variables the app writes
// (GITEA_URL, MATE_BROKER_URL, GITEA_TOKEN) are read out of the container's
// live env store, the broker hands the pair a repository, the branch is
// pushed and a pull request opened (A1), and the group's recipe is proposed
// to {org}/group (A2).
//
// Unlike newHarness this builds the server with runtime.Detect() and a real
// mounter — an empty runtime.Info makes every Mate-shaped reconcile a no-op.
//
// Run: /var/www/e2e-test -test.v -test.run TestE2E_GiteaBackboneLive

package e2e_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

func TestE2E_GiteaBackboneLive(t *testing.T) {
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set — skipping")
	}
	client, err := platform.NewZeropsClient(token, defaultAPIHost())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}
	authInfo.Region = "prg1"
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}

	rt := runtime.Detect()
	t.Logf("runtime: inContainer=%v service=%q project=%q gitHostKnown=%v giteaURL=%q",
		rt.InContainer, rt.ServiceName, rt.ProjectID, rt.GitHostKnown, rt.GiteaURL)

	// What the reconciles will read, by key only — never a value.
	wiring := ops.ReadGiteaWiring(func(key string) string {
		if v := liveEnvValue(t, key); v != "" {
			return v
		}
		return os.Getenv(key)
	})
	t.Logf("gitea wiring: ready=%v missing=%v giteaURL=%q brokerURL=%q tokenLen=%d",
		wiring.Ready(), wiring.MissingKeys(), wiring.GiteaURL, wiring.BrokerURL, len(wiring.Token))

	var mounter ops.Mounter
	var sshDeployer ops.SSHDeployer
	if rt.InContainer {
		mounter = platform.NewSystemMounter()
		sshDeployer = platform.NewSystemSSHDeployer()
	}
	srv := server.New(context.Background(), client, authInfo, store,
		platform.NewLogFetcher(), sshDeployer, mounter, rt, nil)
	s := newSession(t, srv)

	suffix := randomSuffix()[:4]
	devHostname := "rm" + suffix + "dev"
	stageHostname := "rm" + suffix + "stage"

	if os.Getenv("ZCP_E2E_KEEP") == "" {
		t.Cleanup(func() {
			cctx, ccancel := context.WithTimeout(context.Background(), 180*time.Second)
			defer ccancel()
			s.callTool("zerops_workflow", map[string]any{"action": "reset"})
			cleanupServices(cctx, client, authInfo.ProjectID, devHostname, stageHostname)
		})
	}

	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "intent": "the auth backbone's first live Mate",
	})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
		"intent": "the auth backbone's first live Mate",
	})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []any{map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"stageHostname": stageHostname,
				"type":          "nodejs@22",
				"bootstrapMode": "standard",
			},
		}},
	})

	importYAML := "services:\n" +
		"  - hostname: " + devHostname + "\n" +
		"    type: nodejs@22\n" +
		"    startWithoutCode: true\n" +
		"    minContainers: 1\n" +
		"    maxContainers: 1\n" +
		"    enableSubdomainAccess: true\n" +
		"  - hostname: " + stageHostname + "\n" +
		"    type: nodejs@22\n" +
		"    minContainers: 1\n" +
		"    maxContainers: 1\n" +
		"    enableSubdomainAccess: true\n"
	t.Logf("import:\n%s", s.mustCallSuccess("zerops_import", map[string]any{"content": importYAML}))
	// The import answers FINISHED while the containers are still CREATING, so
	// the pair is polled on the platform rather than on the import's word.
	waitForLiveStatus(t, client, authInfo.ProjectID, devHostname, "RUNNING", "ACTIVE")
	waitForLiveStatus(t, client, authInfo.ProjectID, stageHostname, "RUNNING", "ACTIVE", "READY_TO_DEPLOY")
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})

	// The provision CHECK reads the platform too — a refused check leaves the
	// step where it was, so the pass is retried rather than failed.
	var provText string
	for attempt := 1; attempt <= 8; attempt++ {
		provText = s.mustCallSuccess("zerops_workflow", map[string]any{
			"action": "complete", "step": "provision",
			"attestation": "The pair is up; the dev half is running without code.",
		})
		if strings.Contains(provText, `"current":{"name":"close"`) {
			break
		}
		t.Logf("provision check not passed on attempt %d; retrying", attempt)
		time.Sleep(20 * time.Second)
	}
	t.Logf("PROVISION COMPLETE (A1 no longer runs here):\n%s", provText)

	closeText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete", "step": "close",
		"attestation": "Bootstrap closed for the backbone's live run.",
	})
	t.Logf("CLOSE COMPLETE:\n%s", closeText)

	// A1's branch must DESCEND from the repository's protected `main`. The
	// broker's `main` is born with an initial commit and zcp git-initialises
	// the pair with a history of its own; a branch off that history shares no
	// commit with `main`, and Gitea then refuses both `merge` and `squash` on
	// the pull request.
	branch := devSSH(t, devHostname, "cd /var/www && git rev-parse --abbrev-ref HEAD")
	t.Logf("BRANCH: %q", branch)
	if !strings.HasPrefix(branch, "mate/") {
		t.Errorf("the pair works on %q, want the Mate's own mate/{bot} branch", branch)
	}
	if out, err := devSSHErr(devHostname, "cd /var/www && git merge-base --is-ancestor origin/main HEAD"); err != nil {
		t.Errorf("the pair's branch does not descend from origin/main (%v): %s\n%s",
			err, out, devSSH(t, devHostname, "cd /var/www && git log --oneline --all | head -20"))
	}

	// A3: the workflow that ships this repository to the group's stage has to
	// be IN the tree A1 wired, and it carries no credential of any kind.
	workflowPath := "/var/www/" + devHostname + "/.gitea/workflows/zerops.yml"
	workflowBody, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Errorf("A1 must leave the workflow at %s: %v", workflowPath, err)
	} else {
		t.Logf("WORKFLOW:\n%s", workflowBody)
		for _, want := range []string{"zeropsio/gitea-mate/actions/deploy@v1", "environment: stage", "service: " + devHostname} {
			if !strings.Contains(string(workflowBody), want) {
				t.Errorf("the workflow is missing %q", want)
			}
		}
		for _, forbidden := range []string{"secrets.", "ZEROPS_TOKEN", "zcli"} {
			if strings.Contains(string(workflowBody), forbidden) {
				t.Errorf("the workflow must carry no %q", forbidden)
			}
		}
	}

	statusText := s.mustCallSuccess("zerops_workflow", map[string]any{"action": "status"})
	t.Logf("STATUS:\n%s", statusText)

	// A2.
	recipeResult := s.callTool("zerops_workflow", map[string]any{"action": "group-recipe"})
	t.Logf("GROUP-RECIPE (isError=%v):\n%s", recipeResult.IsError, getE2ETextContent(t, recipeResult))

	// A group recipe names the setup block that builds each runtime, and a
	// pair that has never deployed has none recorded. A develop session
	// stamps it on the first deploy; here the pair's zerops.yaml is written
	// on the mounted dev container and the setup recorded the product's own
	// way, so A2 is exercised on a pair whose recipe can actually build.
	yaml := "zerops:\n  - setup: " + devHostname + "\n    build:\n      base: nodejs@22\n" +
		"      buildCommands:\n        - npm i --omit=dev\n      deployFiles: ./\n" +
		"    run:\n      base: nodejs@22\n      start: node index.js\n"
	if err := os.WriteFile("/var/www/"+devHostname+"/zerops.yaml", []byte(yaml), 0o644); err != nil {
		t.Logf("could not write the pair's zerops.yaml on the mount: %v", err)
	}
	t.Logf("SET-DEFAULT-SETUP:\n%s", getE2ETextContent(t, s.callTool("zerops_workflow", map[string]any{
		"action": "set-default-setup", "targetService": devHostname, "setup": devHostname,
	})))
	withSetup := s.callTool("zerops_workflow", map[string]any{"action": "group-recipe"})
	t.Logf("GROUP-RECIPE WITH A SETUP (isError=%v):\n%s", withSetup.IsError, getE2ETextContent(t, withSetup))

	// A second group-recipe pass: idempotence, and a second chance for A1 if
	// the first one was too early.
	again := s.callTool("zerops_workflow", map[string]any{"action": "group-recipe"})
	t.Logf("GROUP-RECIPE AGAIN (isError=%v):\n%s", again.IsError, getE2ETextContent(t, again))

	// The Mate's own code, on the Mate's own branch: A1 wires the remote but
	// pushes nothing (the pair has no commits at bootstrap), so the branch
	// the report names does not exist until the Mate deploys. Write an app on
	// the mounted dev container, commit it there, and let zcp deliver it the
	// way a configured pair delivers — git-push.
	app := "require('http').createServer((_, res) => res.end('ZPROBE-REALMATE-ONE\\n')).listen(3000)\n"
	if err := os.WriteFile("/var/www/"+devHostname+"/index.js", []byte(app), 0o644); err != nil {
		t.Fatalf("write the app on the mount: %v", err)
	}
	commit := exec.Command("ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR", devHostname,
		"cd /var/www && git add -A && git -c user.email=mate@example.invalid -c user.name=mate commit -m 'the first page'")
	if out, err := commit.CombinedOutput(); err != nil {
		t.Logf("commit on %s: %v\n%s", devHostname, err, out)
	} else {
		t.Logf("commit on %s:\n%s", devHostname, out)
	}
	push := s.callTool("zerops_deploy", map[string]any{
		"targetService": devHostname, "strategy": "git-push", "setup": devHostname,
	})
	pushText := getE2ETextContent(t, push)
	t.Logf("GIT-PUSH DEPLOY (isError=%v):\n%s", push.IsError, pushText)
	// The push is what puts the Mate's branch on the remote, so it is the
	// first moment the pull request that lands it CAN exist — and the Mate is
	// done pushing, so nothing else would open it.
	if !push.IsError && !strings.Contains(pushText, `"pullRequest"`) {
		t.Errorf("a git-push to the account's Gitea must open (or find) its pull request:\n%s", pushText)
	}

	// And it is idempotent: a second push finds the same request rather than
	// piling up duplicates.
	again2 := s.callTool("zerops_deploy", map[string]any{
		"targetService": devHostname, "strategy": "git-push", "setup": devHostname,
	})
	t.Logf("GIT-PUSH AGAIN (isError=%v):\n%s", again2.IsError, getE2ETextContent(t, again2))

	afterPush := s.callTool("zerops_workflow", map[string]any{"action": "group-recipe"})
	t.Logf("GROUP-RECIPE AFTER THE PUSH (isError=%v):\n%s", afterPush.IsError, getE2ETextContent(t, afterPush))
}

// devSSH runs one command on a mounted dev container and returns its trimmed
// output, failing the test when the command does.
func devSSH(t *testing.T, hostname, command string) string {
	t.Helper()
	out, err := devSSHErr(hostname, command)
	if err != nil {
		t.Fatalf("ssh %s %q: %v\n%s", hostname, command, err, out)
	}
	return out
}

// devSSHErr is devSSH without the fatal: the error is the answer for a probe
// whose failure is the thing being asserted.
func devSSHErr(hostname, command string) (string, error) {
	cmd := exec.Command("ssh", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR", hostname, command)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// liveEnvValue reads one key out of the container's live env store, the way
// the reconciles do. Returns "" when the store is unreadable.
func liveEnvValue(t *testing.T, key string) string {
	t.Helper()
	lines, err := mate.LoadLiveEnv(mate.LiveEnvStorePath)
	if err != nil {
		t.Logf("live env store unreadable: %v", err)
		return ""
	}
	for _, line := range lines {
		if len(line) > len(key)+1 && line[:len(key)] == key && line[len(key)] == '=' {
			return line[len(key)+1:]
		}
	}
	return ""
}

// waitForLiveStatus polls the platform until the service reports one of the
// given statuses. The import's process status is FINISHED well before the
// container is, and the provision check reads the container.
func waitForLiveStatus(t *testing.T, client platform.Client, projectID, hostname string, want ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Minute)
	last := ""
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		services, err := client.ListServices(ctx, projectID)
		cancel()
		if err == nil {
			for _, svc := range services {
				if svc.Name != hostname {
					continue
				}
				if svc.Status != last {
					last = svc.Status
					t.Logf("  %s: %s", hostname, svc.Status)
				}
				for _, w := range want {
					if svc.Status == w {
						return
					}
				}
			}
		}
		time.Sleep(10 * time.Second)
	}
	t.Fatalf("%s never reached %v (last %q)", hostname, want, last)
}
