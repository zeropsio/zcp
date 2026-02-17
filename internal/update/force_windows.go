//go:build windows

package update

// ForceCheckSignal returns nil on Windows — SIGUSR1 is not available.
func ForceCheckSignal() <-chan struct{} {
	return nil
}
