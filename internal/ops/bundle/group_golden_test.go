package bundle

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

// updateGroupGolden rewrites the group recipe's golden files:
//
//	go test ./internal/ops/bundle/ -run TestBuildGroupRecipe_MedusaGolden -update-group-golden
var updateGroupGolden = flag.Bool("update-group-golden", false, "rewrite the group recipe golden files")

// medusaLiveSecrets are the values a Mate's project holds that no file of the
// recipe may carry. Every one is invented.
var medusaLiveSecrets = map[string]string{
	"JWT_SECRET":          "f3c1a9e07b2d4c68a5e19b3d7f04c2a86e1b9d3f5a7c0e24",
	"COOKIE_SECRET":       "9b2e7d4a1c6f3e8b0d5a2c7f4e1b8d3a6c0f9e2b",
	"RELOAD_SECRET":       "r3l0ad-5ecret-7e2a91",
	"SUPERADMIN_PASSWORD": "Juno-Adm1n-2026",
	"SENDGRID_API_KEY":    "SG.Z3VyZ2xlLWxpdmUta2V5.bm90LWEtcmVhbC1zZW5kZ3JpZC1rZXktYXQtYWxs",
	"MEDUSA_ADMIN_TOKEN":  "adm1n-t0ken-medusastage-4b8e",
	"MAILPIT_UI_PASSWORD": "mailpit-ui-9c1f",
}

// medusaInputs is a medusa-shaped Mate as the reconcile reads it: a backend
// and a storefront pair, the data they share, a public-build mail catcher,
// the project's wiring and its secrets. The storefront reaches the backend
// through project variables, and the backend reaches the mail catcher.
func medusaInputs() GroupRecipeInputs {
	const medusaYAML = `zerops:
  - setup: medusadev
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: zsc noop --silent
  - setup: medusaprod
    build:
      base: nodejs@22
      buildCommands:
        - yarn
        - yarn build
      deployFiles: .medusa/server
    run:
      base: nodejs@22
      initCommands:
        - zsc execOnce ${appVersionId} -- npx medusa db:migrate
      start: yarn start
      envVariables:
        DATABASE_URL: ${db_connectionString}/medusa
        REDIS_URL: ${redis_connectionString}
        S3_ENDPOINT: ${storage_apiUrl}
        MEILISEARCH_API_KEY: ${search_masterKey}
        SMTP_HOST: ${mailpit_hostname}
`
	const nextstoreYAML = `zerops:
  - setup: nextstoredev
    run:
      base: nodejs@22
      start: zsc noop --silent
  - setup: nextstoreprod
    build:
      base: nodejs@22
      envVariables:
        NEXT_PUBLIC_MEDUSA_PUBLISHABLE_KEY: ${STORE_PUBLISHABLE_KEY}
        NEXT_PUBLIC_BASE_URL: ${APP_URL}
      buildCommands:
        - yarn
        - yarn build
      deployFiles: ./
    run:
      base: nodejs@22
      start: yarn start
      envVariables:
        MEDUSA_BACKEND_URL: ${MEDUSA_INTERNAL_URL}
`
	s := medusaLiveSecrets
	return GroupRecipeInputs{
		Name:            "medusa",
		Title:           "medusa",
		MateProjectName: "medusa-juno",
		CorePackage:     "LIGHT",
		ProjectEnvs: []ProjectEnvVar{
			{Key: "APP_URL", Value: "https://nextstorestage-${zeropsSubdomainHost}-8000.prg1.zerops.app"},
			{Key: "API_URL", Value: "https://medusastage-${zeropsSubdomainHost}-9000.prg1.zerops.app"},
			{Key: "MEDUSA_INTERNAL_URL", Value: "http://medusastage:9000"},
			{Key: "STOREFRONT_INTERNAL_URL", Value: "http://nextstorestage:8000"},
			{Key: "STORE_PUBLISHABLE_KEY", Value: "${medusastage_CHANNEL_PUBLISHABLE_KEY}"},
			{Key: "SUPERADMIN_EMAIL", Value: "admin@example.com"},
			{Key: "STRIPE_PUBLISHABLE_KEY", Value: ""},
			{Key: "JWT_SECRET", Value: s["JWT_SECRET"]},
			{Key: "COOKIE_SECRET", Value: s["COOKIE_SECRET"]},
			{Key: "RELOAD_SECRET", Value: s["RELOAD_SECRET"]},
			{Key: "SUPERADMIN_PASSWORD", Value: s["SUPERADMIN_PASSWORD"]},
			{Key: "STRIPE_API_KEY", Value: ""},
			{Key: "STRIPE_WEBHOOK_SECRET", Value: ""},
			{Key: "SENDGRID_API_KEY", Value: s["SENDGRID_API_KEY"]},
		},
		Runtimes: []GroupRuntime{
			{
				DevHostname: "medusadev", StageHostname: "medusastage", ServiceType: "nodejs@22",
				RepoURL:   "https://hq.example.com/git/app-1/medusadev.git",
				SetupName: "medusadev", StageSetupName: "medusaprod", ZeropsYAMLBody: medusaYAML,
				SubdomainEnabled: true,
				Scaling:          &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 5, MinRAM: 1, MaxRAM: 8, MinDisk: 1, MaxDisk: 20},
				ServiceEnvs: []ProjectEnvVar{
					{Key: "MEDUSA_WORKER_MODE", Value: "shared"},
				},
				StageServiceEnvs: []ProjectEnvVar{
					{Key: "MEDUSA_WORKER_MODE", Value: "server"},
					{Key: "ADMIN_CORS", Value: "https://medusastage-${zeropsSubdomainHost}-9000.prg1.zerops.app"},
					{Key: "MEDUSA_ADMIN_TOKEN", Value: s["MEDUSA_ADMIN_TOKEN"], Sensitive: true},
				},
			},
			{
				DevHostname: "nextstoredev", StageHostname: "nextstorestage", ServiceType: "nodejs@22",
				RepoURL:   "https://hq.example.com/git/app-1/nextstoredev",
				SetupName: "nextstoredev", ZeropsYAMLBody: nextstoreYAML,
				SubdomainEnabled: true,
				Scaling:          &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 5, MinRAM: 1, MaxRAM: 4, MinDisk: 1, MaxDisk: 10},
				StageServiceEnvs: []ProjectEnvVar{
					{Key: "OPENAI_API_KEY", Value: "REDACTED", Sensitive: true},
				},
			},
		},
		Utilities: []GroupUtility{{
			Hostname: "mailpit", ServiceType: "alpine@3.21",
			BuildFromGit:     "https://github.com/zerops-recipe-apps/mailpit-app",
			SubdomainEnabled: true,
			Scaling:          &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 2, MinRAM: 0.25, MaxRAM: 1},
			ServiceEnvs:      []ProjectEnvVar{{Key: "MP_UI_AUTH", Value: "admin:" + s["MAILPIT_UI_PASSWORD"]}},
		}},
		ManagedServices: []ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@17", Profile: "oltp-hobby", Scaling: &Scaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 3, MinRAM: 0.25, MaxRAM: 4, MinDisk: 1, MaxDisk: 20}},
			{Hostname: "redis", Type: "valkey@7.2", Profile: "hobby", Scaling: &Scaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 2, MinRAM: 0.25, MaxRAM: 2, MinDisk: 1, MaxDisk: 5}},
			{Hostname: "search", Type: "meilisearch@1.20", Scaling: &Scaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 3, MinRAM: 0.5, MaxRAM: 4, MinDisk: 1, MaxDisk: 20}},
			{Hostname: "storage", Type: "object-storage", QuotaGBytes: 5, ObjectStoragePolicy: "public-read"},
		},
		HAIncapable: []string{"search"},
	}
}

// The whole of a medusa-shaped Mate, composed: every tier pinned byte for
// byte, valid against the import schema's structure, and carrying none of the
// project's secrets. A change to what a tier says shows here as a diff a
// person can read.
func TestBuildGroupRecipe_MedusaGolden(t *testing.T) {
	t.Parallel()
	layout, warnings, err := BuildGroupRecipe(medusaInputs())
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	if len(layout.Tiers) != 3 {
		t.Fatalf("tiers = %d, want 3", len(layout.Tiers))
	}
	if !warningsContain(warnings, `"search"`) {
		t.Errorf("warnings %v do not say search stays single-node", warnings)
	}
	for _, tier := range layout.Tiers {
		t.Run(tier.Title, func(t *testing.T) {
			t.Parallel()
			golden := filepath.Join("testdata", "group_recipe", "medusa-"+tier.Slug+".yaml")
			if *updateGroupGolden {
				if err := os.WriteFile(golden, []byte(tier.ImportYAML), 0o600); err != nil {
					t.Fatalf("write %s: %v", golden, err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read %s: %v (run with -update-group-golden to write it)", golden, err)
			}
			if tier.ImportYAML != string(want) {
				t.Errorf("tier %q differs from %s:\n--- got\n%s\n--- want\n%s", tier.Title, golden, tier.ImportYAML, want)
			}
			if errs := schema.ValidateImportYAMLStructure(tier.ImportYAML); len(errs) > 0 {
				t.Errorf("tier %q fails the import schema: %v", tier.Title, errs)
			}
		})
	}
	for path, body := range groupFiles(t, layout) {
		for key, value := range medusaLiveSecrets {
			if strings.Contains(body, value) {
				t.Errorf("%s carries the live value of %s", path, key)
			}
		}
	}
}
