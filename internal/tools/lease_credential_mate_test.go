// Tests for: tools/lease.go + tools/lease_sync.go + tools/errwire.go — a
// lease's git failing on the pool's GIT_TOKEN. Outside a Mate the agent asks
// the user (the user-owned credential contract, B6b). Inside one the token is
// the person's own in the pool's own vault, which Mate's vault does not show,
// so the agent never asks for it in the chat: it asks the person to set it on
// the pool's service in Zerops.
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// plainCredentialContract is the user-owned credential contract as every
// container without a Mate renders it, byte for byte.
const plainCredentialContract = "This credential is a user-held secret — surface this failure and ask the user (AskUserQuestion when your harness exposes it, else ask the user in plain text) for a corrected value; NEVER generate, guess, or mutate a token."

// authFailure is git's refusal of the pool's GIT_TOKEN, as ssh carries it.
func authFailure(host string) error {
	return &platform.SSHExecError{
		Hostname: host, Output: "fatal: Authentication failed for 'https://github.com/acme/app/'",
		Err: errExit1,
	}
}

// credentialRT is the container a credential test runs in: a Mate's, or a
// plain one.
func credentialRT(mate bool) runtime.Info {
	return runtime.Info{InContainer: true, ProjectID: "proj-1", MateEnabled: mate}
}

// assertCredentialContract checks text, the part of a result that carries the
// contract: outside a Mate the plain contract once, byte for byte; inside one
// the Mate contract naming each pool, never the chat ask, its NEVER clause
// kept.
func assertCredentialContract(t *testing.T, text string, mate bool, pools []string) {
	t.Helper()
	if n := strings.Count(text, "user-held secret"); n != 1 {
		t.Fatalf("the contract appears %d times, want once: %s", n, text)
	}
	if !mate {
		if !strings.Contains(text, plainCredentialContract) {
			t.Errorf("outside a Mate the contract is not the plain one byte for byte: %s", text)
		}
		return
	}
	contract := text[strings.Index(text, "This credential is a user-held secret"):]
	for _, want := range []string{"GIT_TOKEN", "Mate's vault does not show", "Never ask for it in the chat", "in Zerops", "a token with access to the repository", "then retry", "NEVER generate, guess, or mutate a token"} {
		if !strings.Contains(contract, want) {
			t.Errorf("the Mate contract lacks %q: %s", want, contract)
		}
	}
	for _, pool := range pools {
		if !strings.Contains(contract, `"`+pool+`"`) {
			t.Errorf("the Mate contract does not name the pool %q: %s", pool, contract)
		}
	}
	for _, banned := range []string{"plain text", "AskUserQuestion"} {
		if strings.Contains(contract, banned) {
			t.Errorf("the Mate contract carries %q: %s", banned, contract)
		}
	}
}

// errorSuggestion is the suggestion of an error result's ErrorWire.
func errorSuggestion(t *testing.T, text string) string {
	t.Helper()
	var wire ErrorWire
	if err := json.Unmarshal([]byte(text), &wire); err != nil {
		t.Fatalf("result is not an ErrorWire: %v\n%s", err, text)
	}
	return wire.Suggestion
}

// TestLeaseTool_AcquireCredentialFailure_ContractByMate: an acquire whose
// checkout the remote refuses on the pool's GIT_TOKEN carries the plain
// contract outside a Mate, byte for byte, and inside one names the pool — or,
// for a group acquire, every pool it requested — whose own vault holds it.
func TestLeaseTool_AcquireCredentialFailure_ContractByMate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		mate      bool
		group     bool
		wantPools []string
	}{
		{"a plain container keeps the user-owned contract", false, false, nil},
		{"a plain container's group acquire keeps it too", false, true, nil},
		{"a Mate names the pool's own vault", true, false, []string{"devpool"}},
		{"a Mate's group acquire names every pool it requested", true, true, []string{"webpool", "hqpool"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ssh := &leaseStubSSH{fn: func(string) ([]byte, error) { return nil, authFailure("pool") }}
			var res *mcp.CallToolResult
			if tt.group {
				srv, _, _ := groupLeaseServerSSHIn(t, ssh, credentialRT(tt.mate))
				res = callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"webpool", "hqpool"}, "repo": leaseTestRepo, "branch": "feat-x"})
			} else {
				srv, _, _ := leaseToolServerIn(t, credentialRT(tt.mate), ssh)
				res = callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feature"})
			}
			text := getTextContent(t, res)
			if !res.IsError || !strings.Contains(text, platform.ErrGitTokenInvalid) {
				t.Fatalf("result = (error %v) %s, want %s", res.IsError, text, platform.ErrGitTokenInvalid)
			}
			assertCredentialContract(t, errorSuggestion(t, text), tt.mate, tt.wantPools)
		})
	}
}

// syncCredentialSSH is syncToolSSH with the remote refusing the pool's
// GIT_TOKEN, once refuse is set: the source's push (push) or the fast-forward
// of member (member).
type syncCredentialSSH struct {
	syncToolSSH
	refuse bool
	push   bool
	member string
}

func (s *syncCredentialSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	refuse := s.refuse
	s.mu.Unlock()
	switch {
	case refuse && s.push && strings.Contains(command, "push --porcelain"):
		return nil, authFailure(host)
	case refuse && host == s.member && strings.Contains(command, "merge --ff-only"):
		return nil, authFailure(host)
	}
	return s.syncToolSSH.ExecSSH(ctx, host, command)
}

func (s *syncCredentialSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// TestLeaseTool_SyncCredentialFailure_ContractByMate: a sync whose source push
// or whose fast-forward of another lease the remote refuses on the pool's
// GIT_TOKEN carries the plain contract outside a Mate, byte for byte, and
// inside one names the pool of the lease that failed.
func TestLeaseTool_SyncCredentialFailure_ContractByMate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		mate      bool
		push      bool
		member    string
		wantPools []string
	}{
		{"plain: the source's push refused", false, true, "", nil},
		{"Mate: the source's push refused names the source's pool", true, true, "", []string{"hqpool"}},
		{"plain: another lease's fast-forward refused", false, false, "webpool-7", nil},
		{"Mate: another lease's fast-forward refused names its pool", true, false, "webpool-7", []string{"webpool"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ssh := &syncCredentialSSH{push: tt.push, member: tt.member}
			srv, _, _ := groupLeaseServerSSHIn(t, ssh, credentialRT(tt.mate))
			acquireFeatX(t, srv)
			ssh.mu.Lock()
			ssh.refuse = true
			ssh.mu.Unlock()

			res := callTool(t, srv, "zerops_lease", map[string]any{"action": "sync", "lease": "hqpool-3"})
			text := getTextContent(t, res)
			if tt.push {
				if !res.IsError || !strings.Contains(text, platform.ErrGitTokenInvalid) {
					t.Fatalf("result = (error %v) %s, want %s", res.IsError, text, platform.ErrGitTokenInvalid)
				}
				assertCredentialContract(t, errorSuggestion(t, text), tt.mate, tt.wantPools)
				return
			}
			if res.IsError {
				t.Fatalf("sync failed: %s", text)
			}
			var got syncToolResult
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, text)
			}
			if m := got.Sync.Members; len(m) != 1 || m[0].Code != platform.ErrGitTokenInvalid {
				t.Fatalf("members = %+v, want webpool-7 failed with %s", m, platform.ErrGitTokenInvalid)
			}
			assertCredentialContract(t, got.NextActions, tt.mate, tt.wantPools)
		})
	}
}

// TestConvertError_CredentialContract_PlainIsByteIdentical pins the plain
// contract every credential-class error without a Mate carries.
func TestConvertError_CredentialContract_PlainIsByteIdentical(t *testing.T) {
	t.Parallel()
	for _, code := range []string{platform.ErrGitTokenInvalid, platform.ErrGitTokenMissing} {
		got := errorSuggestion(t, extractText(convertError(platform.NewPlatformError(code, "failed", "Fix the token."))))
		if want := "Fix the token. " + plainCredentialContract; got != want {
			t.Errorf("%s suggestion = %q, want %q", code, got, want)
		}
	}
}
