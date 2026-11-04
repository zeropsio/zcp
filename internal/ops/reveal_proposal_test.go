// Tests for: internal/ops/reveal_proposal.go — a Mate proposes showing the
// person a hidden value it cannot read; the card reveals it on the person's
// screen with their access, and nothing here ever holds a value
// (docs/spec-mate.md §2.11).
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestProposeReveal_Shape pins the env.reveal proposal a Mate's card is drawn
// from: the key and where it lives, named as Mate's menu names the place, and
// never a value.
func TestProposeReveal_Shape(t *testing.T) {
	t.Parallel()
	note := `"note":"` + RevealNote + `"`
	tests := []struct {
		name string
		in   RevealInput
		want string
	}{
		{
			name: "a service's value in production",
			in:   RevealInput{Environment: "production", Service: "db", Key: "DATABASE_PASSWORD", Reason: "You asked for it to connect from your laptop."},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"env.reveal","summary":"Show DATABASE_PASSWORD from Production","reason":"You asked for it to connect from your laptop.","projectId":"pProd","projectName":"Production","serviceId":"sDb","serviceName":"db","params":{"scope":"service","key":"DATABASE_PASSWORD"}},` + note + `}`,
		},
		{
			name: "a project's value on stage, by the stage's id",
			in:   RevealInput{Environment: "stage", Project: "pStage", Key: "STRIPE_SECRET_KEY", Reason: "to compare with your local one"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"env.reveal","summary":"Show STRIPE_SECRET_KEY from Client demo","reason":"to compare with your local one","projectId":"pStage","projectName":"Client demo","params":{"scope":"project","key":"STRIPE_SECRET_KEY"}},` + note + `}`,
		},
		{
			name: "a service's value in this Mate",
			in:   RevealInput{Environment: "mate", Service: "appdev", Key: "JWT_SECRET", Reason: "to sign a test token"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"env.reveal","summary":"Show JWT_SECRET from this Mate","reason":"to sign a test token","projectId":"pMate","projectName":"This Mate","serviceName":"appdev","params":{"scope":"service","key":"JWT_SECRET"}},` + note + `}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ProposeReveal(context.Background(), revealTestApp(), fixedID, "pMate", tt.in)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var gotDoc, wantDoc any
			_ = json.Unmarshal(raw, &gotDoc)
			if err := json.Unmarshal([]byte(tt.want), &wantDoc); err != nil {
				t.Fatalf("bad want: %v", err)
			}
			if !reflect.DeepEqual(gotDoc, wantDoc) {
				t.Errorf("proposal\n got %s\nwant %s", raw, tt.want)
			}
		})
	}
}

// TestProposeReveal_ReadsOnlyWhatItNames: a reveal in this Mate reads nothing
// through HQ; one in production reads production's services only when it
// names a service.
func TestProposeReveal_ReadsOnlyWhatItNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   RevealInput
		want []string
	}{
		{name: "this Mate", in: RevealInput{Environment: "mate", Key: "API_KEY", Reason: "r"}, want: nil},
		{name: "a project value", in: RevealInput{Environment: "production", Key: "API_KEY", Reason: "r"}, want: nil},
		{name: "a service value", in: RevealInput{Environment: "production", Service: "db", Key: "API_KEY", Reason: "r"}, want: []string{"pProd"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app := revealTestApp()
			if _, err := ProposeReveal(context.Background(), app, fixedID, "pMate", tt.in); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(app.read, tt.want) {
				t.Errorf("services read for %v, want %v", app.read, tt.want)
			}
		})
	}
}

// TestProposeReveal_RefusesAValue: a reveal carries a name and a reason, never
// a value — a key written as KEY=value, a reason holding what looks like a
// secret or more than one line, and a name Mate's vault never lists are
// refused before anything is read, and the refusal never repeats the value.
func TestProposeReveal_RefusesAValue(t *testing.T) {
	t.Parallel()
	secret := strings.Join([]string{"hK7q", "2mVz", "P9xR", "4tLw", "8nB3"}, "")
	tests := []struct {
		name string
		in   RevealInput
		want string
	}{
		{name: "a key with its value", in: RevealInput{Environment: "production", Key: "DATABASE_PASSWORD=" + secret, Reason: "r"}, want: "never KEY=value"},
		{name: "a reason with a token in it", in: RevealInput{Environment: "production", Key: "DATABASE_PASSWORD", Reason: "it is " + secret + " I think"}, want: "never a value"},
		{name: "a reason quoting the key's value", in: RevealInput{Environment: "production", Key: "API_KEY", Reason: "API_KEY: abc"}, want: "never a value"},
		{name: "a reason on two lines", in: RevealInput{Environment: "production", Key: "API_KEY", Reason: "to connect\nabc"}, want: "one line"},
		{name: "a name the vault never lists", in: RevealInput{Environment: "mate", Key: "ZCP_TOKEN", Reason: "r"}, want: "keep for themselves"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app := revealTestApp()
			_, err := ProposeReveal(context.Background(), app, fixedID, "pMate", tt.in)
			assertRevealRefusal(t, err, tt.want)
			if strings.Contains(err.Error(), secret) {
				t.Errorf("refusal %q repeats the value", err)
			}
			if app.read != nil || app.envsRead {
				t.Errorf("read HQ before refusing a value")
			}
		})
	}
}

// TestProposeReveal_RefusesWhatIsNotThere: a place other than the
// application's stage, production or this Mate, a service the environment
// lacks, and a reveal with no reason are refused with a plain error.
func TestProposeReveal_RefusesWhatIsNotThere(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   RevealInput
		want string
	}{
		{name: "no environment", in: RevealInput{Key: "API_KEY", Reason: "r"}, want: "stage, production or mate"},
		{name: "another environment", in: RevealInput{Environment: "dev", Key: "API_KEY", Reason: "r"}, want: "stage, production or mate"},
		{name: "a production that is the stage", in: RevealInput{Environment: "production", Project: "pStage", Key: "API_KEY", Reason: "r"}, want: "zerops_observe"},
		{name: "no such service", in: RevealInput{Environment: "production", Service: "cache", Key: "API_KEY", Reason: "r"}, want: "zerops_observe"},
		{name: "no reason", in: RevealInput{Environment: "production", Key: "API_KEY"}, want: "reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ProposeReveal(context.Background(), revealTestApp(), fixedID, "pMate", tt.in)
			assertRevealRefusal(t, err, tt.want)
		})
	}
}

func assertRevealRefusal(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatal("want a refusal")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a platform error the agent can act on", err)
	}
	if text := pe.Message + " " + pe.Suggestion; !strings.Contains(text, want) {
		t.Errorf("refusal %q, want it to mention %q", text, want)
	}
}

// revealApp is testApp that also records whether the environments were read.
type revealApp struct {
	*testApp
	envsRead bool
}

func (a *revealApp) Environments(ctx context.Context) ([]AppEnvironment, error) {
	a.envsRead = true
	return a.testApp.Environments(ctx)
}

func revealTestApp() *revealApp {
	app := newTestApp()
	app.services["pProd"] = append(app.services["pProd"], platform.ServiceStack{ID: "sDb", Name: "db", Status: platform.ServiceStatusActive})
	return &revealApp{testApp: app}
}
