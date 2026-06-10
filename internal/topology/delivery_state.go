package topology

// DeliveryState is the DERIVED rung of the delivery ladder
// (spec-git-delivery-target §2). Never stored — computed read-time from
// the meta axes + live facts, so it cannot drift (the derive-never-stamp
// doctrine). The ladder dictates the delivery MECHANISM; CloseDeployMode
// is reduced to done-ness OWNERSHIP (auto = ZCP derives done, manual =
// the user owns the loop).
type DeliveryState string

const (
	// DeliveryArtifact (L0): git-push not configured — the artifact is
	// the durability act; ZCP self/cross-deploys exactly as before the
	// ladder existed. Pre-git states keep today's flow (fundament §1.5,
	// the D2a first-deploy exception generalized).
	DeliveryArtifact DeliveryState = "artifact"
	// DeliveryRepo (L1): GitPushState=configured — the REPO is the
	// single source of truth and PUSH is the terminal act of
	// development; builds flow from the repo via integrations. The push
	// source receives no ZCP deploys outside break-glass.
	DeliveryRepo DeliveryState = "repo"
	// DeliveryReleased (L2): L1 + production launched (the source pair
	// carries at least one ProdLaunches back-reference). Adds the
	// release act (tag push → prod pipeline) on top of L1.
	DeliveryReleased DeliveryState = "released"
)

// DeriveDeliveryState computes the ladder rung from the two facts that
// own it: the verified git-push capability and the post-launch
// back-reference. Pure derivation — both inputs are recorded evidence
// (GitPushState is probe-earned, ProdLaunches is launch-stamped).
func DeriveDeliveryState(gitPush GitPushState, hasProdLaunches bool) DeliveryState {
	if gitPush != GitPushConfigured {
		return DeliveryArtifact
	}
	if hasProdLaunches {
		return DeliveryReleased
	}
	return DeliveryRepo
}
