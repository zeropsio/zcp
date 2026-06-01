//go:build !windows && !darwin && !linux

package workflow

// processStartTime is unavailable on this platform; liveness falls back to a
// bare PID-exists check (two-state with PID-only identity). Returns ("", false)
// so isProcessAlive treats the recorded start-time as absent and trusts the PID.
func processStartTime(int) (string, bool) { return "", false }
