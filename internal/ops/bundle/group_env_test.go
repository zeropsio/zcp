package bundle

import (
	"maps"
	"math/rand/v2"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Invented credentials in their owners' formats, built from parts so the
// repository never holds one whole.
var (
	fakeGitHubToken  = "gh" + "p_" + strings.Repeat("Ab1", 12)
	fakeStripeSecret = "sk_" + "live_" + strings.Repeat("a1", 12)
	fakeStripeKey    = "rk_" + "live_" + "abcdef123456"
	fakeStripeHook   = "wh" + "sec_" + "abcdefgh12345678"
	fakeSendGridKey  = "S" + "G." + strings.Repeat("a", 22) + "." + strings.Repeat("b", 43)
)

// The group recipe composes unattended, with nobody to classify a variable,
// and whatever it writes lands in a repository the whole group reads — the
// Core lands a proposal that only adds files by itself. So the composer
// decides every variable itself, and it fails closed: a value is written as
// it is only when nothing says secret — the platform's flag, a masked read, a
// credential's name, a secret's shape — and it has a narrow config shape.
// Anything else is generated, and every generated value but a secret an app
// makes for itself says it was set by hand and has to be set again.
func TestRecipeSecret_Rule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		env          ProjectEnvVar
		wantSecret   bool
		wantSetAgain bool
	}{
		// Config in a config shape, kept as written.
		{name: "plain config", env: ProjectEnvVar{Key: "LOG_LEVEL", Value: "debug"}},
		{name: "a URL built on references", env: ProjectEnvVar{Key: "API_URL", Value: "https://medusa-${zeropsSubdomainHost}-9000.prg1.zerops.app"}},
		{name: "an internal URL", env: ProjectEnvVar{Key: "MEDUSA_INTERNAL_URL", Value: "http://medusa:9000"}},
		{name: "an email", env: ProjectEnvVar{Key: "SUPERADMIN_EMAIL", Value: "admin@example.com"}},
		{name: "a name that only ends near a credential word", env: ProjectEnvVar{Key: "CACHE_KEY_PREFIX", Value: "shop"}},
		{name: "a token's lifetime is no token", env: ProjectEnvVar{Key: "TOKEN_TTL", Value: "3600"}},
		{name: "a secret's expiry is no secret", env: ProjectEnvVar{Key: "JWT_SECRET_EXPIRES_IN", Value: "7d"}},
		{name: "a password file's path is no password", env: ProjectEnvVar{Key: "DB_PASSWORD_FILE", Value: "/run/secrets/db_password"}},
		{name: "a bypass is no pass", env: ProjectEnvVar{Key: "CACHE_BYPASS", Value: "true"}},
		{name: "a flag list is no token", env: ProjectEnvVar{Key: "NODE_OPTIONS", Value: "--max-old-space-size=4096"}},
		{name: "words joined by dashes are written, not generated", env: ProjectEnvVar{Key: "THEME_NAME", Value: "midnight-blue-with-orange-accents"}},
		{name: "a reference whose default is config", env: ProjectEnvVar{Key: "SMTP_PORT", Value: "${SMTP_PORT_OVERRIDE:-587}"}},
		{name: "JVM options", env: ProjectEnvVar{Key: "JAVA_OPTS", Value: "-Xmx512m -XX:+UseG1GC -XX:MaxRAMPercentage=75.0 -Dfile.encoding=UTF-8"}},
		{name: "node's inspector address", env: ProjectEnvVar{Key: "NODE_OPTIONS", Value: "--enable-source-maps --inspect=0.0.0.0:9229"}},

		// References are wiring, whatever the name or the flag says.
		{name: "a reference under a key's name", env: ProjectEnvVar{Key: "STORE_PUBLISHABLE_KEY", Value: "${medusa_CHANNEL_PUBLISHABLE_KEY}"}},
		{name: "a flagged reference", env: ProjectEnvVar{Key: "DATABASE_URL", Value: "${db_connectionString}", Sensitive: true}},
		{name: "references joined by punctuation", env: ProjectEnvVar{Key: "SEARCH_AUTH", Value: "${search_user}:${search_password}"}},
		{name: "a URL whose password is a reference", env: ProjectEnvVar{Key: "DATABASE_URL", Value: "postgresql://${db_user}:${db_password}@${db_hostname}:5432/app"}},

		// A public name is public: a browser bundle bakes its value anyway.
		{name: "a publishable key", env: ProjectEnvVar{Key: "STRIPE_PUBLISHABLE_KEY", Value: "pk_" + "live_" + "51Hq0000publishable"}},
		{name: "a NEXT_PUBLIC_ variable", env: ProjectEnvVar{Key: "NEXT_PUBLIC_SEARCH_KEY", Value: "a1b2c3d4e5f6a7b8"}},
		{name: "a public key stays public however random", env: ProjectEnvVar{Key: "NEXT_PUBLIC_ANALYTICS_KEY", Value: "phc_q8Zr2xLw7Tn4Vb1Kd9Fs3Hj6Mc0Pa5Ye"}},
		{name: "a public token shaped like a JWT", env: ProjectEnvVar{Key: "NEXT_PUBLIC_MAPBOX_TOKEN", Value: "pk.ey" + "J1IjoiYWNtZSJ9.c2lnbmF0dXJl"}},

		// Unless the name says secret beside it, or the value is a key.
		{name: "a public bucket's secret key", env: ProjectEnvVar{Key: "S3_PUBLIC_BUCKET_SECRET_KEY", Value: "q8Zr2xLw7Tn4Vb1Kd9Fs3Hj6Mc0Pa5Ye"}, wantSecret: true, wantSetAgain: true},
		{name: "a public CDN's private signing key", env: ProjectEnvVar{Key: "CDN_PUBLIC_SIGNING_PRIVATE_KEY", Value: "-----BEGIN " + "PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----"}, wantSecret: true, wantSetAgain: true},
		{name: "a public name with a password in it", env: ProjectEnvVar{Key: "NEXT_PUBLIC_ADMIN_PASSWORD", Value: "letmein"}, wantSecret: true},
		{name: "a vendor's secret key under a public name", env: ProjectEnvVar{Key: "NEXT_PUBLIC_STRIPE_KEY", Value: fakeStripeSecret}, wantSecret: true, wantSetAgain: true},
		{name: "a private key under a public name", env: ProjectEnvVar{Key: "NEXT_PUBLIC_CERT", Value: "-----BEGIN RSA " + "PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----"}, wantSecret: true, wantSetAgain: true},
		{name: "PUBLICATION is no PUBLIC", env: ProjectEnvVar{Key: "PUBLICATION_TOKEN", Value: "abc"}, wantSecret: true, wantSetAgain: true},

		// The platform's word outranks every other signal but wiring.
		{name: "the platform's sensitive flag", env: ProjectEnvVar{Key: "CUSTOM_SETTING", Value: "opaque", Sensitive: true}, wantSecret: true, wantSetAgain: true},
		{name: "a value the platform masked", env: ProjectEnvVar{Key: "ANYTHING", Value: "REDACTED"}, wantSecret: true, wantSetAgain: true},
		{name: "a flagged reference with a default carries the default", env: ProjectEnvVar{Key: "DB_PASSWORD", Value: "${DB_PASSWORD:-Sup3rS3cret}", Sensitive: true}, wantSecret: true},
		{name: "a flagged public name is flagged", env: ProjectEnvVar{Key: "NEXT_PUBLIC_FLAG", Value: "beta", Sensitive: true}, wantSecret: true, wantSetAgain: true},
		{name: "a masked public name has no value to write", env: ProjectEnvVar{Key: "NEXT_PUBLIC_FLAG", Value: "REDACTED"}, wantSecret: true, wantSetAgain: true},

		// Secrets by their name, whatever the value looks like: every word a
		// credential goes by, a qualifier after it too.
		{name: "_SECRET", env: ProjectEnvVar{Key: "JWT_SECRET", Value: "k3yk3yk3y"}, wantSecret: true},
		{name: "_PASSWORD", env: ProjectEnvVar{Key: "SUPERADMIN_PASSWORD", Value: "correct-horse"}, wantSecret: true},
		{name: "_PASS", env: ProjectEnvVar{Key: "SMTP_PASS", Value: "mailpass"}, wantSecret: true},
		{name: "APP_KEY", env: ProjectEnvVar{Key: "APP_KEY", Value: "0123456789abcdef0123456789abcdef"}, wantSecret: true},
		{name: "SECRET_ opening the name", env: ProjectEnvVar{Key: "SECRET_KEY_BASE", Value: "deadbeef"}, wantSecret: true},
		{name: "_SALT", env: ProjectEnvVar{Key: "HASH_SALT", Value: "pepper"}, wantSecret: true},
		{name: "_CREDENTIALS", env: ProjectEnvVar{Key: "GOOGLE_CREDENTIALS", Value: "{\"type\":\"service_account\"}"}, wantSecret: true, wantSetAgain: true},
		{name: "lower case is the same name", env: ProjectEnvVar{Key: "cookie_secret", Value: "c00k1e"}, wantSecret: true},
		{name: "a plural", env: ProjectEnvVar{Key: "APP_KEYS", Value: "alpha,beta"}, wantSecret: true},
		{name: "a passphrase in words", env: ProjectEnvVar{Key: "GPG_PASSPHRASE", Value: "correct horse battery staple"}, wantSecret: true},
		{name: "PW", env: ProjectEnvVar{Key: "ADMIN_PW", Value: "letmein"}, wantSecret: true, wantSetAgain: true},
		{name: "a qualifier after the credential word", env: ProjectEnvVar{Key: "DB_PASSWORD_PROD", Value: "hunter"}, wantSecret: true, wantSetAgain: true},
		{name: "camel case", env: ProjectEnvVar{Key: "jwtSecret", Value: "mysecret"}, wantSecret: true},
		{name: "a credential word run into the one before it", env: ProjectEnvVar{Key: "DBPASSWORD", Value: "hunter"}, wantSecret: true},

		// What no shape tells from a hostname or a word — a one-case run of
		// letters, a passphrase, a PIN — its name tells.
		{name: "a passcode", env: ProjectEnvVar{Key: "ADMIN_PASSCODE", Value: "Saddle-Piano8-Velvet-Silver"}, wantSecret: true, wantSetAgain: true},
		{name: "a recovery phrase", env: ProjectEnvVar{Key: "RECOVERY_PHRASE", Value: "correct horse battery staple"}, wantSecret: true, wantSetAgain: true},
		{name: "a mnemonic", env: ProjectEnvVar{Key: "WALLET_MNEMONIC", Value: "zoo ask axis bag bus cat cow dog egg era eye fan"}, wantSecret: true, wantSetAgain: true},
		{name: "a PIN", env: ProjectEnvVar{Key: "ADMIN_PIN", Value: "482913"}, wantSecret: true, wantSetAgain: true},
		{name: "a seed", env: ProjectEnvVar{Key: "OTP_SEED", Value: "JBSWYDPEHPKXPXPA"}, wantSecret: true, wantSetAgain: true},
		{name: "SECRETKEY run together", env: ProjectEnvVar{Key: "SECRETKEY", Value: "kqzvbxwmtrplhdfg"}, wantSecret: true, wantSetAgain: true},
		{name: "MASTERKEY run together", env: ProjectEnvVar{Key: "MASTERKEY", Value: "kqzvbxwmtrplhdfg"}, wantSecret: true, wantSetAgain: true},
		{name: "ACCESSKEY run together", env: ProjectEnvVar{Key: "S3_ACCESSKEY", Value: "kqzvbxwmtrplhdfg"}, wantSecret: true, wantSetAgain: true},
		{name: "AUTHTOKEN run together", env: ProjectEnvVar{Key: "TWILIO_AUTHTOKEN", Value: "kqzvbxwmtrplhdfg"}, wantSecret: true, wantSetAgain: true},
		{name: "a webhook's address is its credential", env: ProjectEnvVar{Key: "SLACK_WEBHOOK_URL", Value: "https://hooks." + "slack.com/services/TABCDEFGH/BABCDEFGH/kqzvbxwmtrplhdfgnjcsyaeu"}, wantSecret: true, wantSetAgain: true},

		// A code is no passcode, a boolean hides nothing, and a word about a
		// credential names none.
		{name: "a zip code", env: ProjectEnvVar{Key: "ZIP_CODE", Value: "11000"}},
		{name: "a country code", env: ProjectEnvVar{Key: "COUNTRY_CODE", Value: "CZ"}},
		{name: "a boolean under a credential's name", env: ProjectEnvVar{Key: "DB_SEED", Value: "true"}},
		{name: "a PIN's length", env: ProjectEnvVar{Key: "PIN_LENGTH", Value: "6"}},
		{name: "a webhook's route here", env: ProjectEnvVar{Key: "GITHUB_WEBHOOK_PATH", Value: "/hooks/github"}},

		// A query is config only when every key names no credential and every
		// value is a word, a number or a reference.
		{name: "a query key naming a password", env: ProjectEnvVar{Key: "DB_URL", Value: "postgresql://db:5432/app?password=hunter"}, wantSecret: true, wantSetAgain: true},
		{name: "a query key naming an API key", env: ProjectEnvVar{Key: "GEO_URL", Value: "https://geo.example.com/v1?api_key=abc"}, wantSecret: true, wantSetAgain: true},
		{name: "a query key naming a token", env: ProjectEnvVar{Key: "FEED_URL", Value: "https://feeds.example.com/v1?token=abc"}, wantSecret: true, wantSetAgain: true},
		{name: "a query key naming a key", env: ProjectEnvVar{Key: "MAPS_URL", Value: "https://maps.example.com/js?key=abc"}, wantSecret: true, wantSetAgain: true},
		{name: "a query key naming a secret", env: ProjectEnvVar{Key: "HOOK_URL", Value: "https://in.example.com/x?secret=abc"}, wantSecret: true, wantSetAgain: true},
		{name: "a query key naming a signature", env: ProjectEnvVar{Key: "EXPORT_URL", Value: "https://files.example.com/export?Signature=abc"}, wantSecret: true, wantSetAgain: true},
		{name: "a query key naming a sig", env: ProjectEnvVar{Key: "BLOB_URL", Value: "https://acme.blob.example.net/c?sv=2024&sig=abc"}, wantSecret: true, wantSetAgain: true},
		{name: "a query with a reference", env: ProjectEnvVar{Key: "APP_LINK", Value: "https://app.example.com/?tenant=${TENANT_NAME}"}},
		{name: "a fragment is no config", env: ProjectEnvVar{Key: "APP_LINK", Value: "https://app.example.com/#access_token=abc"}, wantSecret: true, wantSetAgain: true},

		// A credential somebody else issued: generated, and asked for again.
		{name: "_API_KEY", env: ProjectEnvVar{Key: "STRIPE_API_KEY", Value: fakeStripeKey}, wantSecret: true, wantSetAgain: true},
		{name: "_TOKEN", env: ProjectEnvVar{Key: "GITHUB_TOKEN", Value: "tok"}, wantSecret: true, wantSetAgain: true},
		{name: "_WEBHOOK_SECRET, though it ends in SECRET", env: ProjectEnvVar{Key: "STRIPE_WEBHOOK_SECRET", Value: fakeStripeHook}, wantSecret: true, wantSetAgain: true},
		{name: "_CLIENT_SECRET, though it ends in SECRET", env: ProjectEnvVar{Key: "GOOGLE_CLIENT_SECRET", Value: "GOC" + "SPX-abc"}, wantSecret: true, wantSetAgain: true},
		{name: "a vendor's format under an app's own name", env: ProjectEnvVar{Key: "PAYMENTS_SECRET", Value: fakeStripeSecret}, wantSecret: true, wantSetAgain: true},

		// Secrets by their shape, whatever the name.
		{name: "a URL carrying a password", env: ProjectEnvVar{Key: "DATABASE_URL", Value: "postgresql://medusa:s3cr3tpass@db:5432/medusa"}, wantSecret: true, wantSetAgain: true},
		{name: "a private key", env: ProjectEnvVar{Key: "SIGNING", Value: "-----BEGIN " + "PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----"}, wantSecret: true, wantSetAgain: true},
		{name: "a JWT", env: ProjectEnvVar{Key: "SERVICE_ROLE", Value: "ey" + "JhbGciOiJIUzI1NiJ9.ey" + "Jyb2xlIjoiYWRtaW4ifQ.c2lnbmF0dXJl"}, wantSecret: true, wantSetAgain: true},
		{name: "a Stripe secret key", env: ProjectEnvVar{Key: "PAYMENTS", Value: fakeStripeSecret}, wantSecret: true, wantSetAgain: true},
		{name: "a GitHub token", env: ProjectEnvVar{Key: "CI", Value: fakeGitHubToken}, wantSecret: true, wantSetAgain: true},
		{name: "an OpenAI key", env: ProjectEnvVar{Key: "LLM", Value: "sk-" + "proj-abcdefghijklmnopqrstuvwx"}, wantSecret: true, wantSetAgain: true},
		{name: "an AWS access key id", env: ProjectEnvVar{Key: "S3_ID", Value: "AK" + "IAABCDEFGHIJKLMNOP"}, wantSecret: true, wantSetAgain: true},

		// A flag is made of flag words, and names no credential: a generated
		// token opening with a dash is no flag, and a line break ends a list.
		{name: "a base64url token opening with a dash", env: ProjectEnvVar{Key: "COOKIE_SIGNING", Value: "-" + "Xq9rT2pLm9Wn4Xc6Yb1Hd0Fs5Jg7Kh2Nc4Vx8Qa1Ze"}, wantSecret: true, wantSetAgain: true},
		{name: "a flag naming a password PWD", env: ProjectEnvVar{Key: "JAVA_OPTS", Value: "-Xmx512m -Dspring.datasource.pwd=hunter2"}, wantSecret: true, wantSetAgain: true},
		{name: "a flag naming credentials", env: ProjectEnvVar{Key: "JAVA_TOOL_OPTIONS", Value: "-Dapp.credentials=Summer24"}, wantSecret: true, wantSetAgain: true},
		{name: "a flag naming a password PW", env: ProjectEnvVar{Key: "NODE_OPTIONS", Value: "--db-pw=letmein"}, wantSecret: true, wantSetAgain: true},
		{name: "flags naming a salt and creds", env: ProjectEnvVar{Key: "CATALINA_OPTS", Value: "-Dsalt=pepperpot -Dcreds=opensesame"}, wantSecret: true, wantSetAgain: true},
		{name: "a line break is no flag list", env: ProjectEnvVar{Key: "EXTRA_ARGS", Value: "-v\nhunter2"}, wantSecret: true, wantSetAgain: true},

		// Anything outside a config shape is generated: an opaque value under
		// an ordinary name fails closed, and a person sets it again.
		{name: "a random value under an ordinary name", env: ProjectEnvVar{Key: "SIGNING_SEED", Value: "q8Zr2xLw7Tn4Vb1Kd9Fs3Hj6Mc0Pa5Ye"}, wantSecret: true, wantSetAgain: true},
		{name: "a hex digest reads as generated", env: ProjectEnvVar{Key: "RELEASE_SHA", Value: "3f2a9c1e5b7d4f608a1c3e5f7b9d2a4c6e8f0a1b"}, wantSecret: true, wantSetAgain: true},
		{name: "an opaque ID", env: ProjectEnvVar{Key: "STRIPE_PRICE_PRO", Value: "price_" + "1Mq7Xz2Lb9Rt4Wv8Kd3Nc6Hs"}, wantSecret: true, wantSetAgain: true},

		// An empty value hides nothing: a secret's name keeps it a secret, and
		// there is nothing to set again.
		{name: "an empty credential", env: ProjectEnvVar{Key: "STRIPE_API_KEY", Value: ""}, wantSecret: true},
		{name: "an empty setting", env: ProjectEnvVar{Key: "FEATURE_FLAGS", Value: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			secret, setAgain := recipeSecret(tt.env)
			if secret != tt.wantSecret || setAgain != tt.wantSetAgain {
				t.Errorf("recipeSecret(%s=%q, sensitive=%v) = secret %v, set again %v; want %v, %v",
					tt.env.Key, tt.env.Value, tt.env.Sensitive, secret, setAgain, tt.wantSecret, tt.wantSetAgain)
			}
		})
	}
}

// Every value that leaked before the rule failed closed is generated now,
// its live text in no file of the proposal — and the config a person wrote
// is still written as it is, on every tier, as a project variable and as a
// runtime's own.
func TestBuildGroupRecipe_FailsClosed(t *testing.T) {
	t.Parallel()
	slackHook := "https://hooks." + "slack.com/services/" + "T0" + "4BX9RQ2LD/B0" + "5CM7WX1KE/" + strings.Repeat("Zk3v", 6)
	azureKey := strings.Repeat("Zm9v", 21) + "YmE="
	googleSecret := "GOC" + "SPX-" + strings.Repeat("q9", 14)
	npmToken := "np" + "m_" + strings.Repeat("Xy7", 12)
	pgpBody := "lQOYBGZk3vQ8rT2pLm9Wn4Xc6Yb1Hd0Fs5Jg7Kh2Nc4Vx8Qa1Ze3Rb6Tf9Ug"
	tests := []struct {
		name, key, value string
		// secret is what no file may carry; the whole value when empty.
		secret    string
		sensitive bool
		verbatim  bool
	}{
		// What leaked before.
		{name: "Strapi's APP_KEYS", key: "APP_KEYS", value: "ZJ4/nKcrXq3bT9sLw2Vm8pYd1g==,uQ1NhR7cWk5eJ0zFa6Ts4Ob3iA=="},
		{name: "a Slack webhook", key: "SLACK_WEBHOOK_URL", value: slackHook},
		{name: "a DSN whose user is a key", key: "MAILER_DSN", value: "sendgrid+api://" + fakeSendGridKey + "@default", secret: fakeSendGridKey},
		{name: "a clone URL whose user is a token", key: "THEME_REPO", value: "https://" + fakeGitHubToken + "@github.com/acme/theme", secret: fakeGitHubToken},
		{name: "a password in a query", key: "REPORTS_DB_URL", value: "postgresql://reports:5432/app?password=Rep0rts-Pa55", secret: "Rep0rts-Pa55"},
		{name: "a key in a query", key: "GEOCODER_URL", value: "https://geo.example.com/v1?api_key=k3y-9f8e7d6c5b4a", secret: "k3y-9f8e7d6c5b4a"},
		{name: "a .NET connection string", key: "ConnectionStrings__Default", value: "Server=db;Database=app;User Id=sa;" + "Pass" + "word=Str0ng-Pa55;", secret: "Str0ng-Pa55"},
		{name: "an Azure connection string", key: "AZURE_STORAGE_CONNECTION", value: "DefaultEndpointsProtocol=https;AccountName=acmefiles;Account" + "Key=" + azureKey + ";EndpointSuffix=core.windows.net", secret: azureKey},
		{name: "a passphrase", key: "GPG_PASSPHRASE", value: "correct horse battery staple"},
		{name: "a short password's name", key: "ADMIN_PW", value: "letmein"},
		{name: "a password's name with a qualifier after it", key: "DB_PASSWORD_PROD", value: "hunter"},
		{name: "an OAuth client document", key: "GOOGLE_OAUTH_CLIENT", value: `{"web":{"client_id":"1234-abc.apps.googleusercontent.com","client_secret":"` + googleSecret + `"}}`, secret: googleSecret},
		{name: "a bearer header", key: "UPSTREAM_AUTHORIZATION", value: "Bearer " + fakeGitHubToken, secret: fakeGitHubToken},
		{name: "an .npmrc line", key: "NPM_CONFIG_LINE", value: "//registry.npmjs.org/:_auth" + "Token=" + npmToken, secret: npmToken},
		{name: "a password among JVM options", key: "JAVA_TOOL_OPTIONS", value: "-Xmx512m -Dspring.datasource." + "password=Jvm-Pa55-w0rd", secret: "Jvm-Pa55-w0rd"},
		{name: "a PGP private key block", key: "RELEASE_SIGNER", value: "-----BEGIN PGP " + "PRIVATE KEY BLOCK-----\n\n" + pgpBody + "\n-----END PGP PRIVATE KEY BLOCK-----", secret: pgpBody},
		{name: "a whole dotenv file", key: "DOTENV", value: "LOG_LEVEL=info\nMAIL_" + "PASSWORD=m4il-Pa55-w0rd", secret: "m4il-Pa55-w0rd"},
		{name: "random hex, 24 characters", key: "INSTANCE_SEED", value: "5f1d9a3c7e2b4f80a6c1d3e5"},
		{name: "random hex, 32 characters", key: "LICENSE_ID", value: "9c2e4a6b8d0f1e3c5a7b9d1f3e5c7a9b"},
		{name: "a UUID", key: "TENANT_ID", value: "3b9e5c1a-7d2f-4e8b-9a6c-0f1e2d3c4b5a"},
		{name: "a flagged reference whose default is a password", key: "DB_PASSWORD", value: "${DB_PASSWORD:-Sup3rS3cret}", secret: "Sup3rS3cret", sensitive: true},
		{name: "a reference whose default is a URL with a password", key: "CACHE_URL", value: "${CACHE_OVERRIDE:-redis://:Cach3-Pa55@cache:6379}", secret: "Cach3-Pa55"},
		{name: "a wired URL whose password defaults to a word", key: "QUEUE_URL", value: "amqp://${queue_user}:${QUEUE_PASS:-marmalade}@queue:5672", secret: "marmalade"},
		{name: "an opaque ID", key: "STRIPE_PRICE_PRO", value: "price_" + "1Mq7Xz2Lb9Rt4Wv8Kd3Nc6Hs"},
		{name: "a token in a query", key: "FEED_URL", value: "https://feeds.example.com/v1?token=feedtoken", secret: "feedtoken"},
		{name: "a signed URL", key: "EXPORT_URL", value: "https://files.example.com/export?expires=1727712000&sig=sigvalue", secret: "sigvalue"},
		{name: "a random value in a query", key: "REPORT_URL", value: "https://reports.example.com/v1?session=q8Zr2xLw7Tn4Vb1Kd9Fs", secret: "q8Zr2xLw7Tn4Vb1Kd9Fs"},
		{name: "a bare user and password beside a star", key: "BASIC_LOGIN", value: "admin:hunter*", secret: "hunter"},
		{name: "a user and password shaped like an image", key: "SMTP_LOGIN", value: "admin:hunter", secret: "hunter"},
		{name: "a dash-led base64url seed", key: "COOKIE_SIGNING", value: "-" + "Xq9rT2pLm9Wn4Xc6Yb1Hd0Fs5Jg7Kh2Nc4Vx8Qa1Ze"},
		{name: "an access key id before a colon", key: "AWS_CREDENTIALS", value: "AK" + "IAIOSFODNN7EXAMPLE:" + "wJalrXUtnFEMI/K7MDENG/bPxRfiCYzq8Lw2Vm", secret: "AK" + "IAIOSFODNN7EXAMPLE"},
		{name: "a token before a colon", key: "GITLAB_AUTH", value: "gl" + "pat-Xq9rT2pLm9Wn4Xc6Yb1H:x-oauth-basic", secret: "gl" + "pat-Xq9rT2pLm9Wn4Xc6Yb1H"},
		{name: "a passcode in words", key: "ADMIN_PASSCODE", value: "Saddle-Piano8-Velvet-Silver"},
		{name: "a PIN", key: "ADMIN_PIN", value: "482913"},
		{name: "a one-case seed", key: "SESSION_SEED", value: "kqzvbxwmtrplhdfgnjcsyaeu"},
		{name: "a password among JVM options, abbreviated", key: "JAVA_OPTS", value: "-Xmx512m -Dspring.datasource." + "pwd=Jvm-Pa55-w0rd", secret: "Jvm-Pa55-w0rd"},
		{name: "a public bucket's secret key", key: "S3_PUBLIC_BUCKET_SECRET_KEY", value: "wJalrXUtnFEMI" + "/K7MDENG/bPxRfiCYzq8Lw2Vm"},
		{name: "a public CDN's private key", key: "CDN_PUBLIC_SIGNING_PRIVATE_KEY", value: "-----BEGIN " + "PRIVATE KEY-----\n" + pgpBody + "\n-----END PRIVATE KEY-----", secret: pgpBody},

		// The medusa fixture's config, as a person wrote it.
		{name: "medusa's storefront address", key: "APP_URL", value: "https://nextstorestage-${zeropsSubdomainHost}-8000.prg1.zerops.app", verbatim: true},
		{name: "medusa's backend address", key: "API_URL", value: "https://medusastage-${zeropsSubdomainHost}-9000.prg1.zerops.app", verbatim: true},
		{name: "medusa's backend inside the project", key: "MEDUSA_INTERNAL_URL", value: "http://medusastage:9000", verbatim: true},
		{name: "medusa's storefront inside the project", key: "STOREFRONT_INTERNAL_URL", value: "http://nextstorestage:8000", verbatim: true},
		{name: "medusa's publishable key, wired", key: "STORE_PUBLISHABLE_KEY", value: "${medusastage_CHANNEL_PUBLISHABLE_KEY}", verbatim: true},
		{name: "medusa's admin email", key: "SUPERADMIN_EMAIL", value: "admin@example.com", verbatim: true},
		{name: "medusa's worker mode", key: "MEDUSA_WORKER_MODE", value: "server", verbatim: true},
		{name: "medusa's admin CORS", key: "ADMIN_CORS", value: "https://medusastage-${zeropsSubdomainHost}-9000.prg1.zerops.app", verbatim: true},

		// Settings the platform's recipes carry.
		{name: "node's flags", key: "NODE_OPTIONS", value: "--max-old-space-size=4096", verbatim: true},
		{name: "a time zone", key: "TZ", value: "Europe/Prague", verbatim: true},
		{name: "an API's versioned address", key: "API_BASE_URL", value: "https://api.example.com/v1", verbatim: true},
		{name: "a runtime's address inside the project", key: "MEDUSA_BACKEND_URL", value: "http://medusastage:9000", verbatim: true},
		{name: "a database URL wired by references", key: "DATABASE_URL", value: "postgresql://${db_user}:${db_password}@${db_hostname}:5432/app", verbatim: true},
		{name: "an object storage's region", key: "S3_REGION", value: "us-east-1", verbatim: true},
		{name: "a listen address", key: "HOST", value: "0.0.0.0", verbatim: true},
		{name: "a list of origins", key: "STORE_CORS", value: "http://localhost:8000,https://docs.medusajs.com", verbatim: true},
		{name: "a reference with a port for its default", key: "SMTP_PORT", value: "${SMTP_PORT_OVERRIDE:-587}", verbatim: true},
		{name: "a table prefix", key: "WORDPRESS_TABLE_PREFIX", value: "wp_", verbatim: true},
		{name: "a memory limit", key: "PHP_INI_memory_limit", value: "512M", verbatim: true},
		{name: "a version", key: "UMAMI_RELEASE_TAG", value: "v3.0.1", verbatim: true},
		{name: "a seeding switch", key: "DB_SEED", value: "true", verbatim: true},
		{name: "any origin", key: "CORS_ORIGIN", value: "*", verbatim: true},
		{name: "a debug scope", key: "DEBUG", value: "medusa:*", verbatim: true},
		{name: "debug scopes", key: "DEBUG_SCOPES", value: "medusa:*,express:*", verbatim: true},
		{name: "a wildcard host", key: "ALLOWED_ORIGIN", value: "https://*.example.com", verbatim: true},
		{name: "a cron line", key: "BACKUP_CRON", value: "0 3 * * *", verbatim: true},
		{name: "a cron line with steps, ranges and names", key: "REPORT_SCHEDULE", value: "*/15 9-17 * * MON-FRI", verbatim: true},
		{name: "a cron nickname", key: "CLEANUP_SCHEDULE", value: "@daily", verbatim: true},
		{name: "a mailbox", key: "EMAIL_FROM", value: "Acme Shop <noreply@acme.example>", verbatim: true},
		{name: "an image on a registry", key: "WORKER_IMAGE", value: "ghcr.io/acme/worker:1.2.3", verbatim: true},
		{name: "an official image", key: "BASE_IMAGE", value: "node:22-alpine", verbatim: true},
		{name: "a database URL with a query", key: "DATABASE_URL", value: "postgresql://${db_user}:${db_password}@${db_hostname}:5432/app?sslmode=require", verbatim: true},
		{name: "a query of several settings", key: "MONGO_URL", value: "mongodb://${mongo_hostname}:27017/app?authSource=admin&retryWrites=true&w=1", verbatim: true},
		{name: "a zip code", key: "ZIP_CODE", value: "11000", verbatim: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := ProjectEnvVar{Key: tt.key, Value: tt.value, Sensitive: tt.sensitive}
			in := groupInputsFixture()
			in.ProjectEnvs = []ProjectEnvVar{env}
			in.Runtimes[0].ServiceEnvs = []ProjectEnvVar{env}
			in.Runtimes[0].StageServiceEnvs = []ProjectEnvVar{env}
			layout, _, err := BuildGroupRecipe(in)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			for _, tier := range layout.Tiers {
				project := mappingValue(tierMapping(t, tier.ImportYAML), "project")
				written := map[string]string{
					"project": scalarMap(mappingValue(project, "envVariables"))[tt.key] + scalarMap(mappingValue(project, "envSecrets"))[tt.key],
				}
				for _, host := range []string{"apidev", "apistage", "api"} {
					if service := serviceNodeOrNil(t, tier.ImportYAML, host); service != nil {
						written[host] = scalarMap(mappingValue(service, "envSecrets"))[tt.key]
					}
				}
				for where, got := range written {
					switch {
					case tt.verbatim && got != tt.value:
						t.Errorf("%s: %s's %s = %q, want it written as it is", tier.Title, where, tt.key, got)
					case !tt.verbatim && !strings.Contains(got, "<@generateRandomString("):
						t.Errorf("%s: %s's %s = %q, want it generated", tier.Title, where, tt.key, got)
					}
				}
			}
			if tt.verbatim {
				return
			}
			secret := tt.secret
			if secret == "" {
				secret = tt.value
			}
			for path, body := range groupFiles(t, layout) {
				if strings.Contains(body, secret) {
					t.Errorf("%s carries the live value of %s", path, tt.key)
				}
			}
		})
	}
}

// A value a generator made — random hex, a UUID, base62, base64 — is never a
// config shape, whatever it is called: the rule does not guess at randomness,
// it writes as it is only what a person writes a setting as. The guess it
// replaced missed 55% of 24-character hex and 19% of 32-character.
func TestRecipeSecret_RandomValuesAreGenerated(t *testing.T) {
	t.Parallel()
	const (
		hex       = "0123456789abcdef"
		base62    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
		base64    = base62 + "+/"
		base64url = base62 + "-_"
	)
	rng := rand.New(rand.NewPCG(20260930, 1)) //nolint:gosec // a seeded generator keeps the samples the same on every run
	random := func(alphabet string, n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = alphabet[rng.IntN(len(alphabet))]
		}
		return string(b)
	}
	shapes := []struct {
		name string
		make func() string
	}{
		{"hex, 16 characters", func() string { return random(hex, 16) }},
		{"hex, 24 characters", func() string { return random(hex, 24) }},
		{"hex, 32 characters", func() string { return random(hex, 32) }},
		{"hex, 40 characters", func() string { return random(hex, 40) }},
		{"a UUID", func() string {
			return random(hex, 8) + "-" + random(hex, 4) + "-" + random(hex, 4) + "-" + random(hex, 4) + "-" + random(hex, 12)
		}},
		{"base62, 24 characters", func() string { return random(base62, 24) }},
		{"base62, 32 characters", func() string { return random(base62, 32) }},
		{"base64, 44 characters", func() string { return random(base64, 42) + "==" }},
		{"base64url, 22 characters", func() string { return random(base64url, 22) }},
		{"base64url, 43 characters", func() string { return random(base64url, 43) }},
		{"base64url, 64 characters", func() string { return random(base64url, 64) }},
		{"base64url opening with a dash", func() string { return "-" + random(base64url, 42) }},
		{"base64url opening with two dashes", func() string { return "--" + random(base64url, 41) }},
	}
	for _, shape := range shapes {
		var written []string
		for range 5000 {
			value := shape.make()
			if secret, _ := recipeSecret(ProjectEnvVar{Key: "INSTANCE_REF", Value: value}); !secret {
				written = append(written, value)
			}
		}
		if len(written) > 0 {
			t.Errorf("%s: %d of 5000 written as they are, e.g. %q", shape.name, len(written), written[:min(3, len(written))])
		}
	}
}

// What a shape cannot tell from a hostname or a word — a one-case run of
// letters, a passphrase, a PIN — is a secret by its name: under any name
// that says so, every such value is generated.
func TestRecipeSecret_CredentialNamesCoverWhatShapesCannot(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(20260930, 2)) //nolint:gosec // a seeded generator keeps the samples the same on every run
	words := strings.Fields("apple river stone cloud tiger lemon piano rocket garden silver candle forest orange hammer mirror planet saddle velvet walnut zebra")
	random := func(alphabet string, n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = alphabet[rng.IntN(len(alphabet))]
		}
		return string(b)
	}
	passphrase := func() string {
		parts := make([]string, 3+rng.IntN(6))
		for i := range parts {
			parts[i] = words[rng.IntN(len(words))]
		}
		return strings.Join(parts, []string{" ", "-", "_", "."}[rng.IntN(4)])
	}
	values := []func() string{
		func() string { return random("abcdefghijklmnopqrstuvwxyz", 8+rng.IntN(17)) },
		func() string { return random("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567", 16) },
		func() string { return random("0123456789", 4+rng.IntN(7)) },
		passphrase,
	}
	for _, key := range []string{"SIGNING_SEED", "OTP_SEED", "ADMIN_PASSCODE", "RECOVERY_PHRASE", "WALLET_MNEMONIC", "ADMIN_PIN", "SECRETKEY", "MASTERKEY", "API_ACCESSKEY", "APP_AUTHTOKEN"} {
		for range 2000 {
			value := values[rng.IntN(len(values))]()
			if secret, _ := recipeSecret(ProjectEnvVar{Key: key, Value: value}); !secret {
				t.Errorf("%s=%q is written as it is", key, value)
				break
			}
		}
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
		{"Laravel's base64 key becomes the raw key its recipes write", "base64:" + strings.Repeat("A", 43) + "=", "<@generateRandomString(<32>)>"},
		{"a basic-auth pair keeps its user", "mailpit:" + strings.Repeat("p", 24), "mailpit:<@generateRandomString(<24>)>"},
		{"a pair's short password still gets 16", "admin:pw", "admin:<@generateRandomString(<16>)>"},
		{"a URL is no pair", "redis://cache:6379", "<@generateRandomString(<18>)>"},
		{"a second colon is no pair", "a:b:c", "<@generateRandomString(<16>)>"},
		// A pair keeps its user only when the user is a plain word: an
		// access key's id, a token or a UUID before the colon is the secret.
		{"an access key id is no user", "AK" + "IAIOSFODNN7EXAMPLE:" + strings.Repeat("s", 40), "<@generateRandomString(<61>)>"},
		{"a token is no user", "gl" + "pat-Xq9rT2pLm9Wn4Xc6Yb1H:x-oauth-basic", "<@generateRandomString(<40>)>"},
		{"a UUID is no user", "3b9e5c1a-7d2f-4e8b-9a6c-0f1e2d3c4b5a:" + strings.Repeat("p", 20), "<@generateRandomString(<57>)>"},
		{"a user of joined words is no plain word", "admin_user:" + strings.Repeat("p", 20), "<@generateRandomString(<31>)>"},
		{"a capitalized user is a plain word", "Admin:" + strings.Repeat("p", 20), "Admin:<@generateRandomString(<20>)>"},
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
// generators under envSecrets, each with a line saying it was set by hand and
// has to be set again — but for a secret the app makes for itself, which a
// fresh environment simply generates anew.
func TestBuildGroupRecipe_ProjectVariables(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.ProjectEnvs = []ProjectEnvVar{
		{Key: "APP_URL", Value: "https://app-${zeropsSubdomainHost}.prg1.zerops.app"},
		{Key: "STORE_PUBLISHABLE_KEY", Value: "${api_CHANNEL_PUBLISHABLE_KEY}"},
		{Key: "SUPERADMIN_EMAIL", Value: "admin@example.com"},
		{Key: "JWT_SECRET", Value: strings.Repeat("j", 48)},
		{Key: "STRIPE_API_KEY", Value: "rk_" + "live_" + strings.Repeat("s", 22)},
		{Key: "STRIPE_PRICE_PRO", Value: "price_" + "1Mq7Xz2Lb9Rt4Wv8Kd3Nc6Hs"},
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
			secrets := mappingValue(project, "envSecrets")
			wantSecrets := map[string]string{
				"JWT_SECRET":            "<@generateRandomString(<48>)>",
				"STRIPE_API_KEY":        "<@generateRandomString(<30>)>",
				"STRIPE_PRICE_PRO":      "<@generateRandomString(<30>)>",
				"STRIPE_WEBHOOK_SECRET": "",
			}
			if got := scalarMap(secrets); !maps.Equal(got, wantSecrets) {
				t.Errorf("envSecrets = %v, want %v", got, wantSecrets)
			}
			if !strings.HasPrefix(tier.ImportYAML, preprocessorHeader) {
				t.Errorf("a generator without the preprocessor's first line")
			}
			for _, key := range []string{"STRIPE_API_KEY", "STRIPE_PRICE_PRO"} {
				comment := keyComment(secrets, key)
				if lower := strings.ToLower(comment); !strings.Contains(lower, "set by hand") || !strings.Contains(comment, in.MateProjectName) || !strings.Contains(lower, "set it again") {
					t.Errorf("%s says %q, want that it was set by hand in %s and must be set again", key, comment, in.MateProjectName)
				}
			}
			for _, key := range []string{"JWT_SECRET", "STRIPE_WEBHOOK_SECRET"} {
				if c := keyComment(secrets, key); c != "" {
					t.Errorf("%s is simply regenerated, or empty, yet says %q", key, c)
				}
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
		{Key: "SENDGRID_API_KEY", Value: fakeSendGridKey},
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

// serviceNodeOrNil is a tier's service with the given hostname, nil when the
// tier has none.
func serviceNodeOrNil(t *testing.T, body, hostname string) *yaml.Node {
	t.Helper()
	for _, item := range mappingValue(tierMapping(t, body), "services").Content {
		if node := mappingValue(item, "hostname"); node != nil && node.Value == hostname {
			return item
		}
	}
	return nil
}
