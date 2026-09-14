//go:build !windows

package main

import "syscall"

// detachedSysProcAttr puts the detached farm runner in its own session so it
// survives the kickoff SSH session dropping (docs/spec-eval-farm.md §3.1 FM-18).
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
