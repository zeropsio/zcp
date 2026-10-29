package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Probe-only diagnostics (ciprobe-run branch): never merged.

func TestZZProbe_HelperFDs(t *testing.T) {
	l := newRelayLab(t)
	url, h := l.repo(t, false, "org/app-1/appdev")
	checkout := l.checkout(t, url)
	log := filepath.Join(t.TempDir(), "fds")
	helper := `!f() { echo "helper $$ args $*" >> ` + log + `; ls -l /proc/$$/fd >> ` + log + ` 2>&1; }; f`
	for _, sh := range relayShells() {
		script := "exec 3</dev/null 4>/dev/null; git -c credential.helper= -c 'credential.helper=" + helper + "' show " + h.first + ":app.txt >/dev/null 4>&-; echo rc=$?"
		cmd := exec.CommandContext(t.Context(), sh[0], append(sh[1:], "-c", script)...)
		cmd.Dir = checkout
		cmd.Env = l.env()
		out, err := cmd.CombinedOutput()
		t.Logf("%v: err %v\n%s", sh, err, out)
		b, _ := os.ReadFile(log)
		t.Logf("%v helper fds:\n%s", sh, b)
		_ = os.Remove(log)
	}
}

func TestZZProbe_AutoGC(t *testing.T) {
	l := newRelayLab(t)
	url, _ := l.repo(t, false, "org/app-1/appdev")
	checkout := l.checkout(t, url)
	for _, kv := range [][2]string{{"gc.auto", "1"}, {"gc.autoDetach", "true"}, {"maintenance.autoDetach", "true"}, {"maintenance.gc.enabled", "true"}} {
		l.git(t, checkout, "config", kv[0], kv[1])
	}
	l.looseObjectsUnder17(t, checkout, 3)
	seen := filepath.Join(t.TempDir(), "seen")
	if err := os.WriteFile(filepath.Join(checkout, ".git", "hooks", "pre-auto-gc"), []byte("#!/bin/sh\necho ran >> "+seen+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "git", "-c", "gc.autoDetach=false", "-c", "maintenance.autoDetach=false", "commit", "--allow-empty", "-m", "x")
	cmd.Dir = checkout
	cmd.Env = append(l.env(), "GIT_TRACE=1")
	out, err := cmd.CombinedOutput()
	t.Logf("commit err %v\n%s", err, out)
	b, _ := os.ReadFile(seen)
	t.Logf("pre-auto-gc: %q", b)
	cfg := exec.CommandContext(t.Context(), "git", "config", "--list", "--show-origin")
	cfg.Dir = checkout
	cfg.Env = l.env()
	c, _ := cfg.CombinedOutput()
	t.Logf("config:\n%s", c)
}
