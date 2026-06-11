//go:build e2e

// FULL-CHAIN live replay of the prod.txt T2 scenario against the fixes
// (spec-git-delivery-target §5/§6.1): a real push through the credential
// helper triggers the repo's REAL GitHub Actions workflow; the build
// watch follows the integration build to terminal; the (legacy, no -g)
// workflow's artifact replaces the container WITHOUT .git — the exact
// state that spiraled in the session — and the reconstruction rebuilds
// it from the remote, leaving HEAD == remote HEAD and a clean tree.
//
// MUTATING + BILLABLE-ish (one CI run, one container replace) — opt-in:
//
//	export ZCP_E2E_GIT_DELIVERY_SERVICE=weather
//	export ZCP_E2E_GIT_DELIVERY_REMOTE=https://github.com/krls2020/xy3
//	export ZCP_E2E_GIT_DELIVERY_FULLCHAIN=1
//	go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_GitDeliveryFullChain -timeout 900s
package e2e_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestE2E_GitDeliveryFullChain(t *testing.T) {
	hostname := os.Getenv("ZCP_E2E_GIT_DELIVERY_SERVICE")
	remote := os.Getenv("ZCP_E2E_GIT_DELIVERY_REMOTE")
	if hostname == "" || remote == "" || os.Getenv("ZCP_E2E_GIT_DELIVERY_FULLCHAIN") == "" {
		t.Skip("full-chain replay is opt-in (ZCP_E2E_GIT_DELIVERY_FULLCHAIN=1) — mutates the repo + replaces the container")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Minute)
	defer cancel()
	ssh := platform.NewSystemSSHDeployer()

	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set")
	}
	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}
	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	projectID := authInfo.ProjectID
	svc, err := ops.LookupService(ctx, client, projectID, hostname)
	if err != nil {
		t.Fatalf("lookup %s: %v", hostname, err)
	}

	// 1. Trivial commit on the container + push through the credential
	//    helper (the real BuildGitPushCommand — the path agents run).
	marker := fmt.Sprintf("fullchain-%d", time.Now().Unix())
	commitCmd := fmt.Sprintf(
		`cd /var/www && echo %q >> .zcp-e2e-fullchain && git add -A && git -c user.email='agent@zerops.io' -c user.name='Zerops Agent' commit -q -m %q`,
		marker, "e2e full-chain "+marker,
	)
	if out, err := ssh.ExecSSH(ctx, hostname, commitCmd); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	pushedAt := time.Now().UTC()
	if out, err := ssh.ExecSSH(ctx, hostname, ops.BuildGitPushCommand("/var/www", remote, "main")); err != nil {
		t.Fatalf("push through credential helper: %v\n%s", err, out)
	}
	t.Logf("pushed %s at %s", marker, pushedAt.Format(time.RFC3339))

	// 2. Build watch: the repo's real Actions workflow (legacy template,
	//    no -g) runs zcli push against this service — the watch must
	//    DISCOVER and follow it to terminal.
	watch, err := ops.WatchIntegrationBuild(ctx, client, projectID, svc.ID, pushedAt, func(msg string, _, _ float64) {
		t.Logf("watch: %s", msg)
	})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if !watch.Observed {
		t.Fatalf("integration build did not appear within the discovery budget — check the repo's Actions runs")
	}
	if watch.TimedOut || watch.Event.Status != "ACTIVE" {
		t.Fatalf("integration build did not reach ACTIVE (status=%s timedOut=%v)", watch.Event.Status, watch.TimedOut)
	}
	t.Logf("integration build ACTIVE (appVersion %s)", watch.Event.ID)

	// 3. The legacy workflow ships no .git → the replaced container must
	//    be git-less (the prod.txt T2 reality, now an EXPECTED state).
	//    SSH needs a beat to come back after the replace.
	deadline := time.Now().Add(2 * time.Minute)
	var presence string
	for time.Now().Before(deadline) {
		out, sshErr := ssh.ExecSSH(ctx, hostname, "test -d /var/www/.git && echo present || echo absent")
		if sshErr == nil {
			presence = strings.TrimSpace(string(out))
			break
		}
		time.Sleep(5 * time.Second)
	}
	if !strings.Contains(presence, "absent") {
		// A -g-fixed workflow (future state) would keep .git — that is
		// the BETTER outcome; log + skip the reconstruction half.
		t.Logf(".git survived the CI build (presence=%q) — workflow already ships -g; reconstruction half not exercised", presence)
		return
	}
	t.Log("container replaced git-less (legacy no -g workflow) — exercising live reconstruction")

	// 4. Live reconstruction from the recorded remote.
	if out, err := ssh.ExecSSH(ctx, hostname, ops.BuildGitReconstructCommand("/var/www", remote)); err != nil {
		t.Fatalf("reconstruction: %v\n%s", err, out)
	}
	state, err := ssh.ExecSSH(ctx, hostname,
		`cd /var/www && echo "HEAD=$(git rev-parse HEAD 2>/dev/null)" && echo "DIRTY=$(git status --porcelain | head -3)" && git remote get-url origin`)
	if err != nil {
		t.Fatalf("post-reconstruction read: %v", err)
	}
	body := string(state)
	if !strings.Contains(body, "HEAD=") || strings.Contains(body, "HEAD=\n") {
		t.Errorf("reconstructed repo must have a resolvable HEAD: %s", body)
	}
	if !strings.Contains(body, remote) {
		t.Errorf("reconstructed origin must be the recorded remote: %s", body)
	}
	// The remote HEAD must match the local HEAD (the pushed commit).
	lsOut, err := ssh.ExecSSH(ctx, hostname, ops.BuildGitAuthedLsRemoteCommand(remote))
	if err != nil {
		t.Fatalf("authed ls-remote: %v", err)
	}
	// System ssh mixes the known-hosts warning into the captured output —
	// the SHA is the last non-empty line.
	remoteHead := lastNonEmptyLine(string(lsOut))
	if remoteHead == "" || !strings.Contains(body, "HEAD="+remoteHead) {
		t.Errorf("reconstructed HEAD must equal remote HEAD %q; state: %s", remoteHead, body)
	}
	t.Logf("reconstruction verified: HEAD == remote HEAD (%s)", remoteHead[:10])
}
