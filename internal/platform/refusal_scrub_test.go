// Tests for: the secrets every refusal a client surfaces is scrubbed of
// (review R4-11) — the Swarm client's bearer and lease from every field its
// Error() prints (code and reason included), the Box client's token from its
// metadata's keys as well as its values, and a sensitive userData write's
// value in the escaped forms a server echoing the JSON body it was sent
// carries, not only verbatim.
package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestSwarmClient_CodeAndReasonEchoingSecrets_Scrubbed: an orchestrator that
// echoes what it was sent in its error code or in its metadata's reason —
// both printed by SwarmError.Error() — never puts the bearer or the lease in
// the error.
func TestSwarmClient_CodeAndReasonEchoingSecrets_Scrubbed(t *testing.T) {
	t.Parallel()
	const lease = "feedfacefeedfacefeedfacefeedface"
	const orchToken = "orch-api-token-0001"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"bad-` + orchToken + `","message":"refused","meta":[` +
			`{"code":"x","error":"x","metadata":{"reason":["lease-` + lease + `"],"retryable":["false"]}}]}}`))
	}))
	defer srv.Close()

	c := NewSwarmClient(srv.URL, orchToken, srv.Client())
	_, err := c.Release(context.Background(), "c-5", lease, true)
	var se *SwarmError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v (%T), want a *SwarmError", err, err)
	}
	for what, text := range map[string]string{"Error()": err.Error(), "Code": se.Code, "Reason": se.Reason} {
		for _, secret := range []string{orchToken, lease} {
			if strings.Contains(text, secret) {
				t.Errorf("%s carries a secret: %s", what, text)
			}
		}
	}
}

// TestBoxClient_MetadataKeyEchoingTheToken_Scrubbed: a Box refusal whose
// metadata names the bearer as a key keeps no key carrying it.
func TestBoxClient_MetadataKeyEchoingTheToken_Scrubbed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"workspaceFull","message":"no room",` +
			`"meta":[{"metadata":{"resource":["memory"],"token ` + boxTestToken + `":["seen"]}}]}}`))
	}))
	defer srv.Close()
	var mints int
	var mu sync.Mutex
	c := NewBoxClient(staticMint(srv.URL, &mints, &mu), srv.Client())
	_, err := c.PullBase(context.Background(), BoxPullBaseRequest{ImageRef: "x", Name: "default"}, "k-1")
	var be *BoxError
	if !errors.As(err, &be) {
		t.Fatalf("err = %v (%T), want a *BoxError", err, err)
	}
	if be.Metadata["resource"] != "memory" {
		t.Errorf("metadata = %v, want the refusal's own keys kept", be.Metadata)
	}
	for k := range be.Metadata {
		if strings.Contains(k, boxTestToken) {
			t.Errorf("metadata key %q carries the token", k)
		}
	}
}

// TestCreateServiceEnvVar_RefusalEchoingTheEncodedBody_SurfacesWithoutIt: a
// refusal that echoes the request body it was sent carries the value as the
// body encoded it — `<`, `>` and `&` as <, > and &, a quote
// and a backslash escaped — which an exact match never finds. The error
// carries none of its forms.
func TestCreateServiceEnvVar_RefusalEchoingTheEncodedBody_SurfacesWithoutIt(t *testing.T) {
	t.Parallel()
	const secret = `tok<en>&"quote\slash-0001`
	encodings := map[string]string{
		"HTML-safe, as Go encodes it": `tok\\u003cen\\u003e\\u0026\\\"quote\\\\slash-0001`,
		"plain JSON":                  `tok<en>&\\\"quote\\\\slash-0001`,
	}
	for name, echoed := range encodings {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				// The message quotes the body: {"key":…,"content":"<echoed>"}.
				_, _ = w.Write([]byte(`{"error":{"code":"userDataInvalidContent","message":"invalid body {\"content\":\"` + echoed + `\"}",` +
					`"meta":[{"code":"invalidContent","error":"x","metadata":{"body":["{\"content\":\"` + echoed + `\"}"]}}]}}`))
			}))
			t.Cleanup(srv.Close)
			z, err := NewZeropsClient("fake-token", srv.URL)
			if err != nil {
				t.Fatalf("NewZeropsClient: %v", err)
			}
			_, err = z.CreateServiceEnvVar(context.Background(), "svc-1", "GIT_TOKEN", secret, true)
			var pe *PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a *PlatformError", err)
			}
			text := err.Error() + fmt.Sprintf("%+v", pe.APIMeta) + pe.Message
			for _, fragment := range []string{"tok<en", "tok\\u003cen", "quote", "slash-0001"} {
				if strings.Contains(text, fragment) {
					t.Errorf("the error carries %q of the submitted value: %s", fragment, text)
				}
			}
			if pe.APICode != "userDataInvalidContent" || !strings.Contains(pe.Message, "invalid body") {
				t.Errorf("APICode %q / Message %q: want the refusal's own code and words", pe.APICode, pe.Message)
			}
		})
	}
}
