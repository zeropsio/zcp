package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

// Tests for: internal/platform/zerops_delegation.go (§3 of
// plans/token-delegation-implementation-spec-2026-07-10.md).

// ---------------------------------------------------------------------------
// DTO mapping (§3.1 — table-driven)
// ---------------------------------------------------------------------------

func TestMapTokenDelegations_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []tokenDelegationDTO
		want []TokenDelegation
	}{
		{
			name: "nil input maps to empty (never nil) slice",
			in:   nil,
			want: []TokenDelegation{},
		},
		{
			name: "single row — live F2 sample shape",
			in: []tokenDelegationDTO{
				{
					ID:      "srdstsF6QM6J72yUMhDRJA",
					TokenID: "3U4vJrDsRvKrAIwBWAw32A",
					TokenPermissions: tokenDelegationPermissionsDTO{
						RoleCode:          "NO_ACCESS",
						CanCreateProjects: true,
					},
					Created: "2026-07-10T08:54:17Z",
				},
			},
			want: []TokenDelegation{
				{
					ID:                "srdstsF6QM6J72yUMhDRJA",
					TokenID:           "3U4vJrDsRvKrAIwBWAw32A",
					RoleCode:          "NO_ACCESS",
					CanCreateProjects: true,
					Created:           "2026-07-10T08:54:17Z",
				},
			},
		},
		{
			name: "CanCreateProjects false preserved, not defaulted",
			in: []tokenDelegationDTO{
				{
					ID:      "d2",
					TokenID: "t2",
					TokenPermissions: tokenDelegationPermissionsDTO{
						RoleCode:          "ADMIN",
						CanCreateProjects: false,
					},
				},
			},
			want: []TokenDelegation{
				{ID: "d2", TokenID: "t2", RoleCode: "ADMIN", CanCreateProjects: false},
			},
		},
		{
			name: "multiple rows preserve order",
			in: []tokenDelegationDTO{
				{ID: "a", TokenID: "ta"},
				{ID: "b", TokenID: "tb"},
			},
			want: []TokenDelegation{
				{ID: "a", TokenID: "ta"},
				{ID: "b", TokenID: "tb"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mapTokenDelegations(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mapTokenDelegations(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Error mapping (§3.3)
// ---------------------------------------------------------------------------

func TestTranslateDelegationUnavailable_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		apiCode  string
		wantCode string
	}{
		{"current platform code translates", apiCodeDelegationUnavailable, ErrDelegationUnavailable},
		{"legacy pre-delegation platform code translates", apiCodeDelegationUnavailableLegacy, ErrDelegationUnavailable},
		{"unrelated 403 code passes through untouched (F5 roleLevelExceeded)", "roleLevelExceeded", ErrPermissionDenied},
		{"empty api code passes through untouched", "", ErrPermissionDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := withAPICode(NewPlatformError(ErrPermissionDenied, "denied by platform", "check permissions"), tt.apiCode, nil)
			got := translateDelegationUnavailable(fmt.Errorf("mint: %w", in))

			var pe *PlatformError
			if !errors.As(got, &pe) {
				t.Fatalf("translateDelegationUnavailable returned non-PlatformError: %v", got)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", pe.Code, tt.wantCode)
			}
			if pe.Message != "denied by platform" {
				t.Errorf("Message = %q, want original message preserved", pe.Message)
			}
		})
	}
}

func TestTranslateDelegationUnavailable_PreservesAPIMeta(t *testing.T) {
	t.Parallel()
	meta := []APIMetaItem{{Code: "x", Error: "y"}}
	in := withAPICode(NewPlatformError(ErrPermissionDenied, "denied", ""), apiCodeDelegationUnavailable, meta)
	got := translateDelegationUnavailable(in)

	var pe *PlatformError
	if !errors.As(got, &pe) {
		t.Fatalf("expected PlatformError, got %T", got)
	}
	if !reflect.DeepEqual(pe.APIMeta, meta) {
		t.Errorf("APIMeta = %+v, want %+v", pe.APIMeta, meta)
	}
}

func TestTranslateDelegationUnavailable_NonPlatformErrorPassesThrough(t *testing.T) {
	t.Parallel()
	in := errors.New("transport failure")
	got := translateDelegationUnavailable(in)
	if got != in {
		t.Errorf("non-PlatformError input should pass through unchanged, got %v", got)
	}
}

// TestApiCodeDelegationUnavailable_Constants pins the wire-error code
// values (F4) — both the current and the legacy pre-delegation platform
// response. Drift here must update the spec doc.
func TestApiCodeDelegationUnavailable_Constants(t *testing.T) {
	t.Parallel()
	if apiCodeDelegationUnavailable != "notAllowedForIntegrationTokenWithoutDelegation" {
		t.Errorf("apiCodeDelegationUnavailable drifted from spec: got %q", apiCodeDelegationUnavailable)
	}
	if apiCodeDelegationUnavailableLegacy != "notAllowedForIntegrationToken" {
		t.Errorf("apiCodeDelegationUnavailableLegacy drifted from spec: got %q", apiCodeDelegationUnavailableLegacy)
	}
}

// ---------------------------------------------------------------------------
// getTokenID caching (mirrors getClientID's mutex/retry-on-error pattern)
// ---------------------------------------------------------------------------

// userInfoJSON builds a minimal, validly-decodable /user/info response body.
// output.UserAuthorize is a plain struct with no custom UnmarshalJSON, so
// fields omitted here simply zero out — only the fields ZCP actually reads
// (id, email, fullName, clientUserList[].clientId) need real values.
func userInfoJSON(userID, clientID string) string {
	return fmt.Sprintf(
		`{"id":%q,"email":"a@b.com","fullName":"ZCP","clientUserList":[{"id":"cu1","clientId":%q,"userId":%q}]}`,
		userID, clientID, userID,
	)
}

func TestGetTokenID_ColdCacheFetchesAndCaches(t *testing.T) {
	t.Parallel()

	var userInfoCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/rest/public/user/info" {
			atomic.AddInt32(&userInfoCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(userInfoJSON("user-tok-123", "client-abc")))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}

	got, err := z.getTokenID(context.Background())
	if err != nil {
		t.Fatalf("getTokenID (cold): %v", err)
	}
	if got != "user-tok-123" {
		t.Errorf("getTokenID = %q, want %q", got, "user-tok-123")
	}

	// Second call must be served from cache — no second /user/info round-trip.
	got2, err := z.getTokenID(context.Background())
	if err != nil {
		t.Fatalf("getTokenID (warm): %v", err)
	}
	if got2 != "user-tok-123" {
		t.Errorf("getTokenID (warm) = %q, want %q", got2, "user-tok-123")
	}
	if calls := atomic.LoadInt32(&userInfoCalls); calls != 1 {
		t.Errorf("GetUserInfo called %d times, want 1 (second getTokenID call should hit the cache)", calls)
	}
}

func TestGetTokenID_FailureNotCached(t *testing.T) {
	t.Parallel()

	var userInfoCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/rest/public/user/info" {
			n := atomic.AddInt32(&userInfoCalls, 1)
			if n == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(userInfoJSON("user-tok-456", "client-abc")))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}

	if _, err := z.getTokenID(context.Background()); err == nil {
		t.Fatal("expected error on first (failing) getTokenID call, got nil")
	}

	got, err := z.getTokenID(context.Background())
	if err != nil {
		t.Fatalf("getTokenID (retry after failure): %v", err)
	}
	if got != "user-tok-456" {
		t.Errorf("getTokenID = %q, want %q", got, "user-tok-456")
	}
}

// ---------------------------------------------------------------------------
// ListOwnTokenDelegations (§3.2 — hand-rolled GET)
// ---------------------------------------------------------------------------

func delegationListPath(clientID, tokenID string) string {
	return "/api/rest/public/client/" + clientID + "/integration-token/" + tokenID + "/delegation"
}

func newDelegationTestServer(t *testing.T, clientID, tokenID string, handler func(w http.ResponseWriter, r *http.Request)) *ZeropsClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/rest/public/user/info":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(userInfoJSON(tokenID, clientID)))
		case delegationListPath(clientID, tokenID):
			handler(w, r)
		default:
			handler(w, r) // let mint tests reuse this for the integration-token POST path too
		}
	}))
	t.Cleanup(srv.Close)

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	return z
}

func TestListOwnTokenDelegations_WellFormed(t *testing.T) {
	t.Parallel()
	const clientID = "client-abc"
	const tokenID = "tok-xyz"

	z := newDelegationTestServer(t, clientID, tokenID, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != delegationListPath(clientID, tokenID) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"list":[{"id":"srdstsF6QM6J72yUMhDRJA","clientId":"BkC8AGjFQMyFrLbzjHoE9g",
			"clientUserId":"WeZQLcWnSJiQexQoZuwa5g","userId":"SbbWs0jmQyeElIA0T9qUxw",
			"tokenId":"3U4vJrDsRvKrAIwBWAw32A",
			"tokenPermissions":{"roleCode":"NO_ACCESS","canCreateProjects":true,
			  "canEditFinances":false,"canViewFinances":false,"projectPermissions":[]},
			"created":"2026-07-10T08:54:17Z","lastUpdate":"2026-07-10T08:54:17Z"}]}`))
	})

	got, err := z.ListOwnTokenDelegations(context.Background())
	if err != nil {
		t.Fatalf("ListOwnTokenDelegations: %v", err)
	}
	want := []TokenDelegation{
		{
			ID:                "srdstsF6QM6J72yUMhDRJA",
			TokenID:           "3U4vJrDsRvKrAIwBWAw32A",
			RoleCode:          "NO_ACCESS",
			CanCreateProjects: true,
			Created:           "2026-07-10T08:54:17Z",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListOwnTokenDelegations = %+v, want %+v", got, want)
	}
}

func TestListOwnTokenDelegations_EmptyList(t *testing.T) {
	t.Parallel()
	const clientID = "client-abc"
	const tokenID = "tok-xyz"

	z := newDelegationTestServer(t, clientID, tokenID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"list":[]}`))
	})

	got, err := z.ListOwnTokenDelegations(context.Background())
	if err != nil {
		t.Fatalf("ListOwnTokenDelegations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListOwnTokenDelegations = %+v, want empty", got)
	}
}

func TestListOwnTokenDelegations_APIErrorMapped(t *testing.T) {
	t.Parallel()
	const clientID = "client-abc"
	const tokenID = "tok-xyz"

	z := newDelegationTestServer(t, clientID, tokenID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"someOtherCode","message":"forbidden"}}`))
	})

	_, err := z.ListOwnTokenDelegations(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != ErrPermissionDenied {
		t.Errorf("Code = %q, want %q (generic 403 mapping — list never needs delegation-specific translation)", pe.Code, ErrPermissionDenied)
	}
}

// ---------------------------------------------------------------------------
// MintDelegatedLaunchToken (§3.2/§3.3 — SDK-backed POST + apiCode translation)
// ---------------------------------------------------------------------------

func mintPath(clientID string) string {
	return "/api/rest/public/client/" + clientID + "/integration-token"
}

// mintTestClientID / mintTestTokenID are the fixed identity every mint test
// resolves via /user/info — hardcoded here (not parameters) since every call
// site uses the same values; kept as named consts so mintPath()/handler
// bodies stay readable at call sites.
const (
	mintTestClientID = "client-abc"
	mintTestTokenID  = "tok-xyz"
)

func newMintTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *ZeropsClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/rest/public/user/info" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(userInfoJSON(mintTestTokenID, mintTestClientID)))
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

func TestMintDelegatedLaunchToken_Success(t *testing.T) {
	t.Parallel()

	z := newMintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != mintPath(mintTestClientID) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"newtok123","name":"zcp-launch-test","roleCode":"NO_ACCESS","canCreateProjects":true,"token":"raw-secret-value"}`))
	})

	got, err := z.MintDelegatedLaunchToken(context.Background(), "zcp-launch-test")
	if err != nil {
		t.Fatalf("MintDelegatedLaunchToken: %v", err)
	}
	want := MintedToken{Token: "raw-secret-value", TokenID: "newtok123", Name: "zcp-launch-test"}
	if got != want {
		t.Errorf("MintDelegatedLaunchToken = %+v, want %+v", got, want)
	}
}

func TestMintDelegatedLaunchToken_NotAllowedWithoutDelegation_MapsToErrDelegationUnavailable(t *testing.T) {
	t.Parallel()

	z := newMintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"notAllowedForIntegrationTokenWithoutDelegation","message":"This action is not allowed for integration tokens without explicit delegation."}}`))
	})

	_, err := z.MintDelegatedLaunchToken(context.Background(), "zcp-launch-test")
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
}

func TestMintDelegatedLaunchToken_LegacyNotAllowedForIntegrationToken_MapsToErrDelegationUnavailable(t *testing.T) {
	t.Parallel()

	z := newMintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"notAllowedForIntegrationToken","message":"This action is not allowed for integration tokens."}}`))
	})

	_, err := z.MintDelegatedLaunchToken(context.Background(), "zcp-launch-test")
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
}

// TestMintDelegatedLaunchToken_RoleLevelExceeded_NotTranslated pins F5: a
// generic 403 unrelated to delegation availability (e.g. roleLevelExceeded)
// must NOT be swallowed into ErrDelegationUnavailable — ZCP never requests
// more than the delegated shape, so this needs no dedicated mapping.
func TestMintDelegatedLaunchToken_RoleLevelExceeded_NotTranslated(t *testing.T) {
	t.Parallel()

	z := newMintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"roleLevelExceeded","message":"exceeds delegated role"}}`))
	})

	_, err := z.MintDelegatedLaunchToken(context.Background(), "zcp-launch-test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != ErrPermissionDenied {
		t.Errorf("Code = %q, want %q (roleLevelExceeded must stay generic)", pe.Code, ErrPermissionDenied)
	}
}

// ---------------------------------------------------------------------------
// Mock (§3.4 — one-shot semantics + call tracking)
// ---------------------------------------------------------------------------

func TestMock_ListOwnTokenDelegations_DefaultEmpty(t *testing.T) {
	t.Parallel()
	m := NewMock()
	got, err := m.ListOwnTokenDelegations(context.Background())
	if err != nil {
		t.Fatalf("ListOwnTokenDelegations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("default mock delegations = %+v, want empty", got)
	}
}

func TestMock_MintDelegatedLaunchToken_DefaultUnavailable(t *testing.T) {
	t.Parallel()
	m := NewMock()
	_, err := m.MintDelegatedLaunchToken(context.Background(), "any-name")
	if err == nil {
		t.Fatal("expected error on unseeded mock (pre-delegation/consumed platform default), got nil")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != ErrDelegationUnavailable {
		t.Errorf("Code = %q, want %q", pe.Code, ErrDelegationUnavailable)
	}
}

// TestMock_MintDelegatedLaunchToken_OneShot pins F4: a successful mint
// consumes the delegation — list is empty afterward, and a second mint
// errors.
func TestMock_MintDelegatedLaunchToken_OneShot(t *testing.T) {
	t.Parallel()
	m := NewMock().WithTokenDelegations(TokenDelegation{ID: "d1", TokenID: "t1", CanCreateProjects: true})

	before, err := m.ListOwnTokenDelegations(context.Background())
	if err != nil {
		t.Fatalf("ListOwnTokenDelegations (before mint): %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("seeded delegations = %+v, want 1 row", before)
	}

	minted, err := m.MintDelegatedLaunchToken(context.Background(), "zcp-launch-x")
	if err != nil {
		t.Fatalf("MintDelegatedLaunchToken (first): %v", err)
	}
	if minted.Token == "" {
		t.Error("minted.Token is empty")
	}

	after, err := m.ListOwnTokenDelegations(context.Background())
	if err != nil {
		t.Fatalf("ListOwnTokenDelegations (after mint): %v", err)
	}
	if len(after) != 0 {
		t.Errorf("delegations after mint = %+v, want empty (F4: mint consumes the delegation)", after)
	}

	_, err = m.MintDelegatedLaunchToken(context.Background(), "zcp-launch-y")
	if err == nil {
		t.Fatal("second mint should error (delegation already consumed), got nil")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != ErrDelegationUnavailable {
		t.Errorf("Code = %q, want %q", pe.Code, ErrDelegationUnavailable)
	}

	if got := m.CallCounts["MintDelegatedLaunchToken"]; got != 2 {
		t.Errorf("MintDelegatedLaunchToken call count = %d, want 2", got)
	}
	if got := m.CallCounts["ListOwnTokenDelegations"]; got != 2 {
		t.Errorf("ListOwnTokenDelegations call count = %d, want 2", got)
	}
}

func TestMock_WithMintedToken_ReturnsSeededValue(t *testing.T) {
	t.Parallel()
	seeded := MintedToken{Token: "sentinel-token-value", TokenID: "tok-seeded", Name: "seeded-name"}
	m := NewMock().
		WithTokenDelegations(TokenDelegation{ID: "d1", TokenID: "t1", CanCreateProjects: true}).
		WithMintedToken(seeded)

	got, err := m.MintDelegatedLaunchToken(context.Background(), "caller-supplied-name")
	if err != nil {
		t.Fatalf("MintDelegatedLaunchToken: %v", err)
	}
	if got != seeded {
		t.Errorf("MintDelegatedLaunchToken = %+v, want seeded %+v", got, seeded)
	}
}

func TestMock_WithError_OverridesDelegationMethods(t *testing.T) {
	t.Parallel()
	sentinel := NewPlatformError(ErrNetworkError, "boom", "")

	m1 := NewMock().WithError("ListOwnTokenDelegations", sentinel)
	if _, err := m1.ListOwnTokenDelegations(context.Background()); !errors.Is(err, error(sentinel)) {
		t.Errorf("ListOwnTokenDelegations error = %v, want sentinel %v", err, sentinel)
	}

	m2 := NewMock().WithError("MintDelegatedLaunchToken", sentinel)
	if _, err := m2.MintDelegatedLaunchToken(context.Background(), "x"); !errors.Is(err, error(sentinel)) {
		t.Errorf("MintDelegatedLaunchToken error = %v, want sentinel %v", err, sentinel)
	}
}
