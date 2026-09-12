//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadScenarioSnapshotFile_ReplacementFIFOIsRejectedWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "scenario.md")
	if err := os.WriteFile(source, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(source, 0o600); err != nil {
		t.Skipf("FIFO unavailable: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := readScenarioSnapshotFile(source, expected)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("readScenarioSnapshotFile accepted replacement FIFO")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readScenarioSnapshotFile blocked opening replacement FIFO")
	}
}
