package service_test

import (
	"context"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/service"
)

// seedNothing stands in for the sign-in seed every mate launch runs: no test
// reads a project's tags.
func seedNothing(context.Context, func(string) string) {}

func TestMain(m *testing.M) {
	service.SetMateSeedSignIns(seedNothing)
	os.Exit(m.Run())
}
