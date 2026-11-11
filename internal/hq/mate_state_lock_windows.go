//go:build windows

package hq

import "os"

// The Mate server runs only in a Linux container.
func lockMateState(*os.File) error              { return nil }
func probeMateStateLock(*os.File) (bool, error) { return false, nil }
