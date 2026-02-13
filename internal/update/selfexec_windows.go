//go:build windows

package update

import "errors"

// Exec is not supported on Windows — the user must manually restart.
func Exec() error {
	return errors.New("auto-restart not supported on Windows; please restart ZCP manually")
}
