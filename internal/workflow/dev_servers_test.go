package workflow

import (
	"testing"
)

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

// TestClaimKeptDevServer pins the at-most-once bring-back per container life:
// the first caller that sees the container changed claims the restore, and
// every later caller in the same life (the keeper's next tick, a deploy's
// restore racing it) finds it claimed.
func TestClaimKeptDevServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		kept      *KeptDevServer
		container string
		wantClaim bool
	}{
		{name: "nothing kept", kept: nil, container: "appdev-1/300", wantClaim: false},
		{name: "same container life — nothing to bring back", kept: keptPtr(keptFixture("appdev", "appdev-1/100")), container: "appdev-1/100", wantClaim: false},
		{name: "restarted — a new container life", kept: keptPtr(keptFixture("appdev", "appdev-1/100")), container: "appdev-1/900", wantClaim: true},
		{name: "redeployed — a new container", kept: keptPtr(keptFixture("appdev", "appdev-1/100")), container: "appdev-2/50", wantClaim: true},
		{name: "unreadable identity claims nothing", kept: keptPtr(keptFixture("appdev", "appdev-1/100")), container: "", wantClaim: false},
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
			rec, claimed, err := ClaimKeptDevServer(dir, "appdev", tt.container)
			if err != nil {
				t.Fatalf("ClaimKeptDevServer: %v", err)
			}
			if claimed != tt.wantClaim {
				t.Fatalf("claimed = %v, want %v", claimed, tt.wantClaim)
			}
			if !claimed {
				return
			}
			if rec == nil || rec.Command != "npm run dev" {
				t.Fatalf("a claim returns the kept start, got %+v", rec)
			}
			if _, again, _ := ClaimKeptDevServer(dir, "appdev", tt.container); again {
				t.Error("a second caller in the same container life must not claim it again")
			}
		})
	}
}

// TestRecordDevServerRestore keeps the outcome of the last bring-back with the
// start it restored — and drops it when a newer start replaced the record in
// between (its container moved on).
func TestRecordDevServerRestore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := KeepDevServer(dir, keptFixture("appdev", "appdev-1/100")); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	if _, claimed, _ := ClaimKeptDevServer(dir, "appdev", "appdev-2/50"); !claimed {
		t.Fatal("claim")
	}
	restore := DevServerRestore{At: "2026-09-29T21:00:00Z", Running: false, Reason: "health_probe_timeout"}
	if err := RecordDevServerRestore(dir, "appdev", "appdev-2/50", restore); err != nil {
		t.Fatalf("RecordDevServerRestore: %v", err)
	}
	got, _ := KeptDevServerFor(dir, "appdev")
	if got.LastRestore == nil || *got.LastRestore != restore {
		t.Errorf("LastRestore = %+v, want %+v", got.LastRestore, restore)
	}

	if err := RecordDevServerRestore(dir, "appdev", "appdev-9/1", DevServerRestore{At: "x", Running: true}); err != nil {
		t.Fatalf("RecordDevServerRestore (stale): %v", err)
	}
	got, _ = KeptDevServerFor(dir, "appdev")
	if got.LastRestore == nil || got.LastRestore.At != restore.At {
		t.Errorf("a restore for another container life must not overwrite, got %+v", got.LastRestore)
	}
}
