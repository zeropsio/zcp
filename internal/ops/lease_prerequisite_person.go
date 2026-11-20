package ops

import "github.com/zeropsio/zcp/internal/platform"

// leasePrerequisite is which missing prerequisite refused an acquire before
// anything was reserved.
type leasePrerequisite int

const (
	prerequisitePoolKey leasePrerequisite = iota
	prerequisitePoolUndeployed
	prerequisiteHoldingLost
	prerequisiteHoldingUnverified
)

// LeasePrerequisiteError carries which prerequisite refused an acquire.
// The platform error addresses an agent; PersonWords addresses the crew editor.
type LeasePrerequisiteError struct {
	*platform.PlatformError
	cause  leasePrerequisite
	pool   string
	branch string
	alias  string
}

func (e *LeasePrerequisiteError) Unwrap() error { return e.PlatformError }

// PersonWords returns the sentence pair the Mate joins at Apply.
func (e *LeasePrerequisiteError) PersonWords() (message, suggestion string) {
	switch e.cause {
	case prerequisitePoolKey:
		// Its words follow the switch.
	case prerequisitePoolUndeployed:
		return "Pool " + e.pool + " has not been deployed yet.",
			"Deploy the pool in Zerops, then try Apply again."
	case prerequisiteHoldingUnverified:
		return "Pool " + e.pool + " did not confirm that container " + e.alias + " is still this Mate's for branch " + e.branch + ".",
			"Check that the pool is running in Zerops, then try Apply again."
	case prerequisiteHoldingLost:
		return "Container " + e.alias + " on pool " + e.pool + " is no longer this Mate's for branch " + e.branch + ".",
			"Remove this builder from the crew and Apply, then add it again."
	}
	return "This Mate has no usable key for pool " + e.pool + ".",
		"Check the pool's key in Zerops, then try Apply again."
}
