package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestWatchKeyRefusal_AZeropsRefusalOfTheKeyAsksHQToRecheckIt: when Zerops
// answers 401 to the Mate's server, its client asks the Mate's HQ for a key
// recheck with the key in the Authorization header (TC-21, zcp half). The
// wait is the fake HQ's receipt, never a timer.
func TestWatchKeyRefusal_AZeropsRefusalOfTheKeyAsksHQToRecheckIt(t *testing.T) {
	t.Parallel()
	zerops := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"no"}}`))
	}))
	defer zerops.Close()
	type ask struct{ method, path, authorization string }
	asked := make(chan ask, 4)
	hqServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked <- ask{r.Method, r.URL.Path, r.Header.Get("Authorization")}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hqServer.Close()
	client, err := platform.NewZeropsClient("zerops-token", zerops.URL)
	if err != nil {
		t.Fatal(err)
	}
	told := make(chan string, 4)
	watchKeyRefusal(client, func() hq.Identity { return hq.Identity{Origin: hqServer.URL, Key: "project-key", ProjectID: "p1"} }, func(s string) { told <- s })

	_, _ = client.GetUserInfo(context.Background())

	if got, want := <-asked, (ask{http.MethodPost, "/api/mate/key-recheck", "Project project-key"}); got != want {
		t.Errorf("HQ was asked %+v, want %+v", got, want)
	}
	if line := <-told; line == "" {
		t.Error("HQ's answer was not reported")
	}
}
