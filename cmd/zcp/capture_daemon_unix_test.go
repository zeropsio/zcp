//go:build darwin || linux

package main

import (
	"os/exec"
	"testing"
)

func TestStopUnreadyCaptureDaemonWaitsForOwnedChildExit(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	if err := stopUnreadyCaptureDaemon(cmd, waited); err != nil {
		t.Fatalf("stopUnreadyCaptureDaemon() error = %v", err)
	}
	if cmd.ProcessState == nil {
		t.Fatal("owned child has no terminal process state after rollback")
	}
}
