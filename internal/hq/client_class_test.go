package hq

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// witness: internal/hq/client.go | if r.Status >= http.StatusInternalServerError || r.Status == http.StatusTooManyRequests { → if r.Status == http.StatusTooManyRequests {
func TestErrorClass_E9_HQNotServingIsUnreachableAndItsNoIsRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want platform.ErrorClass
	}{
		{fmt.Errorf("open repo: %w", &RefusedError{Status: http.StatusBadGateway, Code: "bad_gateway"}), platform.ClassUnreachable},
		{fmt.Errorf("open repo: %w", &RefusedError{Status: http.StatusTooManyRequests, Code: "rate_limited"}), platform.ClassUnreachable},
		{fmt.Errorf("open repo: %w", &RefusedError{Status: http.StatusForbidden, Code: "forbidden", Reason: "repo.read"}), platform.ClassRefused},
		{fmt.Errorf("open repo: %w", &UnavailableError{Code: "unavailable"}), platform.ClassUnreachable},
	}
	for _, c := range cases {
		if got := platform.ClassOf(c.err); got != c.want {
			t.Errorf("ClassOf(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}
