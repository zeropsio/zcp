package console

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

type getErrorStore struct {
	observer.ObjectStore
	key string
	err error
}

func (s getErrorStore) Get(ctx context.Context, key string) ([]byte, error) {
	if key == s.key {
		return nil, s.err
	}
	return s.ObjectStore.Get(ctx, key)
}

func TestPages_HTMLMissingRecord_RecoveryAndStatus(t *testing.T) {
	srv, store, _ := testServer(t)
	seedBatch(t, store, "error-batch", "off", []runFixture{{
		runID: "error-batch-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true,
	}}, true, map[string]string{"error-batch-a": "passed"})

	cases := []struct {
		path, title, parentHref, parentLabel string
	}{
		{"/missing-page", "Page not found", "/", "Back to overview"},
		{"/b/missing-batch", "Batch not found", "/", "Back to overview"},
		{"/r/missing-run", "Run not found", "/", "Back to overview"},
		{"/r/error-batch-a?obs=foreign-observation", "Assessment not found", "/r/error-batch-a", "Back to current run"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			rr := doGET(t, srv.Handler(), tc.path)
			body := rr.Body.String()
			if rr.Code != http.StatusNotFound || rr.Header().Get("Content-Type") != "text/html; charset=utf-8" ||
				!strings.Contains(body, `error-state`) || !strings.Contains(body, tc.title) ||
				!strings.Contains(body, `href="`+tc.parentHref+`"`) || !strings.Contains(body, tc.parentLabel) {
				t.Fatalf("GET %s: status=%d content-type=%q body=%s", tc.path, rr.Code, rr.Header().Get("Content-Type"), body)
			}
			if strings.Contains(body, "404 page not found") {
				t.Errorf("GET %s leaked the plain net/http 404 page", tc.path)
			}
		})
	}

	api := doGET(t, srv.Handler(), "/api/not-a-route")
	if api.Code != http.StatusNotFound || strings.Contains(api.Body.String(), "error-state") ||
		!strings.HasPrefix(api.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("missing API route changed to the HTML error shape: status=%d content-type=%q body=%s", api.Code, api.Header().Get("Content-Type"), api.Body.String())
	}
}

func TestPages_HTMLStoreFailure_RecoveryAndStatus(t *testing.T) {
	type pageCase struct {
		name, path, title string
		status            int
		build             func() *Server
	}
	listFailureServer := func() *Server {
		srv, store, _ := testServer(t)
		store.failListOn("batches/", errors.New("bucket-secret-must-not-leak"))
		return srv
	}
	batchFailureServer := func() *Server {
		srv, store, _ := testServer(t)
		seedBatch(t, store, "broken-batch", "off", nil, false, nil)
		srv.cfg.Store = getErrorStore{
			ObjectStore: srv.cfg.Store, key: "batches/broken-batch/manifest.json", err: errors.New("manifest-secret-must-not-leak"),
		}
		return srv
	}
	cases := []pageCase{
		{"overview", "/", "Overview unavailable", http.StatusInternalServerError, listFailureServer},
		{"findings", "/findings", "Findings unavailable", http.StatusBadGateway, listFailureServer},
		{"problems", "/problems", "Problems unavailable", http.StatusBadGateway, listFailureServer},
		{"batch", "/b/broken-batch", "Batch data unavailable", http.StatusBadGateway, batchFailureServer},
		{"run", "/r/known-run", "Run data unavailable", http.StatusBadGateway, listFailureServer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doGET(t, tc.build().Handler(), tc.path)
			body := rr.Body.String()
			if rr.Code != tc.status || rr.Header().Get("Content-Type") != "text/html; charset=utf-8" ||
				!strings.Contains(body, tc.title) || !strings.Contains(body, `href="`+tc.path+`"`) ||
				!strings.Contains(body, "Try again") || !strings.Contains(body, "Back to overview") {
				t.Fatalf("GET %s: status=%d content-type=%q body=%s", tc.path, rr.Code, rr.Header().Get("Content-Type"), body)
			}
			if strings.Contains(body, "secret-must-not-leak") {
				t.Errorf("GET %s exposed the raw store error: %s", tc.path, body)
			}
		})
	}

	apiSrv := listFailureServer()
	rr := doGET(t, apiSrv.Handler(), "/api/findings.json")
	if rr.Code != http.StatusBadGateway || strings.Contains(rr.Body.String(), "error-state") || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("API error shape changed with HTML recovery work: status=%d content-type=%q body=%s", rr.Code, rr.Header().Get("Content-Type"), rr.Body.String())
	}
}
