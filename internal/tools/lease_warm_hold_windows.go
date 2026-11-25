//go:build windows

package tools

import "syscall"

// heldWarmCloneProcAttr: Windows has no sessions, and a Mate's container is
// Linux, so the process starts with default attributes.
func heldWarmCloneProcAttr() *syscall.SysProcAttr {
	return nil
}
