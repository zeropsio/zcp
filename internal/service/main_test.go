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

func TestMain(m *testing.M) {
	service.SetMateHQKeep(keepNothing)
	service.SetMateSeedSignIns(seedNothing)
	service.SetMateHQPrepare(prepareNothing)
	os.Exit(m.Run())
}
