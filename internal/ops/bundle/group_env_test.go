package bundle

import (
	"maps"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The group recipe composes unattended, with nobody to classify a variable,
// and whatever it writes lands in a repository the whole group reads — the
// broker merges a proposal that only adds files by itself. So the composer
// decides every variable itself, and a secret's value never enters the file.
//
// The platform's sensitive flag is one signal and not the authority: the
// 2026-08 migration read every older service secret back as sensitive:false,
// a project variable's flag never persisted, and ZCP_API_KEY reads false
// (spec-zerops-env-lifecycle §7). So a variable is a secret when the
// platform flags it OR reads it back masked OR its name says credential OR
// its value is shaped like one — and never when its value is only references,
// which carry no secret of their own.
func TestRecipeSecret_Rule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		env          ProjectEnvVar
		wantSecret   bool
		wantExternal bool
	}{
		// Config, kept as written.
		{name: "plain config", env: ProjectEnvVar{Key: "LOG_LEVEL", Value: "debug"}},
		{name: "a URL built on references", env: ProjectEnvVar{Key: "API_URL", Value: "https://medusa-${zeropsSubdomainHost}-9000.prg1.zerops.app"}},
		{name: "an internal URL", env: ProjectEnvVar{Key: "MEDUSA_INTERNAL_URL", Value: "http://medusa:9000"}},
		{name: "an email", env: ProjectEnvVar{Key: "SUPERADMIN_EMAIL", Value: "admin@example.com"}},
		{name: "a name that only ends near a credential word", env: ProjectEnvVar{Key: "CACHE_KEY_PREFIX", Value: "shop"}},
		{name: "a token's lifetime is no token", env: ProjectEnvVar{Key: "TOKEN_TTL", Value: "3600"}},
		{name: "a bypass is no pass", env: ProjectEnvVar{Key: "CACHE_BYPASS", Value: "true"}},

		// References are wiring, whatever the name or the flag says.
		{name: "a reference under a key's name", env: ProjectEnvVar{Key: "STORE_PUBLISHABLE_KEY", Value: "${medusa_CHANNEL_PUBLISHABLE_KEY}"}},
		{name: "a flagged reference", env: ProjectEnvVar{Key: "DATABASE_URL", Value: "${db_connectionString}", Sensitive: true}},
		{name: "references joined by punctuation", env: ProjectEnvVar{Key: "SEARCH_AUTH", Value: "${search_user}:${search_password}"}},
		{name: "a URL whose password is a reference", env: ProjectEnvVar{Key: "DATABASE_URL", Value: "postgresql://${db_user}:${db_password}@${db_hostname}:5432/app"}},

		// A public key is public: a browser bundle bakes it.
		{name: "a publishable key", env: ProjectEnvVar{Key: "STRIPE_PUBLISHABLE_KEY", Value: "pk_live_51Hq0000publishable"}},
		{name: "a NEXT_PUBLIC_ variable", env: ProjectEnvVar{Key: "NEXT_PUBLIC_SEARCH_KEY", Value: "a1b2c3d4e5f6a7b8"}},

		// Secrets, by the platform's word.
		{name: "the platform's sensitive flag", env: ProjectEnvVar{Key: "CUSTOM_SETTING", Value: "opaque", Sensitive: true}, wantSecret: true},
		{name: "a value the platform masked", env: ProjectEnvVar{Key: "ANYTHING", Value: "REDACTED"}, wantSecret: true},

		// Secrets, by their name.
		{name: "_SECRET", env: ProjectEnvVar{Key: "JWT_SECRET", Value: "k3yk3yk3y"}, wantSecret: true},
		{name: "_PASSWORD", env: ProjectEnvVar{Key: "SUPERADMIN_PASSWORD", Value: "correct-horse"}, wantSecret: true},
		{name: "_PASS", env: ProjectEnvVar{Key: "SMTP_PASS", Value: "mailpass"}, wantSecret: true},
		{name: "APP_KEY", env: ProjectEnvVar{Key: "APP_KEY", Value: "0123456789abcdef0123456789abcdef"}, wantSecret: true},
		{name: "SECRET_ opening the name", env: ProjectEnvVar{Key: "SECRET_KEY_BASE", Value: "deadbeef"}, wantSecret: true},
		{name: "_SALT", env: ProjectEnvVar{Key: "HASH_SALT", Value: "pepper"}, wantSecret: true},
		{name: "_CREDENTIALS", env: ProjectEnvVar{Key: "GOOGLE_CREDENTIALS", Value: "{\"type\":\"service_account\"}"}, wantSecret: true},
		{name: "lower case is the same name", env: ProjectEnvVar{Key: "cookie_secret", Value: "c00k1e"}, wantSecret: true},

		// A credential a third party issued: generated, and asked for again.
		{name: "_API_KEY", env: ProjectEnvVar{Key: "STRIPE_API_KEY", Value: "rk_live_abcdef123456"}, wantSecret: true, wantExternal: true},
		{name: "_TOKEN", env: ProjectEnvVar{Key: "GITHUB_TOKEN", Value: "tok"}, wantSecret: true, wantExternal: true},
		{name: "_WEBHOOK_SECRET", env: ProjectEnvVar{Key: "STRIPE_WEBHOOK_SECRET", Value: "whsec_abcdefgh12345678"}, wantSecret: true, wantExternal: true},
		{name: "_CLIENT_SECRET", env: ProjectEnvVar{Key: "GOOGLE_CLIENT_SECRET", Value: "GOCSPX-abc"}, wantSecret: true, wantExternal: true},

		// Secrets, by their shape, whatever the name.
		{name: "a URL carrying a password", env: ProjectEnvVar{Key: "DATABASE_URL", Value: "postgresql://medusa:s3cr3tpass@db:5432/medusa"}, wantSecret: true},
		{name: "a private key", env: ProjectEnvVar{Key: "SIGNING", Value: "-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----"}, wantSecret: true},
		{name: "a JWT", env: ProjectEnvVar{Key: "SERVICE_ROLE", Value: "eyJhbGciOiJIUzI1NiJ9.eyJyb2xlIjoiYWRtaW4ifQ.c2lnbmF0dXJl"}, wantSecret: true},
		{name: "a Stripe secret key", env: ProjectEnvVar{Key: "PAYMENTS", Value: "sk_live_abcdefgh12345678"}, wantSecret: true, wantExternal: true},
		{name: "a GitHub token", env: ProjectEnvVar{Key: "CI", Value: "ghp_abcdefghijklmnopqrstuvwxyz0123456789"}, wantSecret: true, wantExternal: true},
		{name: "an OpenAI key", env: ProjectEnvVar{Key: "LLM", Value: "sk-proj-abcdefghijklmnopqrstuvwx"}, wantSecret: true, wantExternal: true},
		{name: "an AWS access key id", env: ProjectEnvVar{Key: "S3_ID", Value: "AKIAABCDEFGHIJKLMNOP"}, wantSecret: true, wantExternal: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			secret, external := recipeSecret(tt.env)
			if secret != tt.wantSecret || external != tt.wantExternal {
				t.Errorf("recipeSecret(%s=%q, sensitive=%v) = secret %v, external %v; want %v, %v",
					tt.env.Key, tt.env.Value, tt.env.Sensitive, secret, external, tt.wantSecret, tt.wantExternal)
			}
		})
	}
}

// A secret is regenerated in every environment the tier creates, as long as
// the live one — a value's length is often what its reader checks — and never
// shorter than 16. A masked read has no length to keep; an empty secret
// stays empty, since there was nothing to hide.
func TestGeneratedSecret_KeepsTheLength(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, value, want string
	}{
		{"empty stays empty", "", ""},
		{"short grows to 16", "abc", "<@generateRandomString(<16>)>"},
		{"its own length", strings.Repeat("x", 40), "<@generateRandomString(<40>)>"},
		{"characters, not bytes", strings.Repeat("é", 20), "<@generateRandomString(<20>)>"},
		{"never past the preprocessor's 1024", strings.Repeat("x", 3000), "<@generateRandomString(<1024>)>"},
		{"a masked read keeps no length", "REDACTED", "<@generateRandomString(<32>)>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := generatedSecret(tt.value); got != tt.want {
				t.Errorf("generatedSecret(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

// The project's variables go into every tier: config as written, secrets as
// generators under envSecrets, a third party's credential with a line saying
// it has to be set again.
func TestBuildGroupRecipe_ProjectVariables(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.ProjectEnvs = []ProjectEnvVar{
		{Key: "APP_URL", Value: "https://app-${zeropsSubdomainHost}.prg1.zerops.app"},
		{Key: "STORE_PUBLISHABLE_KEY", Value: "${api_CHANNEL_PUBLISHABLE_KEY}"},
		{Key: "SUPERADMIN_EMAIL", Value: "admin@example.com"},
		{Key: "JWT_SECRET", Value: strings.Repeat("j", 48)},
		{Key: "STRIPE_API_KEY", Value: "rk_live_" + strings.Repeat("s", 22)},
		{Key: "STRIPE_WEBHOOK_SECRET", Value: ""},
	}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	for _, tier := range layout.Tiers {
		t.Run(tier.Title, func(t *testing.T) {
			t.Parallel()
			project := mappingValue(tierMapping(t, tier.ImportYAML), "project")
			vars := scalarMap(mappingValue(project, "envVariables"))
			wantVars := map[string]string{
				"APP_URL":               "https://app-${zeropsSubdomainHost}.prg1.zerops.app",
				"STORE_PUBLISHABLE_KEY": "${api_CHANNEL_PUBLISHABLE_KEY}",
				"SUPERADMIN_EMAIL":      "admin@example.com",
			}
			if !maps.Equal(vars, wantVars) {
				t.Errorf("envVariables = %v, want %v", vars, wantVars)
			}
			secrets := scalarMap(mappingValue(project, "envSecrets"))
			wantSecrets := map[string]string{
				"JWT_SECRET":            "<@generateRandomString(<48>)>",
				"STRIPE_API_KEY":        "<@generateRandomString(<30>)>",
				"STRIPE_WEBHOOK_SECRET": "",
			}
			if !maps.Equal(secrets, wantSecrets) {
				t.Errorf("envSecrets = %v, want %v", secrets, wantSecrets)
			}
			if !strings.HasPrefix(tier.ImportYAML, preprocessorHeader) {
				t.Errorf("a generator without the preprocessor's first line")
			}
			comment := keyComment(mappingValue(project, "envSecrets"), "STRIPE_API_KEY")
			if lower := strings.ToLower(comment); !strings.Contains(lower, "set by hand") || !strings.Contains(comment, in.MateProjectName) || !strings.Contains(lower, "set it again") {
				t.Errorf("the third party's key says %q, want that it was set by hand in %s and must be set again", comment, in.MateProjectName)
			}
			if c := keyComment(mappingValue(project, "envSecrets"), "JWT_SECRET"); c != "" {
				t.Errorf("an app's own secret is simply regenerated, yet says %q", c)
			}
		})
	}
}

// A runtime's own variables are service secrets on the platform — the only
// channel an import has for them — so they all go under envSecrets: config
// as written, secrets generated. Each half of the AI Agent tier carries its
// own half's; a group environment runs what the stage half runs.
func TestBuildGroupRecipe_ServiceVariables(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Runtimes[0].ServiceEnvs = []ProjectEnvVar{
		{Key: "NODE_ENV", Value: "development"},
		{Key: "SESSION_SECRET", Value: "REDACTED", Sensitive: true},
	}
	in.Runtimes[0].StageServiceEnvs = []ProjectEnvVar{
		{Key: "NODE_ENV", Value: "production"},
		{Key: "SENDGRID_API_KEY", Value: "SG." + strings.Repeat("a", 22) + "." + strings.Repeat("b", 43)},
	}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	files := groupFiles(t, layout)
	stage := map[string]string{"NODE_ENV": "production", "SENDGRID_API_KEY": "<@generateRandomString(<69>)>"}
	tests := []struct {
		dir, host string
		want      map[string]string
	}{
		{"0 — AI Agent", "apidev", map[string]string{"NODE_ENV": "development", "SESSION_SECRET": "<@generateRandomString(<32>)>"}},
		{"0 — AI Agent", "apistage", stage},
		{"3 — Stage", "api", stage},
		{"4 — Small Production", "api", stage},
	}
	for _, tt := range tests {
		t.Run(tt.dir+"/"+tt.host, func(t *testing.T) {
			t.Parallel()
			services := mappingValue(tierMapping(t, files[tt.dir+"/import.yaml"]), "services")
			var entry *yaml.Node
			for _, item := range services.Content {
				if mappingValue(item, "hostname").Value == tt.host {
					entry = item
				}
			}
			if entry == nil {
				t.Fatalf("no %s", tt.host)
			}
			if got := scalarMap(mappingValue(entry, "envSecrets")); !maps.Equal(got, tt.want) {
				t.Errorf("envSecrets = %v, want %v", got, tt.want)
			}
		})
	}
}

// Whatever a Mate's project holds, the recipe never carries a secret's value:
// every live secret string is absent from every file the proposal writes.
func TestBuildGroupRecipe_ASecretNeverReachesTheRepo(t *testing.T) {
	t.Parallel()
	liveSecrets := map[string]string{
		"JWT_SECRET":          "jwt-live-value-7f3a9c1e5b",
		"COOKIE_SECRET":       "cookie-live-value-2d8e4f6a0c",
		"SUPERADMIN_PASSWORD": "Adm1n-live-P4ssw0rd!",
		"STRIPE_API_KEY":      "rk_live_51LiveStripeValue00000",
		"DATABASE_URL":        "postgresql://medusa:Db-live-P4ss@db:5432/medusa",
		"FLAGGED":             "flagged-live-value-9a8b7c6d",
		"PAYMENTS":            "sk_live_51LiveShapedValue0000",
		"SIGNING_KEY_PEM":     "-----BEGIN PRIVATE KEY-----\nMIIElive\n-----END PRIVATE KEY-----",
	}
	in := groupInputsFixture()
	for key, value := range liveSecrets {
		in.ProjectEnvs = append(in.ProjectEnvs, ProjectEnvVar{Key: key, Value: value, Sensitive: key == "FLAGGED"})
		in.Runtimes[0].ServiceEnvs = append(in.Runtimes[0].ServiceEnvs, ProjectEnvVar{Key: key, Value: value, Sensitive: key == "FLAGGED"})
		in.Runtimes[0].StageServiceEnvs = append(in.Runtimes[0].StageServiceEnvs, ProjectEnvVar{Key: key, Value: value, Sensitive: key == "FLAGGED"})
	}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	for path, body := range groupFiles(t, layout) {
		for key, value := range liveSecrets {
			for _, fragment := range append([]string{value}, strings.Split(value, "\n")...) {
				if len(fragment) >= 6 && strings.Contains(body, fragment) {
					t.Errorf("%s carries the live value of %s (%q)", path, key, fragment)
				}
			}
		}
	}
}

// scalarMap reads a mapping of scalars, nil for no mapping.
func scalarMap(m *yaml.Node) map[string]string {
	if m == nil {
		return nil
	}
	out := map[string]string{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		out[m.Content[i].Value] = m.Content[i+1].Value
	}
	return out
}

// keyComment is the comment written above a key of a mapping.
func keyComment(m *yaml.Node, key string) string {
	if m == nil {
		return ""
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i].HeadComment
		}
	}
	return ""
}
