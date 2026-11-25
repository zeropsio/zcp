//go:build !windows

package tools

import "syscall"

// heldWarmCloneProcAttr puts the held warm clone's process in a session of its
// own, so no signal to its starter's process group reaches it.
func heldWarmCloneProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
