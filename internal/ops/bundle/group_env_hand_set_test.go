package bundle

import (
	"strings"
	"testing"
)

// TestBuildGroupRecipe_HandSetValue_LeftOutAndNamed is flip.studio's Kaďák:
// every tier wrote FLIP_GOOGLE_CLIENT_ID and FLIP_GOOGLE_CLIENT_SECRET, which
// a person set by hand in the Mate, as 32 random characters, and the project
// made from the AI Agent tier answered Google's sign-in with invalid_client
// while its agent worked out that the values were in no format of Google's.
// A random value serves only a secret the app makes and reads itself — a
// signing secret like FLIP_AUTH_SECRET. A value a person set by hand — an
// OAuth client's, an API key, a mail password — no tier carries and none
// makes up: it is left out of the vault it was in, so the environment holds
// no such variable until a person sets it, and the tier's header names it as
// what to set before the environment can work.
func TestBuildGroupRecipe_HandSetValue_LeftOutAndNamed(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.MateProjectName = "flip.studio - Filip"
	in.ProjectEnvs = []ProjectEnvVar{
		{Key: "FLIP_AUTH_SECRET", Value: strings.Repeat("a", 43), Sensitive: true},
		{Key: "FLIP_GOOGLE_CLIENT_ID", Value: "1234567890-" + "abcdefghijklmnop" + ".apps.googleusercontent.com", Sensitive: true},
		{Key: "FLIP_GOOGLE_CLIENT_SECRET", Value: "GOC" + "SPX-" + strings.Repeat("q9", 14), Sensitive: true},
		{Key: "LOG_LEVEL", Value: "info"},
	}
	in.Runtimes[0].Stages[0].ServiceEnvs = []ProjectEnvVar{
		{Key: "NODE_ENV", Value: "production"},
		{Key: "OPENAI_API_KEY", Value: "sk-" + "proj-abcdefghijklmnopqrstuvwx"},
		{Key: "SMTP_PASSWORD", Value: "m4il-Pa55-w0rd"},
	}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	for _, tier := range layout.Tiers {
		t.Run(tier.Title, func(t *testing.T) {
			t.Parallel()
			body := tier.ImportYAML
			stage := "api"
			if tier.Title == "AI Agent" {
				stage = "apistage"
			}
			if got, _ := tierVaultValue(t, body, "", "FLIP_AUTH_SECRET"); got != "<@generateRandomString(<43>)>" {
				t.Errorf("FLIP_AUTH_SECRET = %q, want the app's own secret generated anew", got)
			}
			if got, _ := tierVaultValue(t, body, "", "LOG_LEVEL"); got != "info" {
				t.Errorf("LOG_LEVEL = %q, want it written as it is", got)
			}
			if got, _ := tierVaultValue(t, body, stage, "NODE_ENV"); got != "production" {
				t.Errorf("%s's NODE_ENV = %q, want it written as it is", stage, got)
			}
			for _, key := range []string{"FLIP_GOOGLE_CLIENT_ID", "FLIP_GOOGLE_CLIENT_SECRET"} {
				if got, ok := tierVaultValue(t, body, "", key); ok {
					t.Errorf("the project's %s is written as %q, want it left out", key, got)
				}
			}
			for _, key := range []string{"OPENAI_API_KEY", "SMTP_PASSWORD"} {
				if got, ok := tierVaultValue(t, body, stage, key); ok {
					t.Errorf("%s's %s is written as %q, want it left out", stage, key, got)
				}
			}
			header := tierHeaderText(body)
			want := `Set before this environment can work: FLIP_GOOGLE_CLIENT_ID and FLIP_GOOGLE_CLIENT_SECRET in the project; OPENAI_API_KEY and SMTP_PASSWORD in runtime "` + stage + `". A person set each by hand in the Mate "flip.studio - Filip"; a tier carries no such value and makes none up, so each is left out here.`
			if !strings.Contains(header, want) {
				t.Errorf("header = %q, want it to say %q", header, want)
			}
			if strings.Contains(header, "FLIP_AUTH_SECRET") || strings.Contains(header, "LOG_LEVEL") {
				t.Errorf("header = %q, names a value the tier writes", header)
			}
			if strings.Contains(body, "Set by hand in") {
				t.Errorf("the tier still asks for a value it generated:\n%s", body)
			}
		})
	}
}

// A tier that writes every value has nothing to set first, and says nothing
// of it.
func TestBuildGroupRecipe_NothingHandSet_HeaderSaysNothing(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.ProjectEnvs = []ProjectEnvVar{
		{Key: "JWT_SECRET", Value: strings.Repeat("j", 48)},
		{Key: "LOG_LEVEL", Value: "info"},
	}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	for _, tier := range layout.Tiers {
		if header := tierHeaderText(tier.ImportYAML); strings.Contains(header, "Set before") {
			t.Errorf("%s: header = %q, want nothing to set first", tier.Title, header)
		}
	}
}

// TestBuildGroupRecipe_AUIsOwnSignIn_StaysGenerated: mailpit's MP_UI_AUTH is
// the sign-in of mailpit's own web UI, which shows every mail the app sends —
// sign-up links, password resets — on a public subdomain. It is the
// service's own access credential, not a third party's: any random password
// serves it, and left out, the UI would answer anyone. So a basic-auth pair
// guarding a service's own UI is generated as an app's own secret is, its
// user kept, and never named as one to set first.
func TestBuildGroupRecipe_AUIsOwnSignIn_StaysGenerated(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Utilities = []GroupUtility{{
		Hostname: "mailpit", ServiceType: "alpine@3.21", BuildFromGit: "https://github.com/zerops-recipe-apps/mailpit-app", SubdomainEnabled: true,
		ServiceEnvs: []ProjectEnvVar{{Key: "MP_UI_AUTH", Value: "admin:" + strings.Repeat("p", 24)}},
	}}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	carried := 0
	for _, tier := range layout.Tiers {
		if serviceNodeOrNil(t, tier.ImportYAML, "mailpit") == nil {
			continue
		}
		carried++
		if got, _ := tierVaultValue(t, tier.ImportYAML, "mailpit", "MP_UI_AUTH"); got != "admin:<@generateRandomString(<24>)>" {
			t.Errorf("%s: mailpit's MP_UI_AUTH = %q, want its user kept and its password generated", tier.Title, got)
		}
		if header := tierHeaderText(tier.ImportYAML); strings.Contains(header, "MP_UI_AUTH") {
			t.Errorf("%s: header = %q, names mailpit's own sign-in as one to set first", tier.Title, header)
		}
	}
	if carried == 0 {
		t.Fatal("no tier carries mailpit")
	}
}
