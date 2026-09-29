package workflow

import (
	"testing"
	"time"
)

var devServerClock = time.Date(2026, 9, 29, 21, 0, 0, 0, time.UTC)

func keptPtr(k KeptDevServer) *KeptDevServer { return &k }

func keptFixture(host, container string) KeptDevServer {
	return KeptDevServer{
		Hostname:   host,
		Command:    "npm run dev",
		Port:       3000,
		HealthPath: "/",
		Container:  container,
		StartedAt:  "2026-09-29T20:20:00Z",
	}
}

// keptAfterRestore is a kept server whose last bring-back, in container life
// container, ended as given at the given moment.
func keptAfterRestore(container string, at time.Time, running bool, reason string, attempts int) *KeptDevServer {
	rec := keptFixture("appdev", container)
	rec.LastRestore = &DevServerRestore{At: at.Format(time.RFC3339), Running: running, Reason: reason, Attempts: attempts}
	return &rec
}

// TestKeepDevServer_RoundTrip pins the store: what zerops_dev_server started
// is kept per hostname, a later start replaces it, and stop forgets it.
func TestKeepDevServer_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if got, err := KeptDevServerFor(dir, "appdev"); err != nil || got != nil {
		t.Fatalf("empty store: got %+v, %v; want nil, nil", got, err)
	}
	if err := KeepDevServer(dir, keptFixture("appdev", "appdev-1/100")); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	if err := KeepDevServer(dir, keptFixture("apidev", "apidev-1/200")); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	replaced := keptFixture("appdev", "appdev-1/100")
	replaced.Command = "npm run dev -- --host 0.0.0.0"
	if err := KeepDevServer(dir, replaced); err != nil {
		t.Fatalf("KeepDevServer (replace): %v", err)
	}

	got, err := KeptDevServerFor(dir, "appdev")
	if err != nil || got == nil {
		t.Fatalf("KeptDevServerFor: %+v, %v", got, err)
	}
	if got.Command != replaced.Command {
		t.Errorf("a later start replaces the kept command: got %q", got.Command)
	}

	all, err := KeptDevServers(dir)
	if err != nil {
		t.Fatalf("KeptDevServers: %v", err)
	}
	if len(all) != 2 || all[0].Hostname != "apidev" || all[1].Hostname != "appdev" {
		t.Errorf("KeptDevServers = %+v, want apidev then appdev", all)
	}

	if err := ForgetDevServer(dir, "appdev"); err != nil {
		t.Fatalf("ForgetDevServer: %v", err)
	}
	if got, _ := KeptDevServerFor(dir, "appdev"); got != nil {
		t.Errorf("a stopped dev server is forgotten, got %+v", got)
	}
	if err := ForgetDevServer(dir, "nothing-here"); err != nil {
		t.Errorf("forgetting what was never kept is a no-op, got %v", err)
	}
	if err := KeepDevServer(dir, keptFixture("appdev", "")); err == nil {
		t.Error("a start whose container life is unknown cannot be kept: a restart could not be told from a crash")
	}
}

// TestClaimKeptDevServer pins who may bring a kept server back, and when: once
// as soon as its container life ended, again in that life only after a
// bring-back that did not come up (bounded, spaced) — never while another
// caller's bring-back runs, never over a server that is up or that the agent
// started in this life.
func TestClaimKeptDevServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		kept         *KeptDevServer
		container    string
		now          time.Time
		wantClaim    bool
		wantAttempts int
	}{
		{name: "nothing kept", kept: nil, container: "appdev-1/300", now: devServerClock},
		{name: "the agent's own start in this life", kept: keptPtr(keptFixture("appdev", "appdev-1/100")), container: "appdev-1/100", now: devServerClock},
		{name: "restarted — a new container life", kept: keptPtr(keptFixture("appdev", "appdev-1/100")), container: "appdev-1/900", now: devServerClock, wantClaim: true, wantAttempts: 1},
		{name: "redeployed — a new container", kept: keptPtr(keptFixture("appdev", "appdev-1/100")), container: "appdev-2/50", now: devServerClock, wantClaim: true, wantAttempts: 1},
		{name: "unreadable identity claims nothing", kept: keptPtr(keptFixture("appdev", "appdev-1/100")), container: "", now: devServerClock},
		{name: "a new life after an old restore starts counting again", kept: keptAfterRestore("appdev-1/100", devServerClock.Add(-time.Hour), false, "health_probe_timeout", 3), container: "appdev-1/900", now: devServerClock, wantClaim: true, wantAttempts: 1},
		{name: "brought back and up — a later crash stays down", kept: keptAfterRestore("appdev-1/900", devServerClock.Add(-time.Hour), true, "", 1), container: "appdev-1/900", now: devServerClock},
		{name: "another caller's bring-back is running", kept: keptAfterRestore("appdev-1/900", devServerClock.Add(-10*time.Second), false, DevServerRestoring, 1), container: "appdev-1/900", now: devServerClock},
		{name: "a bring-back whose claimer died is taken again", kept: keptAfterRestore("appdev-1/900", devServerClock.Add(-10*time.Minute), false, DevServerRestoring, 1), container: "appdev-1/900", now: devServerClock, wantClaim: true, wantAttempts: 2},
		{name: "a failed bring-back is retried once spaced", kept: keptAfterRestore("appdev-1/900", devServerClock.Add(-time.Minute), false, "health_probe_connection_refused", 1), container: "appdev-1/900", now: devServerClock, wantClaim: true, wantAttempts: 2},
		{name: "but not straight away", kept: keptAfterRestore("appdev-1/900", devServerClock.Add(-5*time.Second), false, "health_probe_connection_refused", 1), container: "appdev-1/900", now: devServerClock},
		{name: "and not past the bound", kept: keptAfterRestore("appdev-1/900", devServerClock.Add(-time.Hour), false, "health_probe_connection_refused", MaxDevServerRestoreAttempts), container: "appdev-1/900", now: devServerClock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tt.kept != nil {
				if err := KeepDevServer(dir, *tt.kept); err != nil {
					t.Fatalf("KeepDevServer: %v", err)
				}
			}
			rec, claimed, err := ClaimKeptDevServer(dir, "appdev", tt.container, tt.now)
			if err != nil {
				t.Fatalf("ClaimKeptDevServer: %v", err)
			}
			if claimed != tt.wantClaim {
				t.Fatalf("claimed = %v, want %v", claimed, tt.wantClaim)
			}
			if !claimed {
				return
			}
			if rec == nil || rec.Command != "npm run dev" || rec.Container != tt.container {
				t.Fatalf("a claim returns the kept start in the claimed life, got %+v", rec)
			}
			stored, _ := KeptDevServerFor(dir, "appdev")
			if stored.LastRestore == nil || stored.LastRestore.Reason != DevServerRestoring || stored.LastRestore.Attempts != tt.wantAttempts {
				t.Fatalf("the claim marks the attempt in progress: %+v, want attempts=%d", stored.LastRestore, tt.wantAttempts)
			}
			if _, again, _ := ClaimKeptDevServer(dir, "appdev", tt.container, tt.now); again {
				t.Error("a second caller must not claim a bring-back already running")
			}
		})
	}
}

// TestRecordDevServerRestore keeps the outcome of the bring-back with the
// claim's attempt count — and drops it when a newer start replaced the record
// in between (its container moved on).
func TestRecordDevServerRestore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := KeepDevServer(dir, keptFixture("appdev", "appdev-1/100")); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	if _, claimed, _ := ClaimKeptDevServer(dir, "appdev", "appdev-2/50", devServerClock); !claimed {
		t.Fatal("claim")
	}
	restore := DevServerRestore{At: "2026-09-29T21:00:20Z", Running: false, Reason: "health_probe_timeout"}
	if err := RecordDevServerRestore(dir, "appdev", "appdev-2/50", restore); err != nil {
		t.Fatalf("RecordDevServerRestore: %v", err)
	}
	got, _ := KeptDevServerFor(dir, "appdev")
	want := restore
	want.Attempts = 1
	if got.LastRestore == nil || *got.LastRestore != want {
		t.Errorf("LastRestore = %+v, want %+v", got.LastRestore, want)
	}

	if err := RecordDevServerRestore(dir, "appdev", "appdev-9/1", DevServerRestore{At: "x", Running: true}); err != nil {
		t.Fatalf("RecordDevServerRestore (stale): %v", err)
	}
	got, _ = KeptDevServerFor(dir, "appdev")
	if got.LastRestore == nil || got.LastRestore.At != restore.At {
		t.Errorf("a restore for another container life must not overwrite, got %+v", got.LastRestore)
	}
}

// TestTakeOverKeptDevServer: an agent starting the kept server in a new life
// makes that life its own — no bring-back is claimed in it alongside.
func TestTakeOverKeptDevServer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := TakeOverKeptDevServer(dir, "appdev", "appdev-1/900"); err != nil {
		t.Fatalf("nothing kept: %v", err)
	}
	if got, _ := KeptDevServerFor(dir, "appdev"); got != nil {
		t.Fatalf("taking over nothing keeps nothing, got %+v", got)
	}
	if err := KeepDevServer(dir, *keptAfterRestore("appdev-1/100", devServerClock, false, "health_probe_timeout", 2)); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	if err := TakeOverKeptDevServer(dir, "appdev", "appdev-1/900"); err != nil {
		t.Fatalf("TakeOverKeptDevServer: %v", err)
	}
	got, _ := KeptDevServerFor(dir, "appdev")
	if got.Container != "appdev-1/900" || got.LastRestore != nil {
		t.Fatalf("the life is the agent's: %+v", got)
	}
	if _, claimed, _ := ClaimKeptDevServer(dir, "appdev", "appdev-1/900", devServerClock.Add(time.Hour)); claimed {
		t.Error("no bring-back is claimed in a life the agent took over")
	}
}
