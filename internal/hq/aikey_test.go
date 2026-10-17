package hq

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// aiKeyValue is a made-up key, built from parts so no scanner takes it for one.
var aiKeyValue = strings.Join([]string{"fixture", "nebius", "0123456789"}, "-")

func TestClient_AIKey_HandsTheMateTheOrganizationsKey(t *testing.T) {
	t.Parallel()
	client, calls := answering(t, `{"provider":"nebius","env":"NEBIUS_API_KEY","value":"`+aiKeyValue+`","baseUrl":"https://api.tokenfactory.nebius.com/v1"}`)

	key, err := client.AIKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := AIKey{Provider: "nebius", Env: "NEBIUS_API_KEY", Value: aiKeyValue, BaseURL: "https://api.tokenfactory.nebius.com/v1"}
	if key != want {
		t.Errorf("key = %+v, want %+v", key, want)
	}
	wantCall := hqCall{http.MethodPost, "/api/mate/ai-key", "Mate good", "application/json", `{}`}
	if len(*calls) != 1 || (*calls)[0] != wantCall {
		t.Errorf("calls = %+v, want [%+v]", *calls, wantCall)
	}
}

func TestClient_AIKey_RefusalsAndIncompleteAnswersNeverCarryTheKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		wantNone bool
	}{
		{name: "the organization has no key", status: http.StatusNotFound, body: `{"code":"no_key"}`, wantNone: true},
		{name: "another refusal", status: http.StatusForbidden, body: `{"code":"forbidden"}`},
		{name: "an answer without its variable", status: http.StatusOK, body: `{"provider":"nebius","env":"","value":"` + aiKeyValue + `","baseUrl":"https://api.tokenfactory.nebius.com/v1"}`},
		{name: "an answer without a value", status: http.StatusOK, body: `{"provider":"nebius","env":"NEBIUS_API_KEY","value":"","baseUrl":"https://api.tokenfactory.nebius.com/v1"}`},
		{name: "an answer without its base URL", status: http.StatusOK, body: `{"provider":"nebius","env":"NEBIUS_API_KEY","value":"` + aiKeyValue + `","baseUrl":""}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)

			key, err := enrolledClient(t, srv).AIKey(context.Background())
			if err == nil {
				t.Fatalf("AIKey = %+v, want an error", key)
			}
			if got := errors.Is(err, ErrNoAIKey); got != tc.wantNone {
				t.Errorf("errors.Is(err, ErrNoAIKey) = %v, want %v (err %v)", got, tc.wantNone, err)
			}
			if strings.Contains(err.Error(), aiKeyValue) {
				t.Errorf("error carries the key: %v", err)
			}
			if key != (AIKey{}) {
				t.Errorf("key = %+v, want none", key)
			}
		})
	}
}
