package safety

import (
	"errors"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const testWriteToken = "write-secret-token"

// TestAuthorizeWrite pins the caller-bound write gate: a write is authorized only by
// presenting the EXACT process write token AND a per-action confirm. Every capability
// failure — arming not permitted, no token minted, or a wrong token — collapses to
// the SAME uniform ErrReadOnly (no oracle); a correct token still needs the confirm.
func TestAuthorizeWrite(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		armingPermitted bool
		mintedToken     string
		writeCap        string
		confirmed       bool
		want            error
	}{
		{"correct token + confirm writes", true, testWriteToken, testWriteToken, true, nil},
		{"correct token, no confirm needs confirm", true, testWriteToken, testWriteToken, false, provider.ErrNeedsConfirm},
		{"wrong token refused even confirmed", true, testWriteToken, "not-the-token", true, provider.ErrReadOnly},
		{"empty cap refused even confirmed", true, testWriteToken, "", true, provider.ErrReadOnly},
		{"no token minted refuses empty cap", true, "", "", true, provider.ErrReadOnly},
		{"arming not permitted refuses correct token", false, testWriteToken, testWriteToken, true, provider.ErrReadOnly},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := NewPolicy(c.armingPermitted, c.mintedToken, "")
			if got := p.AuthorizeWrite(c.writeCap, c.confirmed); !errors.Is(got, c.want) {
				t.Fatalf("AuthorizeWrite(%q, %v) = %v, want %v", c.writeCap, c.confirmed, got, c.want)
			}
		})
	}
}

// TestAuthorizeWrite_CallerBound pins the property the process-global arm model
// violated: authority is per CALLER (the write token), never a process latch. A
// token holder's successful write does NOT enable a different caller who lacks the
// token — a bearer-only caller stays refused both before and after.
func TestAuthorizeWrite_CallerBound(t *testing.T) {
	t.Parallel()
	p := NewPolicy(true, testWriteToken, "")
	// Caller A holds the write token: authorized.
	if err := p.AuthorizeWrite(testWriteToken, true); err != nil {
		t.Fatalf("token holder = %v, want nil", err)
	}
	// Caller B (bearer only, no token) is refused — A's success latched nothing.
	if err := p.AuthorizeWrite("", true); !errors.Is(err, provider.ErrReadOnly) {
		t.Fatalf("bearer-only after a token-holder write = %v, want ErrReadOnly", err)
	}
	// A stays authorized; B's attempt changed nothing about the process.
	if err := p.AuthorizeWrite(testWriteToken, true); err != nil {
		t.Fatalf("token holder second write = %v, want nil", err)
	}
}

// TestAuthorizeWrite_Concurrent runs AuthorizeWrite from many goroutines under -race
// to prove the immutable policy needs no external lock.
func TestAuthorizeWrite_Concurrent(t *testing.T) {
	t.Parallel()
	p := NewPolicy(true, testWriteToken, "")
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(2)
		go func() { defer wg.Done(); _ = p.AuthorizeWrite(testWriteToken, true) }()
		go func() { defer wg.Done(); _ = p.AuthorizeWrite("", true) }()
	}
	wg.Wait()
}

// TestEnvironment pins the reserved prod-discriminator accessor: it round-trips the
// constructor value and is empty in v1's default construction.
func TestEnvironment(t *testing.T) {
	t.Parallel()
	if got := NewPolicy(true, testWriteToken, "").Environment(); got != "" {
		t.Fatalf("default Environment() = %q, want empty", got)
	}
	if got := NewPolicy(true, testWriteToken, "production").Environment(); got != "production" {
		t.Fatalf("Environment() = %q, want production", got)
	}
}
