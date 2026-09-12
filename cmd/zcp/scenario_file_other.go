//go:build !aix && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris && !windows

package main

import "os"

func openScenarioSnapshotFile(path string) (*os.File, error) {
	return os.Open(path)
}
