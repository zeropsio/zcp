package eval

import (
	"testing"
)

// TestProcessIdentity_ClassifyAndAccept pins the portable half of
// docs/spec-testing-architecture.md §10.4 "Observed process identity":
// ProcessIdentityAccepted over a fabricated observation set, independent of
// how the observations were produced (Linux /proc reader or the
// non-Linux "unsupported" stand-in).
func TestProcessIdentity_ClassifyAndAccept(t *testing.T) {
	t.Parallel()
	const wantProject = "proj-bound"

	cases := []struct {
		name       string
		result     *BehavioralResult
		wantOK     bool
		wantReason string
	}{
		{
			name:       "no observations at all",
			result:     &BehavioralResult{Binding: &ExecutionBindingRecord{ProjectID: wantProject}},
			wantOK:     false,
			wantReason: "process identity: none",
		},
		{
			name: "unsupported OS only",
			result: &BehavioralResult{
				Binding:         &ExecutionBindingRecord{ProjectID: wantProject},
				ProcessIdentity: []ProcessIdentity{{Classification: processIdentityUnsupported}},
			},
			wantOK:     false,
			wantReason: "process identity: unsupported OS",
		},
		{
			name: "unobservable only",
			result: &BehavioralResult{
				Binding:         &ExecutionBindingRecord{ProjectID: wantProject},
				ProcessIdentity: []ProcessIdentity{{PID: 111, Classification: processIdentityUnobserved}},
			},
			wantOK:     false,
			wantReason: "process identity: none",
		},
		{
			name: "counted match",
			result: &BehavioralResult{
				Binding: &ExecutionBindingRecord{ProjectID: wantProject},
				ProcessIdentity: []ProcessIdentity{
					{PID: 222, Classification: processIdentityCounted, MatchesCandidate: true, ProjectID: wantProject},
				},
			},
			wantOK: true,
		},
		{
			name: "counted contradiction by digest",
			result: &BehavioralResult{
				Binding: &ExecutionBindingRecord{ProjectID: wantProject},
				ProcessIdentity: []ProcessIdentity{
					{PID: 333, Classification: processIdentityCounted, MatchesCandidate: false, ProjectID: wantProject},
				},
			},
			wantOK:     false,
			wantReason: "process identity: contradiction pid 333",
		},
		{
			name: "counted contradiction by project",
			result: &BehavioralResult{
				Binding: &ExecutionBindingRecord{ProjectID: wantProject},
				ProcessIdentity: []ProcessIdentity{
					{PID: 444, Classification: processIdentityCounted, MatchesCandidate: true, ProjectID: "some-other-project"},
				},
			},
			wantOK:     false,
			wantReason: "process identity: contradiction pid 444",
		},
		{
			name: "match plus a contradicting observation is still blocked",
			result: &BehavioralResult{
				Binding: &ExecutionBindingRecord{ProjectID: wantProject},
				ProcessIdentity: []ProcessIdentity{
					{PID: 555, Classification: processIdentityCounted, MatchesCandidate: true, ProjectID: wantProject},
					{PID: 666, Classification: processIdentityCounted, MatchesCandidate: false, ProjectID: wantProject},
				},
			},
			wantOK:     false,
			wantReason: "process identity: contradiction pid 666",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, reason := ProcessIdentityAccepted(tc.result)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v (reason=%q)", ok, tc.wantOK, reason)
			}
			if !tc.wantOK && reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}
