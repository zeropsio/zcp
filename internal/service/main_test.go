package service_test

import (
	"context"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/service"
)

// keepNothing stands in for the HQ enrollment and the delivery keep every
// mate launch starts: no test reaches out to Zerops or an HQ.
func keepNothing(context.Context, func() func(string) string) {}

func TestMain(m *testing.M) {
	service.SetMateHQKeep(keepNothing)
	service.SetMateDeliveryKeep(keepNothing)
	os.Exit(m.Run())
}
