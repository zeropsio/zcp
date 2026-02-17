//go:build !windows

package update

import (
	"os"
	"os/signal"
	"syscall"
)

// ForceCheckSignal returns a channel that receives when SIGUSR1 is sent.
// Returns nil if signal handling fails.
func ForceCheckSignal() <-chan struct{} {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGUSR1)
	ch := make(chan struct{}, 1)
	go func() {
		for range sigCh {
			select {
			case ch <- struct{}{}:
			default: // don't block if previous signal not consumed
			}
		}
	}()
	return ch
}
