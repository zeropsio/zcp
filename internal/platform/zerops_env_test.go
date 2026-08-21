// Tests for: internal/platform/zerops_env.go — service userData writes
// under the platform's 2026-08 model (docs/spec-zerops-env-lifecycle.md
// §1/§7): sensitive is a REQUIRED write-side flag the pinned SDK body
// doesn't carry, so CreateServiceEnvVar hand-rolls the POST on the SDK's
// own authorized transport (sdkBase.Post) instead of the generated
// PostServiceStackUserData handler.
package platform

import (
	"reflect"
	"testing"

	"github.com/zeropsio/zerops-go/dto/input/body"
)

// TestSDKUserDataBody_StillLacksSensitive is a drift guard, not a red/green
// feature test: it passes BY DESIGN today (the pinned zerops-go SDK's
// generated body.UserDataPost has no Sensitive field), and is expected to
// keep passing until zerops-go ships the field. Its RED was verified once,
// manually, by temporarily inverting the assertion to require Sensitive to
// be present — confirmed to fail for the right reason (the field really is
// absent) — then reverted to this, the real guard. If this test ever FAILS
// in CI, the SDK gained the field: retire the hand-rolled POST in
// zerops_env.go and call the generated PostServiceStackUserData handler
// directly instead.
func TestSDKUserDataBody_StillLacksSensitive(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[body.UserDataPost]()
	if _, ok := typ.FieldByName("Sensitive"); ok {
		t.Fatal("zerops-go now carries Sensitive on UserDataPost — retire the hand-rolled POST in zerops_env.go and use the SDK handler")
	}
}
