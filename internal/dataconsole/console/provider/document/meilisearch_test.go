package document

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// newFakeMeiliServer is a minimal double of the two Meilisearch endpoints
// putDoc drives: PUT .../documents (task enqueue) and GET /tasks/{taskUid}
// (task-status poll). statuses is the sequence of statuses successive polls
// return; the last entry repeats once exhausted, so a single-element slice
// models "always this status" (used by the timeout case). errMsg/errCode are
// only emitted once the returned status is "failed". pollCount, guarded by
// mu, lets a test assert the server was actually polled more than once.
func newFakeMeiliServer(t *testing.T, taskUID int64, statuses []string, errCode, errMsg string) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	pollCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/documents"):
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"taskUid":%d,"indexUid":"products","status":"enqueued","type":"documentAdditionOrUpdate"}`, taskUID)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/documents/"):
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"taskUid":%d,"indexUid":"products","status":"enqueued","type":"documentDeletion"}`, taskUID)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/indexes/") && !strings.Contains(r.URL.Path, "/documents"):
			// primaryKey lookup (GET /indexes/{uid}) — putDoc's identity guard
			// resolves it before every write.
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"uid":"products","primaryKey":"id"}`)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/tasks/"):
			mu.Lock()
			idx := pollCount
			if idx >= len(statuses) {
				idx = len(statuses) - 1
			}
			pollCount++
			mu.Unlock()
			status := statuses[idx]
			w.WriteHeader(http.StatusOK)
			if status == "failed" && errMsg != "" {
				fmt.Fprintf(w, `{"uid":%d,"status":%q,"error":{"message":%q,"code":%q}}`, taskUID, status, errMsg, errCode)
			} else {
				fmt.Fprintf(w, `{"uid":%d,"status":%q}`, taskUID, status)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &pollCount
}

func fastPollEngine(base string) *meiliEngine {
	return &meiliEngine{
		t: &transport{
			base:   base,
			client: http.DefaultClient,
			setAuth: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer test-key")
			},
		},
		pollInterval: 2 * time.Millisecond,
		pollTimeout:  40 * time.Millisecond,
	}
}

// TestPutDoc_TaskSucceeds_ReturnsNil pins the ordinary case: an immediately
// "succeeded" task must not error.
func TestPutDoc_TaskSucceeds_ReturnsNil(t *testing.T) {
	t.Parallel()
	srv, _ := newFakeMeiliServer(t, 1, []string{"succeeded"}, "", "")
	m := fastPollEngine(srv.URL)
	if err := m.putDoc(context.Background(), "products", "1", []byte(`{"id":1}`)); err != nil {
		t.Fatalf("putDoc(succeeded task) = %v, want nil", err)
	}
}

// TestPutDoc_TaskEventuallySucceeds_PollsUntilTerminal proves putDoc keeps
// polling through non-terminal statuses (enqueued/processing) rather than
// reporting success — or giving up — after a single check.
func TestPutDoc_TaskEventuallySucceeds_PollsUntilTerminal(t *testing.T) {
	t.Parallel()
	srv, pollCount := newFakeMeiliServer(t, 7, []string{"enqueued", "processing", "processing", "succeeded"}, "", "")
	m := fastPollEngine(srv.URL)
	if err := m.putDoc(context.Background(), "products", "1", []byte(`{"id":1}`)); err != nil {
		t.Fatalf("putDoc(eventually succeeded) = %v, want nil", err)
	}
	if *pollCount < 4 {
		t.Fatalf("pollCount = %d, want at least 4 (proves it actually polled through the non-terminal statuses)", *pollCount)
	}
}

// TestPutDoc_TaskFails_ReturnsTypedErrorWithSanitizedReason pins DOC-AUD-01's
// core reproduction: a document that fails Meilisearch's async per-document
// validation (e.g. missing primary key) used to report {"ok":true} because
// putDoc only checked the enqueue response, never the task's real outcome
// (plans/dataconsole-audit/document.md DOC-AUD-01). The fix must surface a
// typed, non-nil error whose text carries the sanitized failure reason (for
// the stderr diagnostic sink — the client-facing envelope stays the generic
// per-sentinel message, unaffected by this slice).
func TestPutDoc_TaskFails_ReturnsTypedErrorWithSanitizedReason(t *testing.T) {
	t.Parallel()
	srv, _ := newFakeMeiliServer(t, 3, []string{"failed"}, "missing_document_id", "Document doesn't have a primary key attribute")
	m := fastPollEngine(srv.URL)
	err := m.putDoc(context.Background(), "products", "audit_big1", []byte(`{"title":"no id here"}`))
	if err == nil {
		t.Fatal("putDoc(failed task) = nil, want a non-nil error — this is exactly the audit's silent-failure bug")
	}
	if !errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("putDoc(failed task) = %v, want errors.Is(_, provider.ErrInvalid)", err)
	}
	if !strings.Contains(err.Error(), "missing_document_id") {
		t.Fatalf("putDoc(failed task) error %q does not carry meili's failure reason", err.Error())
	}
}

// TestPutDoc_TaskTimeout_ReturnsAcceptedNotConfirmedError pins the second half
// of DOC-AUD-01's fix: a task still processing when the poll bound elapses
// must not report success either — it is genuinely unknown whether the write
// will apply, so it gets its own distinct "accepted, not confirmed" error
// rather than being folded into the "failed" case.
func TestPutDoc_TaskTimeout_ReturnsAcceptedNotConfirmedError(t *testing.T) {
	t.Parallel()
	srv, pollCount := newFakeMeiliServer(t, 9, []string{"processing"}, "", "") // never terminal
	m := fastPollEngine(srv.URL)
	start := time.Now()
	err := m.putDoc(context.Background(), "products", "1", []byte(`{"id":1}`))
	elapsed := time.Since(start)
	if !errors.Is(err, provider.ErrTimeout) {
		t.Fatalf("putDoc(never-terminal task) = %v, want errors.Is(_, provider.ErrTimeout)", err)
	}
	if errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("putDoc(timeout) must not also classify as ErrInvalid (it is not a rejection): %v", err)
	}
	if elapsed < m.pollTimeout {
		t.Fatalf("putDoc returned after %s, want at least the pollTimeout bound %s", elapsed, m.pollTimeout)
	}
	if *pollCount < 2 {
		t.Fatalf("pollCount = %d, want at least 2 (proves it actually waited/polled, not a fast bail-out)", *pollCount)
	}
}

// TestPutDoc_EnqueueTransportError_PropagatesWithoutPolling proves a failure
// on the enqueue call itself (before any task exists) is returned as-is,
// never masked into a false success.
func TestPutDoc_EnqueueTransportError_PropagatesWithoutPolling(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The primaryKey lookup (identity guard) succeeds; the enqueue PUT itself
		// is rejected — the error the test asserts propagates without polling.
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/indexes/") && !strings.Contains(r.URL.Path, "/documents") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"uid":"products","primaryKey":"id"}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	m := fastPollEngine(srv.URL)
	err := m.putDoc(context.Background(), "products", "1", []byte(`{"id":1}`))
	if err == nil {
		t.Fatal("putDoc(enqueue rejected) = nil, want an error")
	}
	if errors.Is(err, provider.ErrTimeout) {
		t.Fatalf("putDoc(enqueue rejected) misclassified as ErrTimeout: %v", err)
	}
}

// TestDeleteDoc_TaskSucceeds_ReturnsNil pins the ordinary case: an immediately
// "succeeded" deletion task must not error.
func TestDeleteDoc_TaskSucceeds_ReturnsNil(t *testing.T) {
	t.Parallel()
	srv, _ := newFakeMeiliServer(t, 2, []string{"succeeded"}, "", "")
	m := fastPollEngine(srv.URL)
	if err := m.deleteDoc(context.Background(), "products", "1"); err != nil {
		t.Fatalf("deleteDoc(succeeded task) = %v, want nil", err)
	}
}

// TestDeleteDoc_TaskTimeout_ReturnsAcceptedNotConfirmedError pins A8's core
// reproduction: deleteDoc used to return success as soon as the HTTP DELETE
// was accepted (a bare 2xx from Meilisearch's async delete endpoint means only
// "enqueued", never "applied"), so the UI could show "Deleted." while the
// document still existed — a success-lie (spec-dataconsole.md §7.1 I-1). A
// deletion task still non-terminal once the poll bound elapses must surface
// the same "accepted, not confirmed" error putDoc uses, never a false success.
func TestDeleteDoc_TaskTimeout_ReturnsAcceptedNotConfirmedError(t *testing.T) {
	t.Parallel()
	srv, pollCount := newFakeMeiliServer(t, 8, []string{"processing"}, "", "") // never terminal
	m := fastPollEngine(srv.URL)
	start := time.Now()
	err := m.deleteDoc(context.Background(), "products", "1")
	elapsed := time.Since(start)
	if !errors.Is(err, provider.ErrTimeout) {
		t.Fatalf("deleteDoc(never-terminal task) = %v, want errors.Is(_, provider.ErrTimeout) — this is exactly the delete success-lie bug", err)
	}
	if elapsed < m.pollTimeout {
		t.Fatalf("deleteDoc returned after %s, want at least the pollTimeout bound %s", elapsed, m.pollTimeout)
	}
	if *pollCount < 2 {
		t.Fatalf("pollCount = %d, want at least 2 (proves it actually waited/polled, not a fast bail-out)", *pollCount)
	}
}

// TestDeleteDoc_TransportError_PropagatesWithoutPolling proves a failure on
// the delete call itself (before any task exists) is returned as-is, never
// masked into a false success.
func TestDeleteDoc_TransportError_PropagatesWithoutPolling(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	m := fastPollEngine(srv.URL)
	err := m.deleteDoc(context.Background(), "products", "1")
	if err == nil {
		t.Fatal("deleteDoc(delete rejected) = nil, want an error")
	}
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("deleteDoc(delete rejected) = %v, want errors.Is(_, provider.ErrNotFound)", err)
	}
}
