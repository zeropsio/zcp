// Tests for: ops/lease.go — a lease view names the repository its group is
// keyed by (docs/spec-workflows.md §8 DP-17, docs/spec-mate.md §6.3).
package ops

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestLeaseViews_CarryTheRepository: every view of a lease names the
// repository its group is keyed by beside the branch (DP-17) — the record's
// canonical identity, credential-free: no userinfo, query or fragment ever
// reaches a view — so a client binding a conversation to a lease tells two
// apps on the same branch apart (docs/spec-mate.md §6.3).
func TestLeaseViews_CarryTheRepository(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, repo, want string
	}{
		{name: "the canonical identity", repo: "https://github.com/acme/app", want: "https://github.com/acme/app"},
		{name: "a query or fragment never rides", repo: "https://git.example.com/acme/app?token=s3cr3t#frag", want: "https://git.example.com/acme/app"},
		{name: "userinfo never rides", repo: "https://user:s3cr3t@git.example.com/acme/app", want: "https://git.example.com/acme/app"},
		{name: "an older record names none", repo: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := leaseRecord{PoolHostname: "webpool", PoolServiceID: "svc", ContainerID: "c-1", ContainerNumber: 1,
				ContainerHostname: "node-id-1.runtime.webpool.zerops", Lease: testLease, Branch: "main", Repo: tt.repo, AcquiredAt: "2026-10-25T10:00:00Z"}
			for _, state := range []string{LeaseStateHeld, LeaseStateLost, LeaseStateParked} {
				raw, err := json.Marshal(rec.view(state))
				if err != nil {
					t.Fatal(err)
				}
				var got struct {
					Repo *string `json:"repo"`
				}
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatal(err)
				}
				switch {
				case tt.want == "" && got.Repo != nil:
					t.Errorf("%s view carries repo %q, want none", state, *got.Repo)
				case tt.want != "" && (got.Repo == nil || *got.Repo != tt.want):
					t.Errorf("%s view = %s, want repo %q", state, raw, tt.want)
				}
				if strings.Contains(string(raw), "s3cr3t") || strings.Contains(string(raw), testLease) {
					t.Errorf("%s view carries a credential: %s", state, raw)
				}
			}
		})
	}
}
