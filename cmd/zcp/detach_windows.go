//go:build windows

package main

import "syscall"

// detachedSysProcAttr: Windows has no sessions; the farm runner is a Linux
// container concern, so the detached child starts with default attributes.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return nil
}
