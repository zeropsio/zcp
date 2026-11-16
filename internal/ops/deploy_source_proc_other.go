//go:build !unix

package ops

// processGone never reports a process gone on non-Unix platforms, which have
// no signal 0 to ask: a left scratch directory stays rather than risk a live
// one.
func processGone(int) bool {
	return false
}
