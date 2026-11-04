package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Error mapping
// ---------------------------------------------------------------------------

func TestTranslateMintRefused_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		apiCode  string
		wantCode string
	}{
		{"current platform code translates", apiCodeTokenCannotMint, ErrTokenCannotMint},
		{"legacy platform code translates", apiCodeTokenCannotMintLegacy, ErrTokenCannotMint},
		{"unrelated 403 code passes through untouched (F5 roleLevelExceeded)", "roleLevelExceeded", ErrPermissionDenied},
		{"empty api code passes through untouched", "", ErrPermissionDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := withAPICode(NewPlatformError(ErrPermissionDenied, "denied by platform", "check permissions"), tt.apiCode, nil)
			got := translateMintRefused(fmt.Errorf("mint: %w", in))

			var pe *PlatformError
			if !errors.As(got, &pe) {
				t.Fatalf("translateMintRefused returned non-PlatformError: %v", got)
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

func TestTranslateMintRefused_PreservesAPIMeta(t *testing.T) {
	t.Parallel()
	meta := []APIMetaItem{{Code: "x", Error: "y"}}
	in := withAPICode(NewPlatformError(ErrPermissionDenied, "denied", ""), apiCodeTokenCannotMint, meta)
	got := translateMintRefused(in)

	var pe *PlatformError
	if !errors.As(got, &pe) {
		t.Fatalf("expected PlatformError, got %T", got)
	}
	if !reflect.DeepEqual(pe.APIMeta, meta) {
		t.Errorf("APIMeta = %+v, want %+v", pe.APIMeta, meta)
	}
}

func TestTranslateMintRefused_NonPlatformErrorPassesThrough(t *testing.T) {
	t.Parallel()
	in := errors.New("transport failure")
	got := translateMintRefused(in)
	if got != in {
		t.Errorf("non-PlatformError input should pass through unchanged, got %v", got)
	}
}

// TestApiCodeTokenCannotMint_Constants pins the wire-error code values —
// both the current and the legacy platform response.
func TestApiCodeTokenCannotMint_Constants(t *testing.T) {
	t.Parallel()
	if apiCodeTokenCannotMint != "notAllowedForIntegrationTokenWithoutDelegation" {
		t.Errorf("apiCodeTokenCannotMint drifted from spec: got %q", apiCodeTokenCannotMint)
	}
	if apiCodeTokenCannotMintLegacy != "notAllowedForIntegrationToken" {
		t.Errorf("apiCodeTokenCannotMintLegacy drifted from spec: got %q", apiCodeTokenCannotMintLegacy)
	}
}

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

// ---------------------------------------------------------------------------
// MintLaunchKey (SDK-backed POST + apiCode translation)
// ---------------------------------------------------------------------------

func mintPath(clientID string) string {
	return "/api/rest/public/client/" + clientID + "/integration-token"
}

// mintTestClientID is the org every launch-key mint test mints in.
const mintTestClientID = "client-abc"

func newMintTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *ZeropsClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	return z
}

// TestMintedToken_MarshalOmitsCredential pins the carrier's marshal-proof
// shape: the raw token — and even a stable prefix of it — must
// never survive json.Marshal, so an accidental serialization of the struct
// into a response/state/audit surface cannot leak the credential.
func TestMintedToken_MarshalOmitsCredential(t *testing.T) {
	t.Parallel()

	m := MintedToken{Token: "sentinel-minted-credential-value", TokenID: "tok-1"}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, "sentinel-minted-credential-value") {
		t.Errorf("raw token survived Marshal: %s", s)
	}
	if strings.Contains(s, "sentinel-minted") {
		t.Errorf("token prefix survived Marshal: %s", s)
	}
}

func TestMintLaunchKey_Success(t *testing.T) {
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

	got, err := z.MintLaunchKey(context.Background(), mintTestClientID, "zcp-launch-test")
	if err != nil {
		t.Fatalf("MintLaunchKey: %v", err)
	}
	want := MintedToken{Token: "raw-secret-value", TokenID: "newtok123"}
	if got != want {
		t.Errorf("MintLaunchKey = %+v, want %+v", got, want)
	}
}

// TestMintLaunchKey_RequestBodyPinsLaunchKeyShape pins the
// outbound POST body: every permission field is EXPLICIT —
// finance denial is a launch-key invariant, not an incidental Go
// zero value, and projects must serialize as an empty array (not null).
// Without this pin a future edit could silently drop a field and the
// response-decode tests would stay green (2026-07-12 audit #5).
func TestMintLaunchKey_RequestBodyPinsLaunchKeyShape(t *testing.T) {
	t.Parallel()

	var rawBody []byte
	z := newMintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != mintPath(mintTestClientID) {
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
		_, _ = w.Write([]byte(`{"id":"newtok123","name":"zcp-launch-test","roleCode":"NO_ACCESS","canCreateProjects":true,"token":"raw-secret-value"}`))
	})

	if _, err := z.MintLaunchKey(context.Background(), mintTestClientID, "zcp-launch-test"); err != nil {
		t.Fatalf("MintLaunchKey: %v", err)
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
	assertField("name", `"zcp-launch-test"`)
	assertField("roleCode", `"NO_ACCESS"`)
	assertField("canCreateProjects", "true")
	assertField("canViewFinances", "false")
	assertField("canEditFinances", "false")
	if raw, ok := body["projects"]; !ok {
		t.Errorf("mint body must carry projects explicitly; body = %s", rawBody)
	} else if got := strings.TrimSpace(string(raw)); got != "[]" {
		t.Errorf("mint body projects = %s, want [] (empty array, NOT null)", got)
	}
}

func TestMintLaunchKey_NotAllowedWithoutDelegation_MapsToErrTokenCannotMint(t *testing.T) {
	t.Parallel()

	z := newMintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"notAllowedForIntegrationTokenWithoutDelegation","message":"This action is not allowed for integration tokens without explicit delegation."}}`))
	})

	_, err := z.MintLaunchKey(context.Background(), mintTestClientID, "zcp-launch-test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != ErrTokenCannotMint {
		t.Errorf("Code = %q, want %q", pe.Code, ErrTokenCannotMint)
	}
}

func TestMintLaunchKey_LegacyNotAllowedForIntegrationToken_MapsToErrTokenCannotMint(t *testing.T) {
	t.Parallel()

	z := newMintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"notAllowedForIntegrationToken","message":"This action is not allowed for integration tokens."}}`))
	})

	_, err := z.MintLaunchKey(context.Background(), mintTestClientID, "zcp-launch-test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != ErrTokenCannotMint {
		t.Errorf("Code = %q, want %q", pe.Code, ErrTokenCannotMint)
	}
}

// TestMintLaunchKey_RoleLevelExceeded_NotTranslated pins that a
// generic 403 unrelated to an integration-token minter (e.g. roleLevelExceeded)
// must NOT be swallowed into ErrTokenCannotMint.
func TestMintLaunchKey_RoleLevelExceeded_NotTranslated(t *testing.T) {
	t.Parallel()

	z := newMintTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"roleLevelExceeded","message":"exceeds delegated role"}}`))
	})

	_, err := z.MintLaunchKey(context.Background(), mintTestClientID, "zcp-launch-test")
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
