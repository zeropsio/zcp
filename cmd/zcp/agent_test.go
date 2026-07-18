package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

func inContainerDeps(mock platform.Client, stdout *bytes.Buffer) agentMarkOAuthDeps {
	return agentMarkOAuthDeps{
		runtimeInfo: func() runtime.Info { return runtime.Info{InContainer: true, ServiceID: "svc-1"} },
		client:      func() (platform.Client, error) { return mock, nil },
		stdout:      stdout,
	}
}

func TestRunAgentMarkOAuth_UnknownAgentID_UsageErrorNoNetwork(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithError("GetServiceEnv", errors.New("must not be called"))
	var stdout bytes.Buffer

	err := runAgentMarkOAuth([]string{"not-a-real-agent"}, inContainerDeps(mock, &stdout))
	if err == nil {
		t.Fatal("expected an error for an unknown agent id")
	}
	if !errors.Is(err, errAgentUsage) {
		t.Errorf("error = %v, want errAgentUsage in the chain", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on a usage error", stdout.String())
	}
}

func TestRunAgentMarkOAuth_WrongArgCount_UsageError(t *testing.T) {
	t.Parallel()

	tests := [][]string{
		{},
		{"claude-code", "extra"},
	}
	for _, args := range tests {
		mock := platform.NewMock()
		var stdout bytes.Buffer
		err := runAgentMarkOAuth(args, inContainerDeps(mock, &stdout))
		if err == nil {
			t.Fatalf("args=%v: expected a usage error", args)
		}
		if !errors.Is(err, errAgentUsage) {
			t.Errorf("args=%v: error = %v, want errAgentUsage in the chain", args, err)
		}
	}
}

func TestRunAgentMarkOAuth_NotInContainer_Errors(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithError("GetServiceEnv", errors.New("must not be called"))
	var stdout bytes.Buffer
	deps := agentMarkOAuthDeps{
		runtimeInfo: func() runtime.Info { return runtime.Info{InContainer: false} },
		client:      func() (platform.Client, error) { return mock, nil },
		stdout:      &stdout,
	}

	err := runAgentMarkOAuth([]string{"claude-code"}, deps)
	if err == nil {
		t.Fatal("expected an error when not inside a Zerops container")
	}
	if errors.Is(err, errAgentUsage) {
		t.Error("not-in-container must not classify as a usage error (exit 1, not 2)")
	}
}

func TestRunAgentMarkOAuth_Success_WritesSingleLineJSON(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	var stdout bytes.Buffer

	if err := runAgentMarkOAuth([]string{"claude-code"}, inContainerDeps(mock, &stdout)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if n := strings.Count(out, "\n"); n != 1 {
		t.Fatalf("stdout = %q, want exactly one line", out)
	}

	var got agentMarkOAuthOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v (%q)", err, out)
	}
	want := agentMarkOAuthOutput{OK: true, Agent: "claude-code", Key: "ZCP_AGENT_OAUTH_CLAUDE_CODE", Changed: true}
	if got != want {
		t.Errorf("output = %+v, want %+v", got, want)
	}
}

func TestRunAgentMarkOAuth_AlreadyAuthorized_ChangedFalse(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServiceEnv("svc-1", []platform.ServiceEnvVar{
		{ID: "udata-1", Key: "ZCP_AGENT_OAUTH_CODEX", Content: "true"},
	})
	var stdout bytes.Buffer

	if err := runAgentMarkOAuth([]string{"codex"}, inContainerDeps(mock, &stdout)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got agentMarkOAuthOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v (%q)", err, stdout.String())
	}
	if got.Changed {
		t.Error("Changed = true, want false — flag was already authorized")
	}
}

func TestRunAgentMarkOAuth_PlatformError_NotUsageError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithError("GetServiceEnv", errors.New("platform unavailable"))
	var stdout bytes.Buffer

	err := runAgentMarkOAuth([]string{"claude-code"}, inContainerDeps(mock, &stdout))
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, errAgentUsage) {
		t.Error("a platform failure must not classify as a usage error (exit 1, not 2)")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on failure", stdout.String())
	}
}
