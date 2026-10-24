package platform

import (
	"net/http"
	"sync/atomic"
)

// keyRefusalHook is what a client tells when Zerops refuses its key.
type keyRefusalHook = atomic.Pointer[func()]

// OnKeyRefused registers fn to hear each answer in which Zerops refuses this
// client's key itself: 401, the token unknown, expired or revoked. A 403 or a
// 404 is about the resource a valid key asked for and is never told (the
// reason, not the error class, decides: docs/spec-mate.md §3.3). fn runs on
// the calling goroutine before the call returns, so it must not block.
func (z *ZeropsClient) OnKeyRefused(fn func()) {
	z.keyRefused.Store(&fn)
}

// keyRefusalTransport tells the hook of each 401 answer base gives.
type keyRefusalTransport struct {
	base http.RoundTripper
	hook *keyRefusalHook
}

func (t keyRefusalTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		if fn := t.hook.Load(); fn != nil {
			(*fn)()
		}
	}
	return resp, err
}
