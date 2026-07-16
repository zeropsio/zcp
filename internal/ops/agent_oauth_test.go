// Tests for: ops/agent_oauth.go — `zcp agent mark-oauth`'s platform-flag mutation.
package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestAgentOAuthEnvKey_SuffixTable pins the id -> ZCP_AGENT_OAUTH_<SUFFIX>
// mapping against every id in the JS launcher REGISTRY (see the comment on
// agentOAuthSuffixes) plus the unknown-id rejection.
func TestAgentOAuthEnvKey_SuffixTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		agentID string
		wantKey string
		wantOK  bool
	}{
		{"claude-code", "claude-code", "ZCP_AGENT_OAUTH_CLAUDE_CODE", true},
		{"codex", "codex", "ZCP_AGENT_OAUTH_CODEX", true},
		{"antigravity", "antigravity", "ZCP_AGENT_OAUTH_ANTIGRAVITY", true},
		{"grok", "grok", "ZCP_AGENT_OAUTH_GROK", true},
		{"cursor", "cursor", "ZCP_AGENT_OAUTH_CURSOR", true},
		{"unknown id", "not-a-real-agent", "", false},
		{"empty id", "", "", false},
		{"case-sensitive: uppercase id is not the same key", "Claude-Code", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key, ok := AgentOAuthEnvKey(tt.agentID)
			if key != tt.wantKey || ok != tt.wantOK {
				t.Errorf("AgentOAuthEnvKey(%q) = (%q, %v), want (%q, %v)", tt.agentID, key, ok, tt.wantKey, tt.wantOK)
			}
		})
	}
}

func TestMarkAgentOAuth_CreatesWhenAbsent(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()

	result, err := MarkAgentOAuth(context.Background(), mock, "svc-1", "claude-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Changed {
		t.Error("Changed = false, want true (key was absent)")
	}
	if result.Key != "ZCP_AGENT_OAUTH_CLAUDE_CODE" {
		t.Errorf("Key = %q, want ZCP_AGENT_OAUTH_CLAUDE_CODE", result.Key)
	}

	envs, err := mock.GetServiceEnv(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("get service env: %v", err)
	}
	if got := findEnvIDByKey(envs, result.Key); got == "" {
		t.Fatalf("expected %s to be stored on svc-1, got envs=%+v", result.Key, envs)
	}
	for _, e := range envs {
		if e.Key == result.Key && e.Content != "true" {
			t.Errorf("stored content = %q, want %q", e.Content, "true")
		}
	}
}

func TestMarkAgentOAuth_UpdatesWhenPresentButNotTrue(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceEnv("svc-1", []platform.ServiceEnvVar{
		{ID: "udata-1", Key: "ZCP_AGENT_OAUTH_CODEX", Content: "false"},
	})

	result, err := MarkAgentOAuth(context.Background(), mock, "svc-1", "codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Changed {
		t.Error("Changed = false, want true (existing value was not \"true\")")
	}

	envs, err := mock.GetServiceEnv(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("get service env: %v", err)
	}
	found := false
	for _, e := range envs {
		if e.Key == "ZCP_AGENT_OAUTH_CODEX" {
			found = true
			if e.Content != "true" {
				t.Errorf("content = %q, want %q", e.Content, "true")
			}
		}
	}
	if !found {
		t.Fatal("ZCP_AGENT_OAUTH_CODEX missing after update")
	}
}

// TestMarkAgentOAuth_AlreadyTrue_NoMutation pins the check-before-mutate
// discipline (CLAUDE.md trap / spec-workflows.md §8 O3): when the flag is
// already "true", MarkAgentOAuth must not call CreateServiceEnvVar or
// DeleteUserData at all. Proven by injecting a hard failure on both mutating
// methods — if MarkAgentOAuth called either, the test would fail via the
// injected error rather than by an unobserved call count.
func TestMarkAgentOAuth_AlreadyTrue_NoMutation(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServiceEnv("svc-1", []platform.ServiceEnvVar{
			{ID: "udata-1", Key: "ZCP_AGENT_OAUTH_CLAUDE_CODE", Content: "true"},
		}).
		WithError("CreateServiceEnvVar", errors.New("must not be called")).
		WithError("DeleteUserData", errors.New("must not be called"))

	result, err := MarkAgentOAuth(context.Background(), mock, "svc-1", "claude-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Changed {
		t.Error("Changed = true, want false (flag was already true — no mutation expected)")
	}
	if result.Key != "ZCP_AGENT_OAUTH_CLAUDE_CODE" {
		t.Errorf("Key = %q, want ZCP_AGENT_OAUTH_CLAUDE_CODE", result.Key)
	}
}

func TestMarkAgentOAuth_UnknownAgentID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithError("GetServiceEnv", errors.New("must not be called"))

	_, err := MarkAgentOAuth(context.Background(), mock, "svc-1", "not-a-real-agent")
	if err == nil {
		t.Fatal("expected an error for an unknown agent id")
	}
}

func TestMarkAgentOAuth_EmptyServiceID_Errors(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	_, err := MarkAgentOAuth(context.Background(), mock, "", "claude-code")
	if err == nil {
		t.Fatal("expected an error for an empty serviceID")
	}
}

func TestMarkAgentOAuth_ReadError_Wrapped(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	mock := platform.NewMock().WithError("GetServiceEnv", wantErr)

	_, err := MarkAgentOAuth(context.Background(), mock, "svc-1", "claude-code")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error chain does not wrap the underlying error: %v", err)
	}
}

func TestMarkAgentOAuth_CreateError_Wrapped(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	mock := platform.NewMock().WithError("CreateServiceEnvVar", wantErr)

	_, err := MarkAgentOAuth(context.Background(), mock, "svc-1", "claude-code")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error chain does not wrap the underlying error: %v", err)
	}
}
