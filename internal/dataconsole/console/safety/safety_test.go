package safety

import (
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

func TestAuthorizeWrite(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		allowWrites bool
		confirmed   bool
		want        error
	}{
		{"read-only blocks even confirmed", false, true, provider.ErrReadOnly},
		{"read-only blocks unconfirmed", false, false, provider.ErrReadOnly},
		{"writes-on needs confirm", true, false, provider.ErrNeedsConfirm},
		{"writes-on confirmed ok", true, true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Policy{AllowWrites: c.allowWrites}.AuthorizeWrite(c.confirmed)
			if !errors.Is(got, c.want) {
				t.Fatalf("AuthorizeWrite(%v) = %v, want %v", c.confirmed, got, c.want)
			}
		})
	}
}
