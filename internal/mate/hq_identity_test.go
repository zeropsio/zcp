package mate_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
)

// TestHQIdentity_NoOriginAnywhere_NamesBothSources: a container whose
// environment and installed Mate both name no HQ says so for each, so the
// reader knows the declaration was consulted too.
func TestHQIdentity_NoOriginAnywhere_NamesBothSources(t *testing.T) {
	// non-parallel: HOME is process-wide.
	t.Setenv("HOME", t.TempDir())
	env := map[string]string{hq.EnvKey: "test-key", hq.EnvProjectID: "p-mate"}
	_, err := hq.Open(http.DefaultClient, mate.HQIdentity(func(name string) string { return env[name] }))
	if err == nil {
		t.Fatal("Open succeeded without an HQ origin")
	}
	for _, want := range []string{hq.EnvOrigin, "installed Mate declares none"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}
