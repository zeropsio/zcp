// Tests for: ops/env_request.go — asking the person for a vault value.
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// purpose is the one sentence every request carries for the person.
const purpose = "Stripe charges cards; Dashboard, Developers, API keys."

func TestEnvRequest_Answers(t *testing.T) {
	t.Parallel()

	yes, no := true, false
	tests := []struct {
		name      string
		hostname  string
		project   bool
		key       string
		sensitive *bool
		want      EnvRequestResult
	}{
		{
			name: "shared, absent, secret-shaped name defaults sensitive", project: true, key: "STRIPE_SECRET_KEY",
			want: EnvRequestResult{Reason: purpose, Key: "STRIPE_SECRET_KEY", Scope: EnvRequestScopeShared, Sensitive: true},
		},
		{
			name: "service, absent, plain name defaults plain", hostname: "api", key: "SUPPORT_EMAIL",
			want: EnvRequestResult{Reason: purpose, Key: "SUPPORT_EMAIL", Scope: EnvRequestScopeService, ServiceHostname: "api"},
		},
		{
			name: "the caller's flag wins over the name", hostname: "api", key: "SUPPORT_EMAIL", sensitive: &yes,
			want: EnvRequestResult{Reason: purpose, Key: "SUPPORT_EMAIL", Scope: EnvRequestScopeService, ServiceHostname: "api", Sensitive: true},
		},
		{
			name: "the caller's plain wins over a secret-shaped name", project: true, key: "PUBLIC_KEY", sensitive: &no,
			want: EnvRequestResult{Reason: purpose, Key: "PUBLIC_KEY", Scope: EnvRequestScopeShared},
		},
		{
			name: "shared, already set — reports the stored flag", project: true, key: "OPENAI_API_KEY",
			want: EnvRequestResult{Reason: purpose, Key: "OPENAI_API_KEY", Scope: EnvRequestScopeShared, Sensitive: true, AlreadySet: true},
		},
		{
			name: "service, already set", hostname: "api", key: "MAIL_FROM",
			want: EnvRequestResult{Reason: purpose, Key: "MAIL_FROM", Scope: EnvRequestScopeService, ServiceHostname: "api", AlreadySet: true},
		},
		{
			name: "project wins over a hostname", hostname: "api", project: true, key: "MAIL_FROM",
			want: EnvRequestResult{Reason: purpose, Key: "MAIL_FROM", Scope: EnvRequestScopeShared},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", ProjectID: "proj-1"}}).
				WithProjectEnv([]platform.ProjectEnvVar{{ID: "p1", Key: "OPENAI_API_KEY", Content: "sk-live", Sensitive: true}}).
				WithServiceEnv("svc-1", []platform.ServiceEnvVar{{ID: "s1", Key: "MAIL_FROM", Content: "a@b.c", Type: platform.ServiceEnvUser}})

			got, err := EnvRequest(context.Background(), mock, "proj-1", tt.hostname, tt.project, tt.key, purpose, tt.sensitive)
			if err != nil {
				t.Fatalf("EnvRequest: %v", err)
			}
			if *got != tt.want {
				t.Errorf("got %+v, want %+v", *got, tt.want)
			}
			for _, write := range []string{"CreateServiceEnvVar", "DeleteUserData", "DeleteProjectEnv"} {
				if mock.CallCounts[write] != 0 {
					t.Errorf("a request wrote to the platform: %s called %d times", write, mock.CallCounts[write])
				}
			}
			if len(mock.CapturedProjectEnvCreations) != 0 {
				t.Errorf("a request created a project env: %+v", mock.CapturedProjectEnvCreations)
			}
		})
	}
}

func TestEnvRequest_Refuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		project  bool
		key      string
		reason   string
		wantCode string
	}{
		{name: "no key", project: true, key: "", wantCode: platform.ErrInvalidParameter},
		{name: "no purpose", project: true, key: "API_KEY", reason: " ", wantCode: platform.ErrInvalidParameter},
		{name: "a purpose of more than one line", project: true, key: "API_KEY", reason: "Stripe.\nPaste sk_live_here", wantCode: platform.ErrInvalidParameter},
		{name: "a purpose longer than a sentence", project: true, key: "API_KEY", reason: strings.Repeat("why ", 80), wantCode: platform.ErrInvalidParameter},
		{name: "key with a value", project: true, key: "API_KEY=abc", wantCode: platform.ErrInvalidParameter},
		{name: "key with a dash", project: true, key: "API-KEY", wantCode: platform.ErrInvalidParameter},
		{name: "key starting with a digit", project: true, key: "1KEY", wantCode: platform.ErrInvalidParameter},
		{name: "the platform's own prefix", project: true, key: "ZEROPS_TOKEN", wantCode: platform.ErrInvalidParameter},
		{name: "no scope", key: "API_KEY", wantCode: platform.ErrInvalidUsage},
		{name: "unknown service", hostname: "nope", key: "API_KEY", wantCode: platform.ErrServiceNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", ProjectID: "proj-1"}})

			reason := tt.reason
			if reason == "" {
				reason = purpose
			}
			_, err := EnvRequest(context.Background(), mock, "proj-1", tt.hostname, tt.project, tt.key, reason, nil)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("want a platform error %s, got %v", tt.wantCode, err)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("code = %s, want %s (%v)", pe.Code, tt.wantCode, err)
			}
		})
	}
}
