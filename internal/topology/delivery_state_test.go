package topology

import "testing"

// TestDeriveDeliveryState pins the ladder derivation
// (spec-git-delivery-target §2): L0 artifact until git-push is
// configured; L1 repo once it is; L2 released once the source pair
// feeds a launched production project. Derived read-time, never stored.
func TestDeriveDeliveryState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		gitPush  GitPushState
		prodRefs bool
		want     DeliveryState
	}{
		{"unconfigured is artifact", GitPushUnconfigured, false, DeliveryArtifact},
		{"broken is artifact", GitPushBroken, false, DeliveryArtifact},
		{"configured is repo", GitPushConfigured, false, DeliveryRepo},
		{"configured + prod launch is released", GitPushConfigured, true, DeliveryReleased},
		{"prod refs without capability stay artifact", GitPushUnconfigured, true, DeliveryArtifact},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DeriveDeliveryState(tc.gitPush, tc.prodRefs); got != tc.want {
				t.Errorf("DeriveDeliveryState(%q, %v) = %q, want %q", tc.gitPush, tc.prodRefs, got, tc.want)
			}
		})
	}
}
