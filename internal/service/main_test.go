package service_test

import (
	"context"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/service"
)

// keepNothing stands in for the HQ enrollment keep every mate launch starts:
// no test reaches out to Zerops or an HQ.
func keepNothing(context.Context, func() func(string) string) {}

// seedNothing stands in for the sign-in seed every mate launch runs: no test
// reads a project's tags.
func seedNothing(context.Context, func(string) string) {}

func prepareNothing(context.Context, func(string) string) error { return nil }

// tuneNothing stands in for the TasksMax tune every vscode and mate start
// runs: no test shells out to `sudo systemctl set-property` — on a dev box
// sudo can sit on a password prompt until the tune's 10 s timeout kills it,
// and on a Zerops container it would retune the live unit. A test that
// stubs the tuner restores this one, never the real tuner.
func tuneNothing(string, int) error { return nil }

func TestMain(m *testing.M) {
	service.SetMateHQKeep(keepNothing)
	service.SetMateSeedSignIns(seedNothing)
	service.SetMateHQPrepare(prepareNothing)
	service.SetTuneFunc(tuneNothing)
	os.Exit(m.Run())
}
