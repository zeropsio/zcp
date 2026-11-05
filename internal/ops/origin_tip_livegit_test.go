//go:build livegit

package ops

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestReadOriginTip_StallingOrigin_EndsWithinItsBound: an origin that answers
// and then stops feeding ends the read within its bound — git's stall bound,
// the call's deadline — with an error, never a hang.
func TestReadOriginTip_StallingOrigin_EndsWithinItsBound(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "001e# service=git-upload-pack\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	start := time.Now()
	_, err := readOriginTipWithin(context.Background(), originBounds{call: 4 * time.Second, stallSeconds: 1}, srv.URL+"/app.git", "main", "")

	if err == nil {
		t.Fatal("a stalling origin answered no error")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("the read took %s, want it ended by its bound", took)
	}
}
