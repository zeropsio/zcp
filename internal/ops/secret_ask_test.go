// Tests for: internal/ops/secret_ask.go — a Mate asks the person for a
// secret its application's stage or production needs; the card takes the
// value and writes it with the person's access, so nothing here ever holds
// one (docs/spec-mate.md §2.11, §5.8).
package ops

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestProposeSecretAsk_Shape pins the ask Mate's engine opens a card on: the
// key, its vault and environment as Mate's menu names it, the services the
// card restarts, always hidden, never a value.
// witness: internal/ops/secret_ask.go | Environment: SecretAskEnvironment{Kind: env.Kind, ProjectID: env.ProjectID, Name: env.Name}, → Environment: SecretAskEnvironment{Kind: env.Kind, Name: env.Name},
func TestProposeSecretAsk_Shape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   SecretAskInput
		want string
	}{
		{
			name: "a project secret in production, restarting what reads it",
			in:   SecretAskInput{Project: "production", Scope: "project", Key: "AUTH_SECRET", Reason: "v0.1.1 signs sessions with it and production has none", Restart: []string{"api", " api "}},
			want: `{"key":"AUTH_SECRET","label":"Auth secret","why":"v0.1.1 signs sessions with it and production has none","scope":"shared","sensitive":true,"environment":{"kind":"production","projectId":"pProd","name":"Production"},"restart":[{"serviceId":"sApi","hostname":"api"}]}`,
		},
		{
			name: "a service's own secret on stage restarts that service",
			in:   SecretAskInput{Project: "pStage", Scope: "service", Service: "api", Key: "STRIPE_SECRET_KEY", Reason: "checkout needs it"},
			want: `{"key":"STRIPE_SECRET_KEY","label":"Stripe secret key","why":"checkout needs it","scope":"service","serviceHostname":"api","sensitive":true,"environment":{"kind":"stage","projectId":"pStage","name":"Client demo"},"restart":[{"serviceId":"sStageApi","hostname":"api"}]}`,
		},
		{
			name: "nothing named to restart",
			in:   SecretAskInput{Project: "Production", Scope: "project", Key: "AUTH_SECRET", Reason: "r"},
			want: `{"key":"AUTH_SECRET","label":"Auth secret","why":"r","scope":"shared","sensitive":true,"environment":{"kind":"production","projectId":"pProd","name":"Production"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ProposeSecretAsk(context.Background(), newTestApp(), tt.in)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(got.Requested)
			if err != nil {
				t.Fatal(err)
			}
			var gotDoc, wantDoc any
			_ = json.Unmarshal(raw, &gotDoc)
			if err := json.Unmarshal([]byte(tt.want), &wantDoc); err != nil {
				t.Fatalf("bad want: %v", err)
			}
			if !reflect.DeepEqual(gotDoc, wantDoc) {
				t.Errorf("ask\n got %s\nwant %s", raw, tt.want)
			}
			if !strings.Contains(got.NextActions, "never send them to a Vault page") {
				t.Errorf("next actions %q do not keep the person out of the Vault", got.NextActions)
			}
		})
	}
}

// TestProposeSecretAsk_RefusesAValue: a value, a key holding one or a reason
// holding one is refused before anything is observed, and never repeated.
// witness: internal/ops/secret_ask.go | if in.Value != "" { → if false {
func TestProposeSecretAsk_RefusesAValue(t *testing.T) {
	t.Parallel()
	const secret = "s3cr3tV4lu3AbCdEf1234567"
	tests := []struct {
		name string
		in   SecretAskInput
		want string
	}{
		{name: "a value", in: SecretAskInput{Project: "production", Scope: "project", Key: "AUTH_SECRET", Value: secret, Reason: "r"}, want: "never in the call"},
		{name: "KEY=value as the key", in: SecretAskInput{Project: "production", Scope: "project", Key: "AUTH_SECRET=" + secret, Reason: "r"}, want: "not a variable name"},
		{name: "a value in the reason", in: SecretAskInput{Project: "production", Scope: "project", Key: "AUTH_SECRET", Reason: "use " + secret}, want: "looks like a value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app := newTestApp()
			_, err := ProposeSecretAsk(context.Background(), app, tt.in)
			words := refusalWords(t, err)
			if !strings.Contains(words, tt.want) {
				t.Errorf("refusal %q, want %q", words, tt.want)
			}
			if strings.Contains(words, secret) {
				t.Errorf("refusal repeats the value: %q", words)
			}
			if len(app.read) != 0 {
				t.Errorf("observed %v before refusing", app.read)
			}
		})
	}
}

// TestProposeSecretAsk_RefusesWhatIsNotThere: an environment that is not the
// application's, a service it lacks and a scope that does not fit are refused
// in words the agent can act on.
// witness: internal/ops/secret_ask.go | s, err := appService(p, ref) → s, err := &platform.ServiceStack{Name: ref}, error(nil)
func TestProposeSecretAsk_RefusesWhatIsNotThere(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   SecretAskInput
		want string
	}{
		{name: "not the application's", in: SecretAskInput{Project: "elsewhere", Scope: "project", Key: "AUTH_SECRET", Reason: "r"}, want: "not your application's stage or production"},
		{name: "a service production lacks", in: SecretAskInput{Project: "production", Scope: "project", Key: "AUTH_SECRET", Reason: "r", Restart: []string{"web"}}, want: `no service "web"`},
		{name: "service scope without the service", in: SecretAskInput{Project: "production", Scope: "service", Key: "AUTH_SECRET", Reason: "r"}, want: "needs the service"},
		{name: "a name Mate keeps for itself", in: SecretAskInput{Project: "production", Scope: "project", Key: "ZEROPS_TOKEN", Reason: "r"}, want: "ZEROPS_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ProposeSecretAsk(context.Background(), newTestApp(), tt.in)
			if words := refusalWords(t, err); !strings.Contains(words, tt.want) {
				t.Errorf("refusal %q, want %q", words, tt.want)
			}
		})
	}
	app := newTestApp()
	if _, err := ProposeSecretAsk(context.Background(), app, SecretAskInput{Project: "production", Scope: "project", Key: "AUTH_SECRET", Reason: "r"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(app.read, nil) {
		t.Errorf("services read for %v with nothing to restart, want none", app.read)
	}
}
