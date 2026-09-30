//go:build windows

package mate

import "os"

// A Mate runs only in a Linux container; on Windows the lock is always free.
func tryLockFile(*os.File) (bool, error) { return true, nil }

func unlockFile(*os.File) error { return nil }
