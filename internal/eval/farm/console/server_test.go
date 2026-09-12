package console

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// FM-50: the pinned kit is one public embedded asset, never an open static tree.
func TestServer_TabularKitAsset_LocalAndRestricted(t *testing.T) {
	t.Parallel()
	srv, _, _ := testServer(t)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/static/vendor/tabler-1.5.1.min.css", 200},
		{http.MethodPost, "/static/vendor/tabler-1.5.1.min.css", 404},
		{http.MethodGet, "/static/vendor/", 404},
		{http.MethodGet, "/static/vendor/TABLER-LICENSE", 404},
		{http.MethodGet, "/static/vendor/../app.css", 404},
		{http.MethodGet, "/static/vendor/tabler-1.5.1.min.css.map", 404},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			t.Parallel()
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.status != http.StatusOK {
				req.Header.Set("Authorization", "Bearer "+testToken)
			}
			srv.Handler().ServeHTTP(rr, req)
			if rr.Code != tc.status {
				t.Fatalf("status = %d, want %d", rr.Code, tc.status)
			}
			const csp = "default-src 'none'; style-src 'self'; img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
			if rr.Header().Get("Content-Security-Policy") != csp || rr.Header().Get("Cache-Control") != "no-store" {
				t.Error("static response changed the security boundary")
			}
			if tc.status != 200 {
				return
			}
			if rr.Header().Get("Content-Type") != "text/css; charset=utf-8" {
				t.Error("kit response is not CSS")
			}
			const digest = "6aa5677e9cfc2620405bf97a98074ba43ff06a411cb35c5337acfb56d124c273"
			if got := fmt.Sprintf("%x", sha256.Sum256(rr.Body.Bytes())); got != digest {
				t.Errorf("kit sha256 = %s, want pinned Tabler 1.5.1 %s", got, digest)
			}
			css := rr.Body.String()
			if strings.Contains(css, "@import") {
				t.Error("kit must not import another stylesheet")
			}
			for _, ref := range regexp.MustCompile(`url\(([^)]+)\)`).FindAllStringSubmatch(css, -1) {
				if !strings.HasPrefix(strings.Trim(ref[1], `"' `), "data:") {
					t.Errorf("kit has non-data asset reference %q", ref[1])
				}
			}
		})
	}
}
