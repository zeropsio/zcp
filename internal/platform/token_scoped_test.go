package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Tests for internal/platform/token_scoped.go
// (docs/spec-eval-farm.md §2.1 FM-10, plans/zcp-test-farm-2026-09-10-briefs/S4b-run-token.md).

const (
	scopedMintTestClientID  = "client-scoped"
	scopedMintTestTokenID   = "tok-scoped"
	scopedMintTestProjectID = "proj-scoped-1"
)

func scopedMintPath(clientID string) string {
	return "/api/rest/public/client/" + clientID + "/integration-token"
}

func newScopedMintTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *ZeropsClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/rest/public/user/info" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(userInfoJSON(scopedMintTestTokenID, scopedMintTestClientID)))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	return z
}

// TestMintProjectScopedToken_SendsNoAccessWithProjectAdmin pins the exact
// outbound POST body (brief §"Token mint body"): NO_ACCESS account role,
// finance flags off, canCreateProjects false, and a single projects entry
// scoping ADMIN to the given project. Response id+token map onto MintedToken.
func TestMintProjectScopedToken_SendsNoAccessWithProjectAdmin(t *testing.T) {
	t.Parallel()

	var rawBody []byte
	z := newScopedMintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != scopedMintPath(scopedMintTestClientID) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var readErr error
		rawBody, readErr = io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("read mint body: %v", readErr)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"run-tok-1","token":"run-secret-value"}`))
	})

	got, err := z.MintProjectScopedToken(context.Background(), scopedMintTestClientID, scopedMintTestProjectID, "farm-run-abc")
	if err != nil {
		t.Fatalf("MintProjectScopedToken: %v", err)
	}
	want := MintedToken{Token: "run-secret-value", TokenID: "run-tok-1"}
	if got != want {
		t.Errorf("MintProjectScopedToken = %+v, want %+v", got, want)
	}

	if len(rawBody) == 0 {
		t.Fatal("mint request body was never captured")
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &body); err != nil {
		t.Fatalf("unmarshal mint body %q: %v", rawBody, err)
	}
	assertField := func(key, want string) {
		t.Helper()
		raw, ok := body[key]
		if !ok {
			t.Errorf("mint body must carry %q explicitly; body = %s", key, rawBody)
			return
		}
		if got := strings.TrimSpace(string(raw)); got != want {
			t.Errorf("mint body %q = %s, want %s", key, got, want)
		}
	}
	assertField("name", `"farm-run-abc"`)
	assertField("roleCode", `"NO_ACCESS"`)
	assertField("canCreateProjects", "false")
	assertField("canViewFinances", "false")
	assertField("canEditFinances", "false")

	raw, ok := body["projects"]
	if !ok {
		t.Fatalf("mint body must carry projects explicitly; body = %s", rawBody)
	}
	var projects []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &projects); err != nil {
		t.Fatalf("unmarshal projects %q: %v", raw, err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %d entries, want 1: %s", len(projects), raw)
	}
	if got := strings.TrimSpace(string(projects[0]["projectId"])); got != `"`+scopedMintTestProjectID+`"` {
		t.Errorf("projects[0].projectId = %s, want %q", got, scopedMintTestProjectID)
	}
	if got := strings.TrimSpace(string(projects[0]["roleCode"])); got != `"ADMIN"` {
		t.Errorf("projects[0].roleCode = %s, want \"ADMIN\"", got)
	}
}

// TestMintProjectScopedToken_IntegrationToken403_TypedError pins both 403
// apiCodes (current + legacy) the platform returns when the minting
// credential is itself an integration token without delegation — mapped to
// ErrDelegationUnavailable, the same typed code MintDelegatedLaunchToken
// already uses for the identical platform-level restriction.
func TestMintProjectScopedToken_IntegrationToken403_TypedError(t *testing.T) {
	t.Parallel()

	for _, code := range []string{apiCodeDelegationUnavailable, apiCodeDelegationUnavailableLegacy} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			z := newScopedMintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"forbidden"}}`))
			})

			_, err := z.MintProjectScopedToken(context.Background(), scopedMintTestClientID, scopedMintTestProjectID, "farm-run-abc")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var pe *PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("expected PlatformError, got %T: %v", err, err)
			}
			if pe.Code != ErrDelegationUnavailable {
				t.Errorf("Code = %q, want %q", pe.Code, ErrDelegationUnavailable)
			}
		})
	}
}
