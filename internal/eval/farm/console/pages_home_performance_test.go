package console

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHome_SnapshotPreservesProblemInputOrder(t *testing.T) {
	srv, store, _ := testServer(t)
	now := fixedNow(t)()
	for i, batch := range []string{"snapshot-a", "snapshot-b"} {
		seedBatchAt(t, store, batch, ObserverOff, now.Add(-time.Duration(2-i)*time.Hour), []runFixture{
			{runID: batch + "-z", scenario: "z", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1, done: true},
			{runID: batch + "-a", scenario: "a", startedAt: now.Add(-time.Hour), durationS: "5s", costUsd: 0.1, done: true},
		}, false, nil)
	}
	want, err := srv.allProblemsRuns(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadBatchSnapshot(t.Context(), srv.cfg.Store, srv.cfg.ObserverDisabled, srv.queueState, srv.runCache, srv.summaryCache, srv.logf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.problemsRuns(), want) {
		t.Fatal("overview snapshot changed the full-history problem inputs or their tie-breaking order")
	}
}

// A warm overview must resolve each batch once even though its rows feed
// the table, latest/previous comparison, and full-history problem summary.
// Empty completed runs deliberately remain retryable; that must not bypass
// the independent observation TTL or multiply their reads by panel count.
func TestHome_WarmRequestReadsEachBatchOnce(t *testing.T) {
	for _, path := range []string{"/", "/?kind=all"} {
		t.Run(path, func(t *testing.T) {
			srv, store, _ := testServer(t)
			now := fixedNow(t)()
			for i := range 5 {
				batch := fmt.Sprintf("warm%d", i)
				runID := batch + "-alpha"
				seedBatchAt(t, store, batch, ObserverOff, now.Add(-time.Duration(i+1)*time.Hour), []runFixture{{
					runID: runID, scenario: "alpha", startedAt: now.Add(-time.Duration(i+1) * time.Hour),
					durationS: "5s", costUsd: 0.1, taskResult: "passed", done: i < 3,
				}}, true, map[string]string{runID: "passed"})
				if i >= 3 {
					store.putText(t, doneKey(runID), `{}`)
				}
			}
			if rr := doGET(t, srv.Handler(), path); rr.Code != http.StatusOK {
				t.Fatalf("warm-up status = %d", rr.Code)
			}
			store.resetCallLog()
			rr := doGET(t, srv.Handler(), path)
			if rr.Code != http.StatusOK {
				t.Fatalf("warm status = %d", rr.Code)
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			t.Logf("warm storage calls: Get=%d Head=%d List=%d", len(store.gets), len(store.heads), len(store.lists))
			for i := range 5 {
				key := manifestKey(fmt.Sprintf("warm%d", i))
				for kind, calls := range map[string][]string{"Get": store.gets, "Head": store.heads} {
					count := 0
					for _, called := range calls {
						if called == key {
							count++
						}
					}
					if count != 0 {
						t.Errorf("%s(%s) = %d calls, want the warm manifest cache", kind, key, count)
					}
				}
			}
			batchLists := 0
			for _, prefix := range store.lists {
				if prefix == "batches/" {
					batchLists++
				}
				if strings.HasSuffix(prefix, "/observer/") {
					t.Errorf("warm request refreshed %s before the observation TTL", prefix)
				}
			}
			if batchLists != 1 {
				t.Errorf("List(batches/) = %d calls, want one shared snapshot", batchLists)
			}
			for _, runID := range []string{"warm3-alpha", "warm4-alpha"} {
				count := 0
				for _, key := range store.heads {
					if key == doneKey(runID) {
						count++
					}
				}
				if count != 1 {
					t.Errorf("retryable run %s resolved %d times, want once per overview", runID, count)
				}
			}
		})
	}
}
