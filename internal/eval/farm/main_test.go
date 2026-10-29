package farm

import (
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/testgit"
)

// non-parallel: TestMain isolates Git's environment before package tests run.
func TestMain(m *testing.M) {
	os.Exit(testgit.Run(m))
}
