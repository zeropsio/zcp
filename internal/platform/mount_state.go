package platform

// MountState represents the tri-state result of checking a mount point.
type MountState int

const (
	MountStateNotMounted MountState = iota
	MountStateActive
	MountStateStale
)
