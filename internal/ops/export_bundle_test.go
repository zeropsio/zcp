package ops

import (
	"context"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/preprocess"
	"github.com/zeropsio/zcp/internal/topology"
)

// laravelZeropsYAML is a representative dev/prod-pair zerops.yaml body
// with two setup blocks and run.envVariables references. Used as Laravel-
// shaped fixture across composer + integration tests.
const laravelZeropsYAML = `zerops:
  - setup: appdev
    build:
      base: php@8.4
      buildCommands:
        - composer install
      deployFiles: ["./"]
    run:
      base: php-apache@8.4
      envVariables:
        APP_ENV: dev
        APP_KEY: ${APP_KEY}
        DB_HOST: ${db_hostname}
        DB_PASSWORD: ${db_password}
  - setup: appprod
    build:
      base: php@8.4
      buildCommands:
        - composer install --no-dev
      deployFiles: ["./"]
    run:
      base: php-apache@8.4
      envVariables:
        APP_ENV: prod
        APP_KEY: ${APP_KEY}
        DB_HOST: ${db_hostname}
        DB_PASSWORD: ${db_password}
      readinessCheck:
        httpGet:
          path: /healthz
          port: 80
`

func TestVerifyZeropsYAMLSetup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		setup   string
		wantErr string // substring; empty = no error
	}{
		{
			name:    "happy path — present setup",
			body:    laravelZeropsYAML,
			setup:   "appdev",
			wantErr: "",
		},
		{
			name:    "happy path — second setup in multi-block file",
			body:    laravelZeropsYAML,
			setup:   "appprod",
			wantErr: "",
		},
		{
			name:    "empty body — chains to scaffold",
			body:    "",
			setup:   "appdev",
			wantErr: "chain to scaffold-zerops-yaml",
		},
		{
			name:    "whitespace-only body — chains to scaffold",
			body:    "   \n\t\n  ",
			setup:   "appdev",
			wantErr: "chain to scaffold-zerops-yaml",
		},
		{
			name:    "missing zerops top-level list",
			body:    "name: not-a-zerops-yaml\n",
			setup:   "appdev",
			wantErr: "missing top-level 'zerops:' list",
		},
		{
			name:    "missing requested setup",
			body:    laravelZeropsYAML,
			setup:   "appstage",
			wantErr: `does not contain setup "appstage"`,
		},
		{
			name:    "malformed YAML",
			body:    "zerops:\n  - setup: appdev\n   build: bad-indent\n",
			setup:   "appdev",
			wantErr: "parse zerops.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := verifyZeropsYAMLSetup(tt.body, tt.setup)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("verifyZeropsYAMLSetup: unexpected error %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("verifyZeropsYAMLSetup: expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("verifyZeropsYAMLSetup: error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestComposeProjectEnvVariables(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		envs            []ProjectEnvVar
		classifications map[string]topology.SecretClassification
		wantOut         map[string]string
		wantWarnSubstr  []string // each must appear in some warning
	}{
		{
			name: "infrastructure-derived dropped",
			envs: []ProjectEnvVar{
				{Key: "DB_HOST", Value: "${db_hostname}"},
			},
			classifications: map[string]topology.SecretClassification{
				"DB_HOST": topology.SecretClassInfrastructure,
			},
			wantOut: map[string]string{},
		},
		{
			name: "auto-secret emits generateRandomString",
			envs: []ProjectEnvVar{
				{Key: "APP_KEY", Value: "base64:abcd…"},
			},
			classifications: map[string]topology.SecretClassification{
				"APP_KEY": topology.SecretClassAutoSecret,
			},
			wantOut: map[string]string{
				"APP_KEY": "<@generateRandomString(<32>)>",
			},
		},
		{
			name: "external-secret with non-empty value emits REPLACE_ME placeholder",
			envs: []ProjectEnvVar{
				{Key: "STRIPE_SECRET", Value: "sk_live_xyz"},
			},
			classifications: map[string]topology.SecretClassification{
				"STRIPE_SECRET": topology.SecretClassExternalSecret,
			},
			wantOut: map[string]string{
				"STRIPE_SECRET": `<@pickRandom(["REPLACE_ME"])>`,
			},
		},
		{
			name: "external-secret with empty value emits empty + warning (M4)",
			envs: []ProjectEnvVar{
				{Key: "STRIPE_SECRET", Value: ""},
			},
			classifications: map[string]topology.SecretClassification{
				"STRIPE_SECRET": topology.SecretClassExternalSecret,
			},
			wantOut: map[string]string{
				"STRIPE_SECRET": "",
			},
			wantWarnSubstr: []string{`STRIPE_SECRET`, "empty external secret"},
		},
		{
			name: "plain-config emits verbatim",
			envs: []ProjectEnvVar{
				{Key: "LOG_LEVEL", Value: "info"},
			},
			classifications: map[string]topology.SecretClassification{
				"LOG_LEVEL": topology.SecretClassPlainConfig,
			},
			wantOut: map[string]string{
				"LOG_LEVEL": "info",
			},
		},
		{
			name: "unset emits verbatim with warning",
			envs: []ProjectEnvVar{
				{Key: "MYSTERY_VAR", Value: "abc"},
			},
			classifications: map[string]topology.SecretClassification{},
			wantOut: map[string]string{
				"MYSTERY_VAR": "abc",
			},
			wantWarnSubstr: []string{"MYSTERY_VAR", "not classified"},
		},
		{
			name: "unknown bucket emits verbatim with warning",
			envs: []ProjectEnvVar{
				{Key: "WEIRD", Value: "z"},
			},
			classifications: map[string]topology.SecretClassification{
				"WEIRD": topology.SecretClassification("nonsense"),
			},
			wantOut: map[string]string{
				"WEIRD": "z",
			},
			wantWarnSubstr: []string{"WEIRD", `unknown classification "nonsense"`},
		},
		{
			name:            "empty input emits empty output and no warnings",
			envs:            nil,
			classifications: nil,
			wantOut:         map[string]string{},
		},
		{
			name: "secret-mid-string plain-config preserves embedded ${} reference",
			envs: []ProjectEnvVar{
				{Key: "MAILGUN_FROM", Value: "Acme Support <support@${zeropsSubdomainHost}>"},
			},
			classifications: map[string]topology.SecretClassification{
				"MAILGUN_FROM": topology.SecretClassPlainConfig,
			},
			wantOut: map[string]string{
				"MAILGUN_FROM": "Acme Support <support@${zeropsSubdomainHost}>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotOut, gotWarns := composeProjectEnvVariables(tt.envs, tt.classifications)
			if len(gotOut) != len(tt.wantOut) {
				t.Errorf("output map size mismatch: got %d entries, want %d (got=%v want=%v)", len(gotOut), len(tt.wantOut), gotOut, tt.wantOut)
			}
			for k, want := range tt.wantOut {
				if gotOut[k] != want {
					t.Errorf("output[%q] = %q, want %q", k, gotOut[k], want)
				}
			}
			for _, sub := range tt.wantWarnSubstr {
				found := false
				for _, w := range gotWarns {
					if strings.Contains(w, sub) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected warning containing %q, got %v", sub, gotWarns)
				}
			}
			if len(tt.wantWarnSubstr) == 0 && len(gotWarns) > 0 {
				t.Errorf("expected no warnings, got %v", gotWarns)
			}
		})
	}
}

// TestRuntimeImportMode pins the platform-mode contract per Phase 5
// schema-validation findings: import.yaml's `mode:` is the platform
// scaling enum (`HA` / `NON_HA`) only. Single-runtime bundle entries
// always emit `NON_HA` — the topology dev/stage/simple/local-only
// distinction is destination-bootstrap concern, not import.yaml
// content. The function preserves the topology.Mode argument as a
// future-extension hook; current contract is mode-independent.
func TestRuntimeImportMode(t *testing.T) {
	t.Parallel()
	tests := []topology.Mode{
		topology.ModeStandard,
		topology.ModeStage,
		topology.ModeDev,
		topology.ModeSimple,
		topology.ModeLocalStage,
		topology.ModeLocalOnly,
		topology.Mode("garbled"),
	}
	for _, mode := range tests {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			if got := runtimeImportMode(mode); got != "NON_HA" {
				t.Errorf("runtimeImportMode(%q) = %q, want NON_HA", mode, got)
			}
		})
	}
}

func TestComposeProjectEnvVariables_AutoSecretDirectiveExpands(t *testing.T) {
	t.Parallel()

	out, warnings := composeProjectEnvVariables(
		[]ProjectEnvVar{{Key: "APP_KEY", Value: "base64:old"}},
		map[string]topology.SecretClassification{"APP_KEY": topology.SecretClassAutoSecret},
	)
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}

	expanded, err := preprocess.Expand(context.Background(), out["APP_KEY"])
	if err != nil {
		t.Fatalf("auto-secret preprocessor directive should expand via zParser: %v", err)
	}
	if len(expanded) != 32 {
		t.Fatalf("expanded auto-secret length = %d, want 32 (value %q)", len(expanded), expanded)
	}
}

func TestAddPreprocessorHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		body        string
		projectEnvs map[string]string
		wantHeader  bool
	}{
		{
			name:        "no envs — no header",
			body:        "project:\n  name: x\n",
			projectEnvs: nil,
			wantHeader:  false,
		},
		{
			name:        "plain-config envs only — no header",
			body:        "project:\n  name: x\n",
			projectEnvs: map[string]string{"LOG_LEVEL": "info"},
			wantHeader:  false,
		},
		{
			name:        "auto-secret directive — header prepended",
			body:        "project:\n  name: x\n",
			projectEnvs: map[string]string{"APP_KEY": "<@generateRandomString(<32>)>"},
			wantHeader:  true,
		},
		{
			name:        "external-secret directive — header prepended",
			body:        "project:\n  name: x\n",
			projectEnvs: map[string]string{"STRIPE": `<@pickRandom(["REPLACE_ME"])>`},
			wantHeader:  true,
		},
		{
			name:        "partial directive without closing )> — no header",
			body:        "project:\n  name: x\n",
			projectEnvs: map[string]string{"WEIRD": "<@notADirective"},
			wantHeader:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := addPreprocessorHeader(tt.body, tt.projectEnvs)
			has := strings.HasPrefix(got, preprocessorHeader)
			if has != tt.wantHeader {
				t.Errorf("addPreprocessorHeader: header presence = %v, want %v (output=%q)", has, tt.wantHeader, got)
			}
		})
	}
}

func TestComposeImportYAML_MinimalRuntimeOnly(t *testing.T) {
	t.Parallel()
	inputs := BundleInputs{
		ProjectName:    "demo",
		TargetHostname: "appdev",
		SourceMode:     topology.ModeStandard,
		ServiceType:    "nodejs@22",
		SetupName:      "appdev",
		ZeropsYAMLBody: laravelZeropsYAML,
		RepoURL:        "https://github.com/example/demo.git",
	}
	body, warnings, err := composeImportYAML(inputs, topology.ExportVariantDev, nil)
	if err != nil {
		t.Fatalf("composeImportYAML: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	doc := mustUnmarshal(t, body)
	project, _ := doc["project"].(map[string]any)
	if project["name"] != "demo" {
		t.Errorf("project.name = %v, want demo", project["name"])
	}
	if _, ok := project["envVariables"]; ok {
		t.Errorf("project.envVariables should be omitted when empty, got %v", project["envVariables"])
	}

	services, _ := doc["services"].([]any)
	if len(services) != 1 {
		t.Fatalf("expected 1 service entry, got %d", len(services))
	}
	svc, _ := services[0].(map[string]any)
	checkServiceField(t, svc, "hostname", "appdev")
	checkServiceField(t, svc, "type", "nodejs@22")
	checkServiceField(t, svc, "mode", "NON_HA")
	checkServiceField(t, svc, "buildFromGit", "https://github.com/example/demo.git")
	checkServiceField(t, svc, "zeropsSetup", "appdev")
	if _, ok := svc["enableSubdomainAccess"]; ok {
		t.Errorf("enableSubdomainAccess should be omitted when SubdomainEnabled=false")
	}

	if strings.HasPrefix(body, preprocessorHeader) {
		t.Errorf("preprocessor header should be absent (no <@...> directives)")
	}
}

func TestComposeImportYAML_WithSubdomainAndManagedDeps(t *testing.T) {
	t.Parallel()
	inputs := BundleInputs{
		ProjectName:      "demo",
		TargetHostname:   "appstage",
		SourceMode:       topology.ModeStage,
		ServiceType:      "php-apache@8.4",
		SubdomainEnabled: true,
		SetupName:        "appprod",
		ZeropsYAMLBody:   laravelZeropsYAML,
		RepoURL:          "https://github.com/example/demo.git",
		ManagedServices: []ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			{Hostname: "cache", Type: "valkey@7.2", Mode: "NON_HA"},
			{Hostname: "store", Type: "object-storage", Mode: ""},
		},
	}
	body, _, err := composeImportYAML(inputs, topology.ExportVariantStage, nil)
	if err != nil {
		t.Fatalf("composeImportYAML: %v", err)
	}

	doc := mustUnmarshal(t, body)
	services, _ := doc["services"].([]any)
	if len(services) != 4 {
		t.Fatalf("expected 4 service entries (1 runtime + 3 managed), got %d", len(services))
	}

	runtime, _ := services[0].(map[string]any)
	checkServiceField(t, runtime, "mode", "NON_HA") // platform scaling mode
	if runtime["enableSubdomainAccess"] != true {
		t.Errorf("enableSubdomainAccess should be true, got %v", runtime["enableSubdomainAccess"])
	}

	for _, idx := range []int{1, 2, 3} {
		m, _ := services[idx].(map[string]any)
		if m["priority"] != 10 {
			t.Errorf("managed service idx %d priority = %v, want 10", idx, m["priority"])
		}
	}

	storage, _ := services[3].(map[string]any)
	if _, ok := storage["mode"]; ok {
		t.Errorf("object-storage mode should be omitted (empty), got %v", storage["mode"])
	}
}

func TestComposeImportYAML_PreprocessorHeaderOnAutoSecret(t *testing.T) {
	t.Parallel()
	inputs := BundleInputs{
		ProjectName:    "demo",
		TargetHostname: "appdev",
		SourceMode:     topology.ModeStandard,
		ServiceType:    "nodejs@22",
		SetupName:      "appdev",
		ZeropsYAMLBody: laravelZeropsYAML,
		RepoURL:        "https://github.com/example/demo.git",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "JWT_SECRET", Value: "long-random-bytes"},
		},
	}
	body, _, err := composeImportYAML(inputs, topology.ExportVariantDev, map[string]topology.SecretClassification{
		"JWT_SECRET": topology.SecretClassAutoSecret,
	})
	if err != nil {
		t.Fatalf("composeImportYAML: %v", err)
	}
	if !strings.HasPrefix(body, preprocessorHeader) {
		t.Errorf("expected preprocessor header on line 1, got body starting with %q", body[:40])
	}
	if !strings.Contains(body, "<@generateRandomString(<32>)>") {
		t.Errorf("expected generateRandomString directive in body, body=%q", body)
	}
}

func TestBuildBundle_HappyPath(t *testing.T) {
	t.Parallel()
	inputs := BundleInputs{
		ProjectName:      "demo",
		TargetHostname:   "appdev",
		SourceMode:       topology.ModeStandard,
		ServiceType:      "php-apache@8.4",
		SubdomainEnabled: true,
		SetupName:        "appdev",
		ZeropsYAMLBody:   laravelZeropsYAML,
		RepoURL:          "https://github.com/example/demo.git",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "APP_KEY", Value: "base64:old"},
			{Key: "DB_HOST", Value: "${db_hostname}"},
			{Key: "LOG_LEVEL", Value: "info"},
			{Key: "STRIPE_SECRET", Value: "sk_live_real"},
		},
		ManagedServices: []ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
		},
	}
	classifications := map[string]topology.SecretClassification{
		"APP_KEY":       topology.SecretClassAutoSecret,
		"DB_HOST":       topology.SecretClassInfrastructure,
		"LOG_LEVEL":     topology.SecretClassPlainConfig,
		"STRIPE_SECRET": topology.SecretClassExternalSecret,
	}

	bundle, err := BuildBundle(inputs, topology.ExportVariantDev, classifications)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	if bundle.ImportYAML == "" {
		t.Error("ImportYAML should not be empty")
	}
	if bundle.ZeropsYAML != laravelZeropsYAML {
		t.Error("ZeropsYAML should mirror live body verbatim")
	}
	if bundle.ZeropsYAMLSource != "live" {
		t.Errorf("ZeropsYAMLSource = %q, want live", bundle.ZeropsYAMLSource)
	}
	if bundle.RepoURL != inputs.RepoURL {
		t.Errorf("RepoURL = %q, want %q", bundle.RepoURL, inputs.RepoURL)
	}
	if bundle.Variant != topology.ExportVariantDev {
		t.Errorf("Variant = %q, want dev", bundle.Variant)
	}
	if bundle.TargetHostname != "appdev" {
		t.Errorf("TargetHostname = %q, want appdev", bundle.TargetHostname)
	}
	if bundle.SetupName != "appdev" {
		t.Errorf("SetupName = %q, want appdev", bundle.SetupName)
	}
	if bundle.Classifications["APP_KEY"] != topology.SecretClassAutoSecret {
		t.Errorf("classifications round-trip lost: APP_KEY = %q", bundle.Classifications["APP_KEY"])
	}

	if !strings.HasPrefix(bundle.ImportYAML, preprocessorHeader) {
		t.Error("ImportYAML should start with preprocessor header (auto-secret + external-secret directives present)")
	}

	doc := mustUnmarshal(t, bundle.ImportYAML)
	project, _ := doc["project"].(map[string]any)
	envs, _ := project["envVariables"].(map[string]any)
	if _, ok := envs["DB_HOST"]; ok {
		t.Error("DB_HOST should be dropped (infrastructure-derived)")
	}
	if envs["APP_KEY"] != "<@generateRandomString(<32>)>" {
		t.Errorf("APP_KEY = %v, want generateRandomString directive", envs["APP_KEY"])
	}
	if envs["LOG_LEVEL"] != "info" {
		t.Errorf("LOG_LEVEL = %v, want info", envs["LOG_LEVEL"])
	}
	if envs["STRIPE_SECRET"] != `<@pickRandom(["REPLACE_ME"])>` {
		t.Errorf("STRIPE_SECRET = %v, want pickRandom directive", envs["STRIPE_SECRET"])
	}
}

func TestBuildBundle_Errors(t *testing.T) {
	t.Parallel()
	base := BundleInputs{
		ProjectName:    "demo",
		TargetHostname: "appdev",
		SourceMode:     topology.ModeStandard,
		ServiceType:    "nodejs@22",
		SetupName:      "appdev",
		ZeropsYAMLBody: laravelZeropsYAML,
		RepoURL:        "https://github.com/example/demo.git",
	}

	tests := []struct {
		name    string
		mutate  func(*BundleInputs)
		wantErr string
	}{
		{
			name:    "missing target hostname",
			mutate:  func(b *BundleInputs) { b.TargetHostname = "" },
			wantErr: "target hostname required",
		},
		{
			name:    "missing repo URL chains to setup-git-push",
			mutate:  func(b *BundleInputs) { b.RepoURL = "" },
			wantErr: "chain to setup-git-push",
		},
		{
			name:    "missing setup name",
			mutate:  func(b *BundleInputs) { b.SetupName = "" },
			wantErr: "setup name required",
		},
		{
			name:    "missing service type",
			mutate:  func(b *BundleInputs) { b.ServiceType = "" },
			wantErr: "service type required",
		},
		{
			name:    "empty zerops.yaml chains to scaffold",
			mutate:  func(b *BundleInputs) { b.ZeropsYAMLBody = "" },
			wantErr: "chain to scaffold-zerops-yaml",
		},
		{
			name:    "setup name absent from zerops.yaml",
			mutate:  func(b *BundleInputs) { b.SetupName = "ghost" },
			wantErr: `does not contain setup "ghost"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := base
			tt.mutate(&inputs)
			_, err := BuildBundle(inputs, topology.ExportVariantDev, nil)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestBuildBundle_NodeShape covers a Node + Mongo + JWT app — exercises
// non-Laravel runtime + a different infrastructure prefix than ${db_*}.
func TestBuildBundle_NodeShape(t *testing.T) {
	t.Parallel()
	const nodeYAML = `zerops:
  - setup: api
    build:
      base: nodejs@22
      buildCommands:
        - npm install
        - npm run build
      deployFiles: ["./"]
    run:
      base: nodejs@22
      envVariables:
        MONGO_URI: ${mongo_connectionString}
        JWT_SECRET: ${JWT_SECRET}
      readinessCheck:
        httpGet:
          path: /healthz
          port: 3000
`
	bundle, err := BuildBundle(BundleInputs{
		ProjectName:    "node-demo",
		TargetHostname: "api",
		SourceMode:     topology.ModeSimple,
		ServiceType:    "nodejs@22",
		SetupName:      "api",
		ZeropsYAMLBody: nodeYAML,
		RepoURL:        "https://github.com/example/node-demo.git",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "JWT_SECRET", Value: "old-jwt-secret"},
			{Key: "MONGO_URI", Value: "${mongo_connectionString}"},
		},
		ManagedServices: []ManagedServiceEntry{
			{Hostname: "mongo", Type: "mongodb@7", Mode: "NON_HA"},
		},
	}, topology.ExportVariantUnset, map[string]topology.SecretClassification{
		"JWT_SECRET": topology.SecretClassAutoSecret,
		"MONGO_URI":  topology.SecretClassInfrastructure,
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	doc := mustUnmarshal(t, bundle.ImportYAML)
	services, _ := doc["services"].([]any)
	if len(services) != 2 {
		t.Fatalf("expected 2 services (api + mongo), got %d", len(services))
	}
	runtime, _ := services[0].(map[string]any)
	if runtime["mode"] != "NON_HA" { // platform scaling mode
		t.Errorf("mode = %v, want NON_HA", runtime["mode"])
	}
	envs, _ := doc["project"].(map[string]any)["envVariables"].(map[string]any)
	if _, ok := envs["MONGO_URI"]; ok {
		t.Error("MONGO_URI should be dropped (infrastructure-derived)")
	}
}

// TestBuildBundle_StaticShape covers a static-only runtime — no run.envs.
func TestBuildBundle_StaticShape(t *testing.T) {
	t.Parallel()
	const staticYAML = `zerops:
  - setup: site
    build:
      base: nodejs@22
      buildCommands:
        - npm install
        - npm run build
      deployFiles: dist
    run:
      base: static
`
	bundle, err := BuildBundle(BundleInputs{
		ProjectName:    "static-demo",
		TargetHostname: "site",
		SourceMode:     topology.ModeSimple,
		ServiceType:    "static",
		SetupName:      "site",
		ZeropsYAMLBody: staticYAML,
		RepoURL:        "https://github.com/example/static-demo.git",
	}, topology.ExportVariantUnset, nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if strings.HasPrefix(bundle.ImportYAML, preprocessorHeader) {
		t.Error("static bundle should not have preprocessor header (no envs)")
	}
}

// TestBuildBundle_PHPSecretMidString covers a privacy-flagged plain
// config that contains a quote-special character — exercises yaml.v3
// quoting decisions on the emitted value.
func TestBuildBundle_PHPSecretMidString(t *testing.T) {
	t.Parallel()
	bundle, err := BuildBundle(BundleInputs{
		ProjectName:    "php-demo",
		TargetHostname: "appdev",
		SourceMode:     topology.ModeStandard,
		ServiceType:    "php-apache@8.4",
		SetupName:      "appdev",
		ZeropsYAMLBody: laravelZeropsYAML,
		RepoURL:        "https://github.com/example/php-demo.git",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "MAIL_FROM", Value: `Acme Support <support@acme.com>`},
		},
	}, topology.ExportVariantDev, map[string]topology.SecretClassification{
		"MAIL_FROM": topology.SecretClassPlainConfig,
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	doc := mustUnmarshal(t, bundle.ImportYAML)
	envs, _ := doc["project"].(map[string]any)["envVariables"].(map[string]any)
	got, _ := envs["MAIL_FROM"].(string)
	if got != `Acme Support <support@acme.com>` {
		t.Errorf("MAIL_FROM round-trip: got %q, want literal", got)
	}
}

// TestBuildBundle_M2IndirectInfraReference exercises the M2 risk: a
// project env classified Infrastructure (and therefore dropped from
// project.envVariables) is referenced by zerops.yaml's run.envVariables
// via `${ENV_NAME}`. Without the warning, re-import would silently
// fail to resolve. Per plan §3.4 amendment 12 + Codex Agent A blocker 1.
func TestBuildBundle_M2IndirectInfraReference(t *testing.T) {
	t.Parallel()
	const indirectYAML = `zerops:
  - setup: appdev
    build:
      base: php@8.4
      buildCommands:
        - composer install
      deployFiles: ["./"]
    run:
      base: php-apache@8.4
      envVariables:
        DATABASE_URL: postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}
`
	bundle, err := BuildBundle(BundleInputs{
		ProjectName:    "indirect-demo",
		TargetHostname: "appdev",
		SourceMode:     topology.ModeStandard,
		ServiceType:    "php-apache@8.4",
		SetupName:      "appdev",
		ZeropsYAMLBody: indirectYAML,
		RepoURL:        "https://github.com/example/indirect-demo.git",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "DB_HOST", Value: "${db_hostname}"},
			{Key: "DB_PASSWORD", Value: "${db_password}"},
			{Key: "DB_USER", Value: "${db_user}"},
			{Key: "DB_PORT", Value: "${db_port}"},
			{Key: "DB_NAME", Value: "${db_dbName}"},
			{Key: "LOG_LEVEL", Value: "info"},
		},
	}, topology.ExportVariantDev, map[string]topology.SecretClassification{
		"DB_HOST":     topology.SecretClassInfrastructure,
		"DB_PASSWORD": topology.SecretClassInfrastructure,
		"DB_USER":     topology.SecretClassInfrastructure,
		"DB_PORT":     topology.SecretClassInfrastructure,
		"DB_NAME":     topology.SecretClassInfrastructure,
		"LOG_LEVEL":   topology.SecretClassPlainConfig,
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	for _, refed := range []string{"DB_HOST", "DB_PASSWORD", "DB_USER", "DB_PORT", "DB_NAME"} {
		found := false
		for _, w := range bundle.Warnings {
			if strings.Contains(w, refed) && strings.Contains(w, "M2") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected M2 warning naming env %q, got warnings:\n%s", refed, strings.Join(bundle.Warnings, "\n"))
		}
	}

	for _, w := range bundle.Warnings {
		if strings.Contains(w, "LOG_LEVEL") && strings.Contains(w, "M2") {
			t.Errorf("LOG_LEVEL is plain-config and zerops.yaml does not reference it — should not trigger M2 warning, got %q", w)
		}
	}
}

// TestBuildBundle_M2NoFalsePositiveOnManagedServiceRef confirms the
// detector does NOT warn when zerops.yaml references a managed-service
// env (`${db_hostname}`) without a corresponding project env of the
// same name — that's the happy path, not M2.
func TestBuildBundle_M2NoFalsePositiveOnManagedServiceRef(t *testing.T) {
	t.Parallel()
	bundle, err := BuildBundle(BundleInputs{
		ProjectName:    "happy-managed-refs",
		TargetHostname: "appdev",
		SourceMode:     topology.ModeStandard,
		ServiceType:    "php-apache@8.4",
		SetupName:      "appdev",
		ZeropsYAMLBody: laravelZeropsYAML, // references ${db_hostname} / ${db_password} (managed-service envs)
		RepoURL:        "https://github.com/example/happy.git",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "APP_KEY", Value: "old"},
		},
	}, topology.ExportVariantDev, map[string]topology.SecretClassification{
		"APP_KEY": topology.SecretClassAutoSecret,
	})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	for _, w := range bundle.Warnings {
		if strings.Contains(w, "M2") {
			t.Errorf("expected no M2 warning when zerops.yaml references managed-service envs, got %q", w)
		}
	}
}

// TestBuildBundle_SentinelExternalSecretFlags covers the polish item
// from Codex Agent A: an external-secret value that matches a known
// test/sentinel pattern emits REPLACE_ME but ALSO adds a warning
// suggesting the agent verify classification (PlainConfig may be more
// appropriate). Per plan §3.4 amendment 12 / M4.
func TestBuildBundle_SentinelExternalSecretFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		value     string
		wantWarn  bool
		wantEmpty bool // when wantEmpty=true the empty-string + M4 warning path applies
	}{
		{name: "Stripe test secret key", value: "sk_test_123", wantWarn: true},
		{name: "Stripe test publishable key", value: "pk_test_xyz", wantWarn: true},
		{name: "Stripe test restricted key", value: "rk_test_abc", wantWarn: true},
		{name: "disabled sentinel", value: "disabled", wantWarn: true},
		{name: "none sentinel", value: "none", wantWarn: true},
		{name: "off sentinel", value: "off", wantWarn: true},
		{name: "real-looking external value", value: "sk_live_realKey9", wantWarn: false},
		{name: "empty value", value: "", wantWarn: false, wantEmpty: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bundle, err := BuildBundle(BundleInputs{
				ProjectName:    "sentinel-demo",
				TargetHostname: "appdev",
				SourceMode:     topology.ModeStandard,
				ServiceType:    "nodejs@22",
				SetupName:      "appdev",
				ZeropsYAMLBody: laravelZeropsYAML,
				RepoURL:        "https://github.com/example/sentinel-demo.git",
				ProjectEnvs: []ProjectEnvVar{
					{Key: "STRIPE_SECRET", Value: tt.value},
				},
			}, topology.ExportVariantDev, map[string]topology.SecretClassification{
				"STRIPE_SECRET": topology.SecretClassExternalSecret,
			})
			if err != nil {
				t.Fatalf("BuildBundle: %v", err)
			}
			gotWarn := false
			gotEmpty := false
			for _, w := range bundle.Warnings {
				if strings.Contains(w, "STRIPE_SECRET") {
					if strings.Contains(w, "sentinel/test pattern") {
						gotWarn = true
					}
					if strings.Contains(w, "empty external secret") {
						gotEmpty = true
					}
				}
			}
			if gotWarn != tt.wantWarn {
				t.Errorf("sentinel warning = %v, want %v (warnings=%v)", gotWarn, tt.wantWarn, bundle.Warnings)
			}
			if gotEmpty != tt.wantEmpty {
				t.Errorf("empty-external warning = %v, want %v (warnings=%v)", gotEmpty, tt.wantEmpty, bundle.Warnings)
			}
		})
	}
}

// TestParseDollarBraceRefs covers the brace-ref scanner used by the
// M2 detector. Edge cases: empty input, no refs, single ref, multiple
// refs, duplicate refs (deduped), unclosed brace, empty braces.
func TestParseDollarBraceRefs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"no refs", "plain text without dollars", nil},
		{"single ref", "${FOO}", []string{"FOO"}},
		{"multiple refs", "${A}-${B}-${C}", []string{"A", "B", "C"}},
		{"duplicate refs deduped", "${A}-${A}-${B}", []string{"A", "B"}},
		{"unclosed brace skipped", "${BAD-no-close", nil},
		{"empty braces skipped", "${}", nil},
		{"compound URL", "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}", []string{"DB_USER", "DB_PASSWORD", "DB_HOST"}},
		{"text plus refs", "prefix ${FOO} middle ${BAR} suffix", []string{"FOO", "BAR"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseDollarBraceRefs(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("len(got)=%d, len(want)=%d (got=%v want=%v)", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d]=%q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestExtractZeropsYAMLRunEnvRefs covers the zerops.yaml ref extractor
// used by the M2 detector. Multi-setup files merge refs across all
// setups; non-string env values are skipped; malformed bodies return
// an empty set silently.
func TestExtractZeropsYAMLRunEnvRefs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want []string // sorted; presence checked
	}{
		{
			name: "empty body",
			body: "",
			want: nil,
		},
		{
			name: "malformed yaml",
			body: "not valid: : :",
			want: nil,
		},
		{
			name: "no zerops top-level",
			body: "name: nope\n",
			want: nil,
		},
		{
			name: "single setup with refs",
			body: `zerops:
  - setup: api
    run:
      base: nodejs@22
      envVariables:
        DB_HOST: ${db_hostname}
        APP_KEY: ${APP_KEY}
`,
			want: []string{"APP_KEY", "db_hostname"},
		},
		{
			name: "multi-setup merges refs",
			body: laravelZeropsYAML,
			want: []string{"APP_KEY", "db_hostname", "db_password"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractZeropsYAMLRunEnvRefs(tt.body)
			for _, name := range tt.want {
				if !got[name] {
					t.Errorf("expected ref %q in set %v", name, got)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("got %d refs, want %d (got=%v want=%v)", len(got), len(tt.want), got, tt.want)
			}
		})
	}
}

// TestIsLikelySentinel pins the conservative allowlist used by
// composeProjectEnvVariables to flag external-secret mis-classification
// candidates. New patterns require a real-app justification per the
// helper's doc comment.
func TestIsLikelySentinel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  bool
	}{
		{"sk_test_abc", true},
		{"SK_TEST_ABC", true}, // case-insensitive
		{"pk_test_xyz", true},
		{"rk_test_123", true},
		{"  pk_test_zzz  ", true}, // trims whitespace
		{"disabled", true},
		{"none", true},
		{"null", true},
		{"false", true},
		{"off", true},
		{"n/a", true},
		{"noop", true},
		{"sk_live_real", false},
		{"some-real-key", false},
		{"", false},
		{"   ", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()
			if got := isLikelySentinel(tt.value); got != tt.want {
				t.Errorf("isLikelySentinel(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// TestBuildBundle_DeterministicOutput confirms repeated calls with the
// same inputs produce byte-identical ImportYAML — required for
// downstream diff-for-review and Phase 5 schema-validation caching.
func TestBuildBundle_DeterministicOutput(t *testing.T) {
	t.Parallel()
	inputs := BundleInputs{
		ProjectName:    "deterministic",
		TargetHostname: "appdev",
		SourceMode:     topology.ModeStandard,
		ServiceType:    "nodejs@22",
		SetupName:      "appdev",
		ZeropsYAMLBody: laravelZeropsYAML,
		RepoURL:        "https://github.com/example/deterministic.git",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "Z_LAST", Value: "z"},
			{Key: "A_FIRST", Value: "a"},
			{Key: "M_MID", Value: "m"},
		},
	}
	classifications := map[string]topology.SecretClassification{
		"Z_LAST":  topology.SecretClassPlainConfig,
		"A_FIRST": topology.SecretClassPlainConfig,
		"M_MID":   topology.SecretClassPlainConfig,
	}
	first, err := BuildBundle(inputs, topology.ExportVariantDev, classifications)
	if err != nil {
		t.Fatalf("first BuildBundle: %v", err)
	}
	second, err := BuildBundle(inputs, topology.ExportVariantDev, classifications)
	if err != nil {
		t.Fatalf("second BuildBundle: %v", err)
	}
	if first.ImportYAML != second.ImportYAML {
		t.Errorf("ImportYAML not deterministic across calls\nfirst:\n%s\nsecond:\n%s", first.ImportYAML, second.ImportYAML)
	}
}

// mustUnmarshal parses an emitted ImportYAML into a map for assertions.
func mustUnmarshal(t *testing.T, body string) map[string]any {
	t.Helper()
	// Strip optional preprocessor header — yaml.Unmarshal treats it as
	// a comment and tolerates either way; the explicit strip avoids
	// any line-1 indentation surprises in tests.
	body = strings.TrimPrefix(body, preprocessorHeader)
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("unmarshal emitted yaml: %v\nbody=%q", err, body)
	}
	return doc
}

func checkServiceField(t *testing.T, svc map[string]any, key string, want any) {
	t.Helper()
	got, ok := svc[key]
	if !ok {
		t.Errorf("service[%q] missing", key)
		return
	}
	if got != want {
		t.Errorf("service[%q] = %v, want %v", key, got, want)
	}
}
