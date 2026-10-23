package hq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestKeep_AddressFromTheLaunchEnvironment: an HQ address the launch
// environment names (EnvAddress) is the HQ — enrollment, recheck and status
// go to it, and the org's member list is never read; unset, the anchor names
// the HQ as before; a Mate enrolled at another address is enrolled anew at
// the configured one, while one enrolled at it keeps its credential.
func TestKeep_AddressFromTheLaunchEnvironment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// enrolledAtAnchor: enrolled with the anchor's HQ before the address is set.
		enrolledAtAnchor bool
		// noAnchor: the org's member list names no HQ.
		noAnchor bool
		// address is the configured value given the configured HQ and the anchor's.
		address func(configured, anchor string) string
		// want is the HQ the Mate ends enrolled with.
		want           func(configured, anchor string) string
		wantCredential string
		wantMemberRead bool
	}{
		{"unset: the anchor names the HQ", false, false,
			func(string, string) string { return "" },
			func(_, anchor string) string { return anchor }, "credential-1", true},
		{"set: the configured HQ, with no anchor in the org", false, true,
			func(configured, _ string) string { return configured + "/" },
			func(configured, _ string) string { return configured }, "credential-1", false},
		{"set, enrolled at the anchor's address: enrolled anew at the configured one", true, false,
			func(configured, _ string) string { return configured },
			func(configured, _ string) string { return configured }, "credential-1", false},
		{"set to the address it is enrolled at: its credential kept", true, false,
			func(_, anchor string) string { return anchor },
			func(_, anchor string) string { return anchor }, "credential-1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			configured := newHQStub(r.zerops)
			srv := httptest.NewServer(http.HandlerFunc(configured.handler))
			t.Cleanup(srv.Close)
			if tt.enrolledAtAnchor {
				mustEnroll(t, r)
			}
			if tt.noAnchor {
				r.zerops.members = r.zerops.members[:1]
			}
			counting := &countingZerops{Zerops: r.enroller.Zerops}
			e := r.enroller
			e.Zerops = counting
			e.Address = tt.address(srv.URL, r.hqURL)
			want := tt.want(srv.URL, r.hqURL)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			log := &keepLog{}
			go Keep(ctx, e.Enroll, e.Recheck, e.Moved, KeepOptions{
				Retry: 2 * time.Millisecond, RetryMax: 8 * time.Millisecond, Recheck: time.Hour,
				Log: log.say, Record: log.record,
			})
			log.await(t, "the Mate enrolled", func(o []Outcome) bool {
				return len(o) > 0 && o[len(o)-1].State == OutcomeEnrolled
			})
			cancel()

			kept, found, err := LoadEnrollment(e.Path)
			if err != nil || !found || kept.HQ != want || kept.Credential != tt.wantCredential {
				t.Fatalf("kept %+v (found %v, err %v), want %s with %s", kept, found, err, want, tt.wantCredential)
			}
			if _, err := e.Recheck(context.Background()); err != nil {
				t.Errorf("the HQ at %s does not know the kept credential: %v", want, err)
			}
			status := e.Status(context.Background())
			if status.HQ.Verdict != VerdictOfficial || status.HQ.Address != want || !status.Enrolled {
				t.Errorf("Status = %+v, want enrolled with %s", status, want)
			}
			counting.mu.Lock()
			read := counting.members > 0
			counting.mu.Unlock()
			if read != tt.wantMemberRead {
				t.Errorf("read the member list: %v, want %v", read, tt.wantMemberRead)
			}
		})
	}
}
