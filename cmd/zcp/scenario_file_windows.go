//go:build windows

package main

import "os"

func openScenarioSnapshotFile(path string) (*os.File, error) {
	return os.Open(path)
}
