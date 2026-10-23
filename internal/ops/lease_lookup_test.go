package ops

import "testing"

func TestLeaseTarget_ResolvesRecordedAliasOnly(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	rec := leaseRecord{
		PoolHostname:      "webpool",
		PoolServiceID:     "svc-pool",
		ContainerID:       "c-3",
		ContainerNumber:   3,
		ContainerHostname: "node-id-3.runtime.webpool.zerops",
		Lease:             "secret-lease-value",
		Branch:            "feat-x",
		AcquiredAt:        "2026-10-23T12:00:00Z",
	}
	if err := writeLeaseRecord(stateDir, rec); err != nil {
		t.Fatalf("write record: %v", err)
	}

	tests := []struct {
		name   string
		host   string
		wantOK bool
		want   LeaseTargetInfo
	}{
		{name: "recorded alias", host: "webpool-3", wantOK: true, want: LeaseTargetInfo{
			Alias: "webpool-3", Pool: "webpool", PoolServiceID: "svc-pool",
			ContainerHostname: "node-id-3.runtime.webpool.zerops", Branch: "feat-x",
		}},
		{name: "alias shape but no record", host: "webpool-4", wantOK: false},
		{name: "service hostname", host: "appdev", wantOK: false},
		{name: "empty", host: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok, err := LeaseTarget(stateDir, tt.host)
			if err != nil {
				t.Fatalf("LeaseTarget(%q) error: %v", tt.host, err)
			}
			if ok != tt.wantOK {
				t.Fatalf("LeaseTarget(%q) ok = %v, want %v", tt.host, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("LeaseTarget(%q) = %+v, want %+v", tt.host, got, tt.want)
			}
		})
	}
}

func TestLeaseTarget_NoStateDir_NotALease(t *testing.T) {
	t.Parallel()
	_, ok, err := LeaseTarget("", "webpool-3")
	if err != nil || ok {
		t.Fatalf("LeaseTarget with no state dir = ok %v err %v, want not a lease, no error", ok, err)
	}
}
