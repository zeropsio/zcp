package sync

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// Tests in this file use t.Setenv (CacheClear reads STRAPI_API_TOKEN), which
// forbids t.Parallel.

func TestCacheClear_ExplicitSlugsResolveToStrapi(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("STRAPI_API_TOKEN", "test-token")

	cfg := &Config{
		APIURL:    srv.URL + "/recipes",
		SlugRemap: map[string]string{"node-js-hello-world": "nodejs-hello-world"},
	}

	results, err := CacheClear(cfg, []string{
		"nodejs-hello-world",  // local slug → resolves to Strapi slug
		"bun-hello-world",     // no remap entry → identity
		"node-js-hello-world", // Strapi slug passed directly → identity
	})
	if err != nil {
		t.Fatalf("CacheClear: %v", err)
	}

	wantPaths := []string{
		"POST /recipes/node-js-hello-world/cache/clear",
		"POST /recipes/bun-hello-world/cache/clear",
		"POST /recipes/node-js-hello-world/cache/clear",
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Errorf("request paths = %v, want %v", paths, wantPaths)
	}

	wantSlugs := []string{"node-js-hello-world", "bun-hello-world", "node-js-hello-world"}
	for i, r := range results {
		if r.Slug != wantSlugs[i] {
			t.Errorf("results[%d].Slug = %q, want %q", i, r.Slug, wantSlugs[i])
		}
		if r.Err != nil {
			t.Errorf("results[%d].Err = %v, want nil", i, r.Err)
		}
		if r.Status != http.StatusOK {
			t.Errorf("results[%d].Status = %d, want 200", i, r.Status)
		}
	}
}

func TestCacheClear_NoArgsClearsFetchedSlugsUnremapped(t *testing.T) {
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":[{"slug":"node-js-hello-world"},{"slug":"nodejs-hello-world"}]}`))
			return
		}
		posts = append(posts, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("STRAPI_API_TOKEN", "test-token")

	cfg := &Config{
		APIURL:    srv.URL + "/recipes",
		SlugRemap: map[string]string{"node-js-hello-world": "nodejs-hello-world"},
	}

	results, err := CacheClear(cfg, nil)
	if err != nil {
		t.Fatalf("CacheClear: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// Fetched slugs are Strapi-side already and must pass through verbatim.
	// The second fixture slug equals a remap VALUE — a wrong implementation
	// that runs fetched slugs through the reverse remap would rewrite it.
	wantPosts := []string{
		"/recipes/node-js-hello-world/cache/clear",
		"/recipes/nodejs-hello-world/cache/clear",
	}
	if !reflect.DeepEqual(posts, wantPosts) {
		t.Errorf("POST paths = %v, want %v", posts, wantPosts)
	}
}

func TestCacheClear_HTTPErrorSurfacesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("STRAPI_API_TOKEN", "test-token")

	cfg := &Config{APIURL: srv.URL + "/recipes"}

	results, err := CacheClear(cfg, []string{"ghost"})
	if err != nil {
		t.Fatalf("CacheClear: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Status != http.StatusNotFound {
		t.Errorf("Status = %d, want 404", results[0].Status)
	}
	if results[0].Err == nil {
		t.Error("Err = nil, want HTTP 404 error")
	}
}
