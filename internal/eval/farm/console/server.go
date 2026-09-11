package console

import (
	"context"
	_ "embed" // assets/login.html, assets/app.css
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

//go:embed assets/login.html
var loginHTMLSrc string

var loginTemplate = template.Must(template.New("login").Parse(loginHTMLSrc))

// appCSS is the console's one stylesheet, shared by the login page and
// every page (§8.2: served from the console itself, no external asset).
//
//go:embed assets/app.css
var appCSS []byte

// Config configures a Server.
type Config struct {
	// Store is the farm bucket, read-only from the console's point of view
	// (a *farm.SinkClient in production). Also passed to observer.Store /
	// observer.NewSinkBundle, which both restrict themselves to
	// runs/<runId>/observer/ and runs/<runId>/* reads respectively — the
	// console itself issues no Put.
	Store observer.ObjectStore
	// Token is the console's bearer/login token (FM-50).
	Token string
	// Now returns the current time; nil defaults to time.Now (tests inject
	// a fixed clock).
	Now func() time.Time
	// ObserverDisabled is ZCP_FARM_OBSERVER=off (§8.1, §8.5): every run
	// reads observerState "observer disabled" regardless of the manifest.
	ObserverDisabled bool
	// LoginFailDelay overrides the production 1s floor on a failed
	// login/bearer attempt (FM-50) — tests set a small value so the delay
	// mechanism is exercised without a real sleep. Zero uses the
	// production default.
	LoginFailDelay time.Duration
	// Sleep overrides time.Sleep for the login/bearer failure delay (tests
	// record the call instead of actually blocking). Nil uses time.Sleep.
	Sleep func(time.Duration)
	// Queue is the console's observation queue (§8.5): the run/batch observe
	// actions (actions.go) enqueue into it and read busy/queued state from
	// it via Queue.State/Queue.BatchBusy, and view.go's read model reads
	// Queue.State to resolve the "observing" observerState. Nil only in a
	// test server that doesn't exercise the worker or actions.
	Queue *Queue
	// Worker discovers finished, unobserved runs on a schedule (§8.5);
	// Server.StartWorker (actions.go) runs it until its context is done.
	// Nil skips the background loop entirely — distinct from a Worker built
	// Disabled (e.g. a missing CLAUDE_CODE_OAUTH_TOKEN or an unresolvable
	// --claude path, both of which still build a Worker so Tick stays a
	// well-defined no-op rather than leaving this nil).
	Worker *Worker
	// WorkerInterval overrides the production 60s worker tick interval
	// (§8.5 FM-53) — tests set a small value. Zero uses the production
	// default.
	WorkerInterval time.Duration
	// ObserverCredentialMissing is true when CLAUDE_CODE_OAUTH_TOKEN was
	// empty at startup (§8.5): the run/batch observe actions answer 503
	// "observer credential missing" instead of enqueueing.
	ObserverCredentialMissing bool
	// ObserverAPIKeySet is true when ANTHROPIC_API_KEY was set at startup
	// (§8.5): treated exactly like a missing credential — the observer runs
	// only under the OAuth token — but named separately on the pages.
	ObserverAPIKeySet bool
	// ObserverClaudePathUnresolved is true when --claude could not be
	// resolved (exec.LookPath + filepath.Abs) at startup (§8.5): the
	// actions answer 503 "observer unavailable" instead of enqueueing.
	ObserverClaudePathUnresolved bool
}

// Server is the farm console's HTTP server (docs/spec-eval-farm.md §8).
type Server struct {
	cfg   Config
	files *fileCache

	// runCache/summaryCache are the run-row cache (fix brief
	// "farm-console-speed-2026-09-11", cache.go): every read-model call
	// below api.go/pages.go threads them through, nil-safe, so a warm
	// load answers from memory instead of the bucket.
	runCache     *runCache
	summaryCache *summaryCache

	// logf logs a batch or run skipped during a listing scan because its
	// manifest/observation/meta could not be read (view.go item 5) —
	// wired at construction (NewServer) rather than a package var, so
	// tests can set it directly on their own Server instance instead of
	// mutating shared global state. Threaded down into the free
	// read-model functions (view.go/cache.go/batches.go) that do the
	// actual logging, nil-safe there too.
	logf func(format string, args ...any)
}

// NewServer builds a Server from cfg. cfg.Store is wrapped in a manifest-
// caching decorator (item 7a) — a batch manifest is written once and never
// mutated (§1.1), so caching it in memory for the Server's lifetime is
// always safe and cuts a page load's repeat bucket calls. When cfg.Queue is
// set, its OnComplete hook is wired to the run cache's observation
// invalidation (cache.go rule 2a): a finished job's run is re-read next
// time, not merely once its TTL lapses.
func NewServer(cfg Config) *Server {
	if cfg.Store != nil {
		cfg.Store = newManifestCachingStore(cfg.Store)
	}
	s := &Server{
		cfg:   cfg,
		files: newFileCache(),
		logf:  defaultLogf,
	}
	s.runCache = newRunCache(s.now)
	s.summaryCache = newSummaryCache(s.now)
	if cfg.Queue != nil {
		cfg.Queue.OnComplete = s.runCache.invalidateObservation
	}
	return s
}

// WarmCache pre-fills the run-row cache for every batch the "/" page shows
// (fix brief item 6): called once in the background at server start
// (cmd/zcp/eval_farm_console.go). A request arriving during warm-up runs
// its own fill concurrently rather than waiting on this one — duplicate
// work is the accepted cost; a fill is a pure, idempotent function of the
// bucket's immutable state, so it never produces a partial or failed row.
func (s *Server) WarmCache(ctx context.Context) {
	if s.cfg.Store == nil {
		return
	}
	if _, err := loadBatchRows(ctx, s.cfg.Store, s.cfg.ObserverDisabled, s.queueState, s.runCache, s.summaryCache, s.logf); err != nil {
		s.logf("warm cache: %v", err)
	}
}

func (s *Server) now() time.Time {
	if s.cfg.Now != nil {
		return s.cfg.Now()
	}
	return time.Now()
}

// Handler returns the console's http.Handler: security headers on every
// response (FM-50), then routing.
//
// Routing is hand-rolled rather than http.ServeMux: ServeMux normalizes
// ("cleans") every request path — collapsing a "results/../done.json"
// segment — and 301-redirects when that changes the path, BEFORE any
// pattern match runs. That would turn a path-traversal probe into a
// redirect instead of the direct 404 FM-52's files/ boundary requires
// (TestAPI_FilesOnlyUnderResults), and there is no per-route way to opt a
// single ServeMux pattern out of it.
func (s *Server) Handler() http.Handler {
	return securityHeaders(http.HandlerFunc(s.route))
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && p == loginPath:
		s.handleLoginGET(w, r)
	case r.Method == http.MethodPost && p == loginPath:
		s.handleLoginPOST(w, r)
	case r.Method == http.MethodGet && p == "/healthz":
		handleHealthz(w, r)
	case r.Method == http.MethodGet && p == "/static/app.css":
		serveAppCSS(w)
	case r.Method == http.MethodGet && p == "/":
		s.requireAuth(s.handleRoot)(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/b/"):
		s.requireAuth(s.handleBatchPage)(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/r/"):
		s.requireAuth(s.handleRunPage)(w, r)
	case r.Method == http.MethodGet && p == "/findings":
		s.requireAuth(s.handleFindingsPage)(w, r)
	case r.Method == http.MethodGet && p == "/terms":
		s.requireAuth(s.handleTermsPage)(w, r)
	case r.Method == http.MethodPost && p == "/logout":
		s.requireAuth(s.handleLogout)(w, r)
	case r.Method == http.MethodGet && (p == "/api/runs.md" || p == "/api/runs.json"):
		s.requireAuth(s.handleRunsList)(w, r)
	case r.Method == http.MethodGet && (p == "/api/findings.md" || p == "/api/findings.json"):
		s.requireAuth(s.handleFindings)(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/api/runs/"):
		s.requireAuth(s.handleRunsSubroute)(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/r/") && strings.HasSuffix(p, "/observe"):
		s.requireAuth(s.handleRunObserve)(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/b/") && strings.HasSuffix(p, "/observe"):
		s.requireAuth(s.handleBatchObserve)(w, r)
	default:
		http.NotFound(w, r)
	}
}

// securityHeaders wraps next so every response — including a 401/404 the
// inner handler produces — carries FM-50's exact header set.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'self'; img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// serveAppCSS answers GET /static/app.css, the one open static route (§8.2).
func serveAppCSS(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write(appCSS)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// handleRoot is §8.3 FM-51's "/" page: every batch, newest first, with its
// id, created time, candidate sha (12 chars), set, per-verdict counts,
// total cost and observed-runs n/m (batches.go's read model).
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	s.handleBatchesPage(w, r)
}

func renderLoginPage(w http.ResponseWriter, failed bool, next string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := loginTemplate.Execute(w, struct {
		Failed bool
		Next   string
	}{failed, next}); err != nil {
		http.Error(w, "template: "+err.Error(), http.StatusInternalServerError)
	}
}

// writeJSON encodes v as the response body with a 200 status — every
// console API handler returns either this or an error status written
// before any body starts (writeStoreError, api.go), so 200 is the only
// status this ever needs to write.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The header and part of the body may already be on the wire —
		// there is nothing left to do but log; the client sees a truncated
		// response, which is exactly what a genuine encode failure is.
		fmt.Fprintf(os.Stderr, "console: encode JSON response: %v\n", err)
	}
}

// fileCache is the immutable-object cache for GET
// /api/runs/<runId>/files/<path> (view.go's read-model notes): only ever
// populated for a run whose done.json exists — nothing under
// runs/<runId>/{started.json, done.json, results/, capture/} is written
// after done.json (§7.6 FM-47), so a cached entry never goes stale.
// The cache holds at most budget bytes; past it the oldest entries go first,
// so a long-lived console never grows without limit.
type fileCache struct {
	mu      sync.Mutex
	entries map[string][]byte
	order   []string
	size    int
	budget  int
}

// fileCacheBudget bounds the files/ passthrough cache.
const fileCacheBudget = 64 << 20

func newFileCache() *fileCache { return newFileCacheWithBudget(fileCacheBudget) }

func newFileCacheWithBudget(budget int) *fileCache {
	return &fileCache{entries: make(map[string][]byte), budget: budget}
}

func (c *fileCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

// manifestCachingStore wraps an observer.ObjectStore, caching a
// batches/<batch>/manifest.json body in memory once a Get for it
// succeeds (item 7a) — a manifest is written once and never mutated
// (§1.1), so a repeat read within the Server's lifetime is always safe to
// serve from memory. Head reports cached-as-existing without a store call
// too; a manifest key that has never been successfully read (including a
// batch that doesn't exist yet) always falls through to the real store, so
// a not-yet-created batch is never cached as missing.
type manifestCachingStore struct {
	observer.ObjectStore
	mu    sync.Mutex
	cache map[string][]byte
}

func newManifestCachingStore(inner observer.ObjectStore) *manifestCachingStore {
	return &manifestCachingStore{ObjectStore: inner, cache: make(map[string][]byte)}
}

// isManifestKey reports whether key is a batches/<batch>/manifest.json
// object key (manifestKey, view.go) — the only key this decorator caches.
func isManifestKey(key string) bool {
	return strings.HasPrefix(key, "batches/") && strings.HasSuffix(key, "/manifest.json")
}

func (c *manifestCachingStore) Head(ctx context.Context, key string) (bool, int64, error) {
	if isManifestKey(key) {
		c.mu.Lock()
		body, ok := c.cache[key]
		c.mu.Unlock()
		if ok {
			return true, int64(len(body)), nil
		}
	}
	return c.ObjectStore.Head(ctx, key)
}

func (c *manifestCachingStore) Get(ctx context.Context, key string) ([]byte, error) {
	if isManifestKey(key) {
		c.mu.Lock()
		body, ok := c.cache[key]
		c.mu.Unlock()
		if ok {
			return body, nil
		}
	}
	body, err := c.ObjectStore.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("console: manifest cache: get %s: %w", key, err)
	}
	if isManifestKey(key) {
		c.mu.Lock()
		c.cache[key] = body
		c.mu.Unlock()
	}
	return body, nil
}

func (c *fileCache) set(key string, data []byte) {
	if len(data) > c.budget {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.entries[key]; ok {
		c.size -= len(old)
	} else {
		c.order = append(c.order, key)
	}
	c.entries[key] = data
	c.size += len(data)
	for c.size > c.budget && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		c.size -= len(c.entries[oldest])
		delete(c.entries, oldest)
	}
}
