// Tests for: internal/tools/workflow_checks_readme_container_ops.go
package tools

import (
	"strings"
	"testing"
)

func TestReadmeContainerOps_SSHFSInGotcha_EmitsInfo(t *testing.T) {
	t.Parallel()
	kb := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- **SSHFS mount requires polling** — The zcli vpn up mount uses sshfs under
  the hood; file watchers need usePolling=true because sshfs does not
  surface inotify events. Without polling the dev loop feels broken.

- **L7 balancer returns 502 without 0.0.0.0 bind** — NestJS defaults to
  127.0.0.1 which is unreachable from the Zerops balancer. Rejects with
  502.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkReadmeContainerOps(kb, "apidev")
	if len(checks) == 0 {
		t.Fatalf("expected at least one info check emitted")
	}
	// Must be info, not fail.
	for _, c := range checks {
		if c.Status == statusFail {
			t.Fatalf("container-ops check must be info-only, got fail: %s", c.Detail)
		}
	}
	// Must name the sshfs token and suggest CLAUDE.md.
	joined := ""
	for _, c := range checks {
		joined += c.Detail + " "
	}
	if !strings.Contains(strings.ToLower(joined), "sshfs") {
		t.Errorf("detail must name the matched token; got: %s", joined)
	}
	if !strings.Contains(joined, "CLAUDE.md") {
		t.Errorf("detail must suggest CLAUDE.md; got: %s", joined)
	}
}

func TestReadmeContainerOps_FuserInGotcha_EmitsInfo(t *testing.T) {
	t.Parallel()
	kb := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- **Port already in use on restart** — The dev container's fuser -k 3000/tcp
  pattern kills the prior dev-server process before re-starting; without it
  the new process exits with EADDRINUSE.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkReadmeContainerOps(kb, "apidev")
	if len(checks) == 0 {
		t.Fatalf("expected an info check for fuser token")
	}
	for _, c := range checks {
		if c.Status == statusFail {
			t.Fatalf("container-ops check must be info-only, got fail")
		}
	}
}

func TestReadmeContainerOps_PlatformOnlyGotcha_NoEmit(t *testing.T) {
	t.Parallel()
	kb := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- **L7 balancer returns 502 without 0.0.0.0 bind** — NestJS default bind of
  127.0.0.1 is unreachable from the Zerops balancer; sockets get rejected.

- **CORS credentials can't use wildcard origin** — The browser rejects the
  Access-Control-Allow-Origin: * + credentials:true combination at preflight.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkReadmeContainerOps(kb, "apidev")
	// At most a single pass check should appear.
	for _, c := range checks {
		if c.Status == "info" {
			t.Fatalf("no container-ops token present but info emitted: %s", c.Detail)
		}
	}
}

func TestReadmeContainerOps_InfoStatusDoesNotBlockFinalize(t *testing.T) {
	t.Parallel()
	kb := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- **SSHFS mount polls** — uses chokidar polling to work around the sshfs
  inotify limitation; without polling the dev loop feels broken.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	checks := checkReadmeContainerOps(kb, "apidev")
	// checksAllPassed returns true unless any check has statusFail —
	// info must not flip it.
	if !checksAllPassed(checks) {
		t.Fatalf("info-status container-ops findings must not fail the finalize gate")
	}
}

func TestReadmeContainerOps_EmptyKB_NoEmit(t *testing.T) {
	t.Parallel()
	if checks := checkReadmeContainerOps("", "apidev"); len(checks) != 0 {
		t.Fatalf("empty kb → no-op; got %+v", checks)
	}
}
