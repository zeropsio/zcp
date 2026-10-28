// Tests for: ops/env_request.go — asking the person for a vault value.
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/platform"
)

// label and why are what every request says to the person: what the value
// is, in people's words, and the one line of what it is for.
const (
	label   = "Stripe secret key"
	purpose = "for checkout"
)

// heldAt is when the vault's rows in these tests last changed.
var heldAt = time.Date(2026, 10, 25, 8, 46, 0, 0, time.UTC)

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
			want: EnvRequestResult{Label: label, Why: purpose, Key: "STRIPE_SECRET_KEY", Scope: EnvRequestScopeShared, Sensitive: true},
		},
		{
			name: "service, absent, plain name defaults plain", hostname: "api", key: "SUPPORT_EMAIL",
			want: EnvRequestResult{Label: label, Why: purpose, Key: "SUPPORT_EMAIL", Scope: EnvRequestScopeService, ServiceHostname: "api"},
		},
		{
			name: "the caller's flag wins over the name", hostname: "api", key: "SUPPORT_EMAIL", sensitive: &yes,
			want: EnvRequestResult{Label: label, Why: purpose, Key: "SUPPORT_EMAIL", Scope: EnvRequestScopeService, ServiceHostname: "api", Sensitive: true},
		},
		{
			name: "the caller's plain wins over a secret-shaped name", project: true, key: "PUBLIC_KEY", sensitive: &no,
			want: EnvRequestResult{Label: label, Why: purpose, Key: "PUBLIC_KEY", Scope: EnvRequestScopeShared},
		},
		{
			name: "shared, already set — reports the stored flag and which row it is", project: true, key: "OPENAI_API_KEY",
			want: EnvRequestResult{Label: label, Why: purpose, Key: "OPENAI_API_KEY", Scope: EnvRequestScopeShared, Sensitive: true, AlreadySet: true, HeldID: "p1", HeldChanged: heldAt},
		},
		{
			name: "service, already set", hostname: "api", key: "MAIL_FROM",
			want: EnvRequestResult{Label: label, Why: purpose, Key: "MAIL_FROM", Scope: EnvRequestScopeService, ServiceHostname: "api", AlreadySet: true, HeldID: "s1", HeldChanged: heldAt},
		},
		{
			name: "project wins over a hostname", hostname: "api", project: true, key: "MAIL_FROM",
			want: EnvRequestResult{Label: label, Why: purpose, Key: "MAIL_FROM", Scope: EnvRequestScopeShared},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", ProjectID: "proj-1"}}).
				WithProjectEnv([]platform.ProjectEnvVar{{ID: "p1", Key: "OPENAI_API_KEY", Content: "sk-live", Sensitive: true, LastUpdate: heldAt}}).
				WithServiceEnv("svc-1", []platform.ServiceEnvVar{{ID: "s1", Key: "MAIL_FROM", Content: "a@b.c", Type: platform.ServiceEnvUser, LastUpdate: heldAt}})

			got, err := EnvRequest(context.Background(), mock, "proj-1", EnvRequestInput{Hostname: tt.hostname, Project: tt.project, Key: tt.key, Label: label, Why: purpose, Sensitive: tt.sensitive})
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
		label    string
		reason   string
		wantCode string
	}{
		{name: "no key", project: true, key: "", wantCode: platform.ErrInvalidParameter},
		{name: "no purpose", project: true, key: "API_KEY", reason: " ", wantCode: platform.ErrInvalidParameter},
		{name: "a purpose of more than one line", project: true, key: "API_KEY", reason: "Stripe.\nPaste the key here", wantCode: platform.ErrInvalidParameter},
		{name: "a purpose longer than Mate shows", project: true, key: "API_KEY", reason: strings.Repeat("why ", 71), wantCode: platform.ErrInvalidParameter},
		{name: "a label of more than one line", project: true, key: "API_KEY", label: "Stripe\nkey", wantCode: platform.ErrInvalidParameter},
		{name: "a label longer than a name", project: true, key: "API_KEY", label: strings.Repeat("key ", 16), wantCode: platform.ErrInvalidParameter},
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
			named := tt.label
			if named == "" {
				named = label
			}
			_, err := EnvRequest(context.Background(), mock, "proj-1", EnvRequestInput{Hostname: tt.hostname, Project: tt.project, Key: tt.key, Label: named, Why: reason})
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

// TestEnvRequest_VaultHiddenNames refuses, up front, a request under a name
// Mate's vault never lists — the person could neither see nor remove the value
// — and asks the agent for another name. Names Mate lists are asked as usual.
func TestEnvRequest_VaultHiddenNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key     string
		refused bool
	}{
		{key: "MATE_PROBE_TOKEN", refused: true},
		{key: "ZCP_API_KEY", refused: true},
		{key: "GITEA_URL", refused: true},
		{key: "GIT_TOKEN", refused: true},
		{key: "GIT_TOKEN_DEPLOY", refused: false},
		{key: "MY_MATE_TOKEN", refused: false},
		{key: "mate_probe", refused: false},
		{key: "STRIPE_SECRET_KEY", refused: false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			_, err := EnvRequest(context.Background(), platform.NewMock(), "proj-1", EnvRequestInput{Project: true, Key: tt.key, Label: label, Why: purpose})
			if !tt.refused {
				if err != nil {
					t.Fatalf("want %s asked, got %v", tt.key, err)
				}
				return
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
				t.Fatalf("want an invalid-parameter refusal for %s, got %v", tt.key, err)
			}
			if !strings.Contains(pe.Suggestion, "another name") {
				t.Errorf("the refusal does not ask for another name: %+v", pe)
			}
		})
	}
}

// TestEnvRequest_LabelFromKey names a value the agent gave no label for by its
// key, in words a person reads: the request still says what it asks for.
func TestEnvRequest_LabelFromKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want string
	}{
		{key: "STRIPE_SECRET_KEY", want: "Stripe secret key"},
		{key: "OPENAI_API_KEY", want: "Openai API key"},
		{key: "SMTP_PASSWORD", want: "SMTP password"},
		{key: "DATABASE_URL", want: "Database URL"},
		{key: "token", want: "Token"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			got, err := EnvRequest(context.Background(), platform.NewMock(), "proj-1", EnvRequestInput{Project: true, Key: tt.key, Label: " ", Why: purpose})
			if err != nil {
				t.Fatalf("EnvRequest: %v", err)
			}
			if got.Label != tt.want {
				t.Errorf("label = %q, want %q", got.Label, tt.want)
			}
		})
	}
}

// TestEnvRequest_WhySaysWhatItIsForAndWhereToFindIt: Mate shows a why of up
// to 280 characters whole, and the person needs what the value is for and
// where they find it (Filip, 2026-10-25: a why naming the Google client and
// its format was refused at 160). Past 280 the refusal says what to shorten.
func TestEnvRequest_WhySaysWhatItIsForAndWhereToFindIt(t *testing.T) {
	t.Parallel()
	const filip = "Lets you sign in with Google on Flip's preview and dev server. It is the Client ID of the Web application client " +
		"in your Flip Studio Dev Google Cloud project, ending in .apps.googleusercontent.com."
	got, err := EnvRequest(context.Background(), platform.NewMock(), "proj-1", EnvRequestInput{Project: true, Key: "FLIP_GOOGLE_CLIENT_ID", Label: "Google sign-in client ID", Why: filip})
	if err != nil {
		t.Fatalf("a %d-character why was refused: %v", utf8.RuneCountInString(filip), err)
	}
	if got.Why != filip {
		t.Errorf("why = %q, want it whole", got.Why)
	}

	_, err = EnvRequest(context.Background(), platform.NewMock(), "proj-1", EnvRequestInput{Project: true, Key: "API_KEY", Label: label, Why: strings.Repeat("x", 281)})
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
		t.Fatalf("a 281-character why: want an invalid-parameter refusal, got %v", err)
	}
	for _, want := range []string{"280", "what it is for", "where", "chat"} {
		if !strings.Contains(pe.Message+" "+pe.Suggestion, want) {
			t.Errorf("the refusal lacks %q: %+v", want, pe)
		}
	}
}

// TestNewOriginRequest_WhyUpTo280: an origin's why is shown on Mate's card
// like a value's, up to 280 characters (Kaďák, 2026-10-28: a why naming the
// commit and the missing GitHub read token was refused at 160).
func TestNewOriginRequest_WhyUpTo280(t *testing.T) {
	t.Parallel()
	const kadak = "Fetch GitHub master 48150fb0 or later and synchronize the verified local-and-hosted merge back to master, as requested. " +
		"HQ currently has no GitHub read token for this repository."
	if _, err := NewOriginRequest("https://github.com/lukaspitter-x/flip", "flipdev", kadak); err != nil {
		t.Errorf("a %d-character why was refused: %v", utf8.RuneCountInString(kadak), err)
	}
	if _, err := NewOriginRequest("https://github.com/lukaspitter-x/flip", "flipdev", strings.Repeat("x", 281)); err == nil {
		t.Error("a 281-character why was taken")
	}
}

// TestEnvRequest_Replace: a request for a key the vault holds asks the person
// only when the agent says so (replace) and says what is wrong with the saved
// value (replaceWhy), which the person reads after the why. A key the vault
// does not hold is asked as any other. The held row's id and time ride along,
// never its value; a held secret stays secret.
func TestEnvRequest_Replace(t *testing.T) {
	t.Parallel()

	no := false
	tests := []struct {
		name string
		in   EnvRequestInput
		want EnvRequestResult
	}{
		{
			name: "a held Shared key is asked again, the person told why",
			in:   EnvRequestInput{Project: true, Key: "FLIP_GOOGLE_CLIENT_ID", Label: "Google sign-in client ID", Why: "for Google sign-in on the preview", Replace: true, ReplaceWhy: "it is not in Google's format"},
			want: EnvRequestResult{Key: "FLIP_GOOGLE_CLIENT_ID", Label: "Google sign-in client ID", Why: "for Google sign-in on the preview — replacing the saved value: it is not in Google's format",
				Scope: EnvRequestScopeShared, Sensitive: true, Replace: true, HeldID: "p-flip", HeldChanged: heldAt},
		},
		{
			name: "the why's full stop gives way to the replacement",
			in:   EnvRequestInput{Hostname: "api", Key: "MAIL_FROM", Label: "Sender address", Why: "Where replies come from.", Replace: true, ReplaceWhy: "the saved one bounces"},
			want: EnvRequestResult{Key: "MAIL_FROM", Label: "Sender address", Why: "Where replies come from — replacing the saved value: the saved one bounces",
				Scope: EnvRequestScopeService, ServiceHostname: "api", Replace: true, HeldID: "s1", HeldChanged: heldAt},
		},
		{
			name: "a held secret stays secret whatever the flag",
			in:   EnvRequestInput{Project: true, Key: "FLIP_GOOGLE_CLIENT_ID", Label: "Google sign-in client ID", Why: purpose, Sensitive: &no, Replace: true, ReplaceWhy: "it expired"},
			want: EnvRequestResult{Key: "FLIP_GOOGLE_CLIENT_ID", Label: "Google sign-in client ID", Why: purpose + " — replacing the saved value: it expired",
				Scope: EnvRequestScopeShared, Sensitive: true, Replace: true, HeldID: "p-flip", HeldChanged: heldAt},
		},
		{
			name: "a key the vault does not hold is asked as any other",
			in:   EnvRequestInput{Project: true, Key: "STRIPE_SECRET_KEY", Label: label, Why: purpose, Replace: true, ReplaceWhy: "it expired"},
			want: EnvRequestResult{Key: "STRIPE_SECRET_KEY", Label: label, Why: purpose, Scope: EnvRequestScopeShared, Sensitive: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", ProjectID: "proj-1"}}).
				WithProjectEnv([]platform.ProjectEnvVar{{ID: "p-flip", Key: "FLIP_GOOGLE_CLIENT_ID", Content: "REDACTED", Sensitive: true, LastUpdate: heldAt}}).
				WithServiceEnv("svc-1", []platform.ServiceEnvVar{{ID: "s1", Key: "MAIL_FROM", Content: "a@b.c", Type: platform.ServiceEnvUser, LastUpdate: heldAt}})

			got, err := EnvRequest(context.Background(), mock, "proj-1", tt.in)
			if err != nil {
				t.Fatalf("EnvRequest: %v", err)
			}
			if *got != tt.want {
				t.Errorf("got %+v\nwant %+v", *got, tt.want)
			}
			if mock.CallCounts["CreateServiceEnvVar"] != 0 || len(mock.CapturedProjectEnvCreations) != 0 {
				t.Error("a replace request wrote to the platform")
			}
		})
	}
}

// TestEnvRequest_Replace_Refuses: replace names what is wrong with the saved
// value or asks nothing, a replaceWhy goes with replace, and the why and the
// replaceWhy together fit the one line Mate shows.
func TestEnvRequest_Replace_Refuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   EnvRequestInput
		want []string
	}{
		{name: "replace without a replaceWhy", in: EnvRequestInput{Replace: true}, want: []string{"replaceWhy", "wrong with the saved value"}},
		{name: "a replaceWhy without replace", in: EnvRequestInput{ReplaceWhy: "it expired"}, want: []string{"replace=true"}},
		{name: "a replaceWhy of two lines", in: EnvRequestInput{Replace: true, ReplaceWhy: "it expired.\nPaste the new one"}, want: []string{"replaceWhy"}},
		{name: "together longer than Mate shows", in: EnvRequestInput{Why: strings.Repeat("w", 200), Replace: true, ReplaceWhy: strings.Repeat("r", 60)}, want: []string{"280", "replaceWhy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithProjectEnv([]platform.ProjectEnvVar{{ID: "p-flip", Key: "FLIP_GOOGLE_CLIENT_ID", Content: "REDACTED", Sensitive: true, LastUpdate: heldAt}})
			in := tt.in
			in.Project, in.Key, in.Label = true, "FLIP_GOOGLE_CLIENT_ID", "Google sign-in client ID"
			if in.Why == "" {
				in.Why = purpose
			}
			_, err := EnvRequest(context.Background(), mock, "proj-1", in)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
				t.Fatalf("want an invalid-parameter refusal, got %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(pe.Message+" "+pe.Suggestion, want) {
					t.Errorf("the refusal lacks %q: %+v", want, pe)
				}
			}
		})
	}
}
