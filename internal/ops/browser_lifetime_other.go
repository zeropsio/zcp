//go:build !linux

package ops

import (
	"context"
	"errors"
	"time"
)

var errBrowserNeedsLinux = errors.New("the managed browser requires a Linux systemd container")

func (execBrowserRunner) Acquire(context.Context, string) (func(bool) error, error) {
	return nil, errBrowserNeedsLinux
}

func (execBrowserRunner) Run(context.Context, string, time.Duration) (string, string, bool, error) {
	return "", "", false, errBrowserNeedsLinux
}
