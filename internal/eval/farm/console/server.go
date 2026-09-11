package console

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
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

//go:embed assets/style.css
var assetsFS embed.FS

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
	// ObserverClaudePathUnresolved is true when --claude could not be
	// resolved (exec.LookPath + filepath.Abs) at startup (§8.5): the
	// actions answer 503 "observer unavailable" instead of enqueueing.
	ObserverClaudePathUnresolved bool
}

// Server is the farm console's HTTP server (docs/spec-eval-farm.md §8).
type Server struct {
	cfg    Config
	files  *fileCache
	static http.Handler
}

// NewServer builds a Server from cfg.
func NewServer(cfg Config) *Server {
	staticFS, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(fmt.Sprintf("console: bad embedded assets: %v", err)) // unreachable: assets/ is compiled in
	}
	return &Server{
		cfg:    cfg,
		files:  newFileCache(),
		static: http.StripPrefix("/static/", http.FileServerFS(staticFS)),
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
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/static/"):
		s.static.ServeHTTP(w, r)
	case r.Method == http.MethodGet && p == "/":
		s.requireAuth(s.handleRoot)(w, r)
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
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// handleRoot is the S3 placeholder for §8.3 FM-51's "/" page (the full
// page — id/created/candidate sha/set/counts/cost/observed n/m — is S4):
// a plain list of links to every batch, newest-id-first.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	ids, err := listBatchIDs(r.Context(), s.cfg.Store)
	if err != nil {
		http.Error(w, "list batches: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, "<!doctype html><html><head><meta charset=\"utf-8\"><title>Farm console</title>"+
		"<link rel=\"stylesheet\" href=\"/static/style.css\"></head><body><h1>Batches</h1><ul class=\"batches\">")
	for i := len(ids) - 1; i >= 0; i-- {
		fmt.Fprintf(w, "<li><a href=\"/api/runs.md?batch=%s\">%s</a></li>", template.HTMLEscapeString(ids[i]), template.HTMLEscapeString(ids[i]))
	}
	fmt.Fprint(w, "</ul></body></html>")
}

func renderLoginPage(w http.ResponseWriter, failed bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := loginTemplate.Execute(w, struct{ Failed bool }{failed}); err != nil {
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
type fileCache struct {
	mu      sync.Mutex
	entries map[string][]byte
}

func newFileCache() *fileCache {
	return &fileCache{entries: make(map[string][]byte)}
}

func (c *fileCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *fileCache) set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = data
}
