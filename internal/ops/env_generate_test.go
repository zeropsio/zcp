package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

//nolint:maintidx // single table-driven test, intentional broad coverage of the resolver paths
func TestEnvGenerateDotenv_ResolvesRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		zeropsYml    string
		hostname     string
		serviceEnvs  map[string][]platform.ServiceEnvVar
		projectEnvs  []platform.ProjectEnvVar
		wantVars     int
		wantServices int
		wantContains []string
		wantErr      string
	}{
		{
			name: "cross-service references resolved",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {
					{ID: "e1", Key: "hostname", Content: "db"},
					{ID: "e2", Key: "port", Content: "5432"},
				},
			},
			wantVars:     2,
			wantServices: 1,
			wantContains: []string{"DB_HOST=db", "DB_PORT=5432"},
		},
		{
			name: "project-level env vars appended",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        DB_HOST: ${db_hostname}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {
					{ID: "e1", Key: "hostname", Content: "db"},
				},
			},
			projectEnvs: []platform.ProjectEnvVar{
				{ID: "pe1", Key: "APP_KEY", Content: "base64:secretkey"},
			},
			wantVars:     2, // 1 from zerops.yaml + 1 project
			wantServices: 1,
			wantContains: []string{"DB_HOST=db", "APP_KEY=base64:secretkey"},
		},
		{
			name: "static value passthrough",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        NODE_ENV: production
        DB_HOST: ${db_hostname}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {
					{ID: "e1", Key: "hostname", Content: "db"},
				},
			},
			wantVars:     2,
			wantServices: 1,
			wantContains: []string{"NODE_ENV=production", "DB_HOST=db"},
		},
		{
			// Compound expression: ${...} refs embedded inside a larger
			// string. The platform substitutes inline at deploy time; the
			// local .env must do the same so DATABASE_URL works against the
			// VPN'd managed service. Reproducer: behavioral eval suite
			// 20260506-145922 — agent wrote a Postgres URL with embedded
			// refs and got literal `${db_user}` in the .env, breaking
			// `npm start` against the VPN.
			name: "compound URL with multiple cross-service refs",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        DATABASE_URL: postgresql://${db_user}:${db_password}@db:${db_port}/${db_dbName}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {
					{ID: "e1", Key: "user", Content: "appuser"},
					{ID: "e2", Key: "password", Content: "s3cret"},
					{ID: "e3", Key: "port", Content: "5432"},
					{ID: "e4", Key: "dbName", Content: "main"},
				},
			},
			wantVars:     1,
			wantServices: 1,
			wantContains: []string{"DATABASE_URL=postgresql://appuser:s3cret@db:5432/main"},
		},
		{
			// Mix: lone ref + compound ref + static value, all in one yaml.
			// Each variable must resolve independently.
			name: "mixed lone + compound + static",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        DATABASE_URL: postgresql://${db_user}@${db_hostname}:${db_port}/main
        NODE_ENV: production
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {
					{ID: "e1", Key: "hostname", Content: "db"},
					{ID: "e2", Key: "user", Content: "u"},
					{ID: "e3", Key: "port", Content: "5432"},
				},
			},
			wantVars:     3,
			wantServices: 1,
			wantContains: []string{
				"DB_HOST=db",
				"DATABASE_URL=postgresql://u@db:5432/main",
				"NODE_ENV=production",
			},
		},
		{
			// Compound with one unresolved ref must error — partial
			// resolution would silently leave a literal `${...}` in the
			// .env, which is exactly the failure mode this whole fix
			// avoids. The error names the unresolved var so the agent
			// can fix the yaml.
			name: "compound with unresolved ref errors",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        DATABASE_URL: postgresql://${db_user}:${db_typoed}@db/main
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {
					{ID: "e1", Key: "user", Content: "u"},
				},
			},
			wantErr: "could not resolve",
		},
		{
			// Recursive expansion: cross-service ref to a value that is
			// itself a sibling-template. Zerops's managed-service
			// `connectionString` follows this shape — `${db_connectionString}`
			// resolves to db.connectionString's value, which is the
			// template `postgresql://${user}:${password}@${hostname}:${port}`
			// where the lone refs are sibling lookups within db's own
			// env. To match deploy-time semantics in the consumer
			// container, the local .env must recurse: expand the cross-
			// service ref, then expand the resulting template against
			// the source service (db) for lone refs.
			name: "recursive: connectionString template fully expands",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        DATABASE_URL: ${db_connectionString}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {
					{ID: "e1", Key: "user", Content: "myuser"},
					{ID: "e2", Key: "password", Content: "s3cret"},
					{ID: "e3", Key: "hostname", Content: "db"},
					{ID: "e4", Key: "port", Content: "5432"},
					{ID: "e5", Key: "connectionString", Content: "postgresql://${user}:${password}@${hostname}:${port}/main"},
				},
			},
			wantVars:     1,
			wantServices: 1,
			wantContains: []string{"DATABASE_URL=postgresql://myuser:s3cret@db:5432/main"},
		},
		{
			// Recursive expansion across a cross-service hop in a fetched
			// template. db's connectionString template embeds a
			// ${cache_url} cross-service ref (rare but a valid Zerops
			// pattern when one managed service composes another's URL).
			// The recursive expander must follow the chain.
			name: "recursive: nested cross-service ref inside fetched value",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        DB_URL: ${db_composed}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {
					{ID: "d1", Key: "composed", Content: "db@${cache_url}"},
				},
				"cache": {
					{ID: "c1", Key: "url", Content: "redis://cache:6379"},
				},
			},
			wantVars:     1,
			wantServices: 2,
			wantContains: []string{"DB_URL=db@redis://cache:6379"},
		},
		{
			// Cycle detection: db.x references db.y references db.x.
			// Without cycle detection, the recursive expander would
			// loop forever (or hit the depth limit and produce a
			// nonsense partial). Surface a specific cycle error so the
			// agent can fix the offending env var on the source side.
			name: "recursive: cycle detection errors",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        FOO: ${db_x}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {
					{ID: "d1", Key: "x", Content: "${y}"},
					{ID: "d2", Key: "y", Content: "${x}"},
				},
			},
			wantErr: "circular",
		},
		{
			// Top-level lone refs (no underscore, no source-service context)
			// stay literal — they're either project-level vars (handled by
			// the GetProjectEnv pass) or runtime placeholders that the
			// deploy-time container resolves. The recursive expander must
			// NOT try to resolve them as cross-service refs.
			name: "recursive: top-level lone ref stays literal",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        URL: https://${hostname}:${port}/api
`,
			hostname:     "app",
			serviceEnvs:  map[string][]platform.ServiceEnvVar{},
			wantVars:     1,
			wantServices: 0,
			wantContains: []string{"URL=https://${hostname}:${port}/api"},
		},
		{
			name: "zerops.yaml envVariable takes precedence over project env",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        SHARED_KEY: custom_value
`,
			hostname: "app",
			projectEnvs: []platform.ProjectEnvVar{
				{ID: "pe1", Key: "SHARED_KEY", Content: "project_value"},
			},
			wantVars:     1,
			wantServices: 0,
			wantContains: []string{"SHARED_KEY=custom_value"},
		},
		{
			name: "no setup entry for hostname",
			zeropsYml: `zerops:
  - setup: other
    run:
      envVariables:
        FOO: bar
`,
			hostname: "app",
			wantErr:  "no setup entry",
		},
		{
			name: "no envVariables and no project envs — nothing to render",
			zeropsYml: `zerops:
  - setup: app
    build:
      base: nodejs@22
`,
			hostname: "app",
			wantErr:  "no env vars to render",
		},
		{
			name: "top-level envVariables ignored (schema requires run.envVariables)",
			zeropsYml: `zerops:
  - setup: app
    envVariables:
      DB_HOST: ${db_hostname}
`,
			hostname: "app",
			// Top-level envVariables is not read (run.envVariables is the
			// only valid location) — with no project envs either, the plan
			// has zero keys, so this is a "nothing to render", not a misread.
			wantErr: "no env vars to render",
		},
		{
			// A setup with no run.envVariables is a valid local bridge when
			// project envs contribute — the old run.envVariables-input guard
			// wrongly rejected it. BuildEnvPlan layers project envs in.
			name: "project-only plan (no run.envVariables) renders from project envs",
			zeropsYml: `zerops:
  - setup: app
    build:
      base: nodejs@22
`,
			hostname:     "app",
			projectEnvs:  []platform.ProjectEnvVar{{ID: "pe1", Key: "APP_NAME", Content: "myapp"}},
			wantVars:     1,
			wantServices: 0,
			wantContains: []string{"APP_NAME=myapp"},
		},
		{
			name: "unresolved reference",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        DB_HOST: ${db_hostname}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {
					{ID: "e1", Key: "port", Content: "5432"},
				},
			},
			wantErr: "could not resolve",
		},
		{
			// Dash-bearing hostname: Zerops accepts hyphens in service
			// names but env-var refs swap them for underscores. Without
			// longest-prefix matching against live hostnames the splitter
			// reads `${my_db_hostname}` as host="my", var="db_hostname"
			// and reports "service not found". Verified live in
			// behavioral eval suite 20260506-145922.
			name: "dash hostname matched via underscore canonical form",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        DB_HOST: ${my_db_hostname}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"my-db": {
					{ID: "e1", Key: "hostname", Content: "my-db"},
				},
			},
			wantVars:     1,
			wantServices: 1,
			wantContains: []string{"DB_HOST=my-db"},
		},
		{
			// Top-level lone ref WITH underscore must stay literal, not
			// blow up as "service not found". The splitter used to treat
			// `${SOME_PROJECT_VAR}` as host=SOME / var=PROJECT_VAR — there
			// is no service "SOME", so the whole generate-dotenv aborted
			// even when the user only wanted a project-level var.
			name: "top-level lone ref with underscore stays literal",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        FALLBACK: ${SOME_PROJECT_VAR}
`,
			hostname:     "app",
			serviceEnvs:  map[string][]platform.ServiceEnvVar{},
			wantVars:     1,
			wantServices: 0,
			wantContains: []string{"FALLBACK=${SOME_PROJECT_VAR}"},
		},
		{
			// A cross-service ref to a sibling var that is legitimately
			// EMPTY must resolve to empty, not be miscounted as unresolved
			// (which hard-failed the whole .env). findEnvValue had no
			// found/absent distinction.
			name: "cross-service ref to an empty sibling var resolves to empty",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        OPTIONAL_FLAG: ${db_EMPTY}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {{ID: "e1", Key: "EMPTY", Content: ""}},
			},
			wantVars:     1,
			wantServices: 1,
			wantContains: []string{"OPTIONAL_FLAG="},
		},
		{
			// A lone ref inside a sibling's VALUE (e.g. db's CONN holding
			// ${BASE_HOST}) must fall back to project env — project vars
			// inherit into every container live, so Zerops resolves it.
			// The sibling cache (slim + app-version) lacks project, so this
			// used to stay unresolved and hard-fail.
			name: "lone ref inside a sibling value falls back to project env",
			zeropsYml: `zerops:
  - setup: app
    run:
      envVariables:
        DB_URL: ${db_CONN}
`,
			hostname: "app",
			serviceEnvs: map[string][]platform.ServiceEnvVar{
				"db": {{ID: "e1", Key: "CONN", Content: "${BASE_HOST}/db"}},
			},
			projectEnvs:  []platform.ProjectEnvVar{{ID: "pe1", Key: "BASE_HOST", Content: "example.com"}},
			wantVars:     2,
			wantServices: 1,
			wantContains: []string{"DB_URL=example.com/db", "BASE_HOST=example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(tt.zeropsYml), 0644); err != nil {
				t.Fatalf("write zerops.yaml: %v", err)
			}

			services := make([]platform.ServiceStack, 0, 1+len(tt.serviceEnvs))
			services = append(services, platform.ServiceStack{
				ID: "svc-app", Name: "app", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			})
			for svcName := range tt.serviceEnvs {
				services = append(services, platform.ServiceStack{
					ID: "svc-" + svcName, Name: svcName, ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"},
				})
			}

			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive}).
				WithServices(services)
			for svcName, envs := range tt.serviceEnvs {
				mock = mock.WithServiceEnv("svc-"+svcName, envs)
			}
			if tt.projectEnvs != nil {
				mock = mock.WithProjectEnv(tt.projectEnvs)
			}

			result, err := EnvGenerateDotenv(context.Background(), mock, "proj-1", tt.hostname, tmpDir, EnvGenerateDotenvOptions{})

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Variables != tt.wantVars {
				t.Errorf("variables = %d, want %d", result.Variables, tt.wantVars)
			}
			if result.Services != tt.wantServices {
				t.Errorf("services = %d, want %d", result.Services, tt.wantServices)
			}

			envContent, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
			if err != nil {
				t.Fatalf("read .env: %v", err)
			}
			content := string(envContent)

			for _, want := range tt.wantContains {
				if !strings.Contains(content, want) {
					t.Errorf(".env should contain %q, got:\n%s", want, content)
				}
			}

			if !strings.Contains(content, "Generated by ZCP") {
				t.Error(".env should contain header comment")
			}
		})
	}
}

// TestEnvGenerateDotenv_RuntimeSiblingYamlBaked pins A2: a ref to a runtime
// sibling's yaml-baked run.envVariables var resolves via the app-version
// userDataList (the slim /env omits it). Pre-fix this hard-errored
// "could not resolve env vars". Spec §1/§6.
func TestEnvGenerateDotenv_RuntimeSiblingYamlBaked(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	yml := `zerops:
  - setup: app
    run:
      envVariables:
        UPSTREAM: ${api_API_URL}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{ID: "svc-api", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
				ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-api"}},
		}).
		// slim /env for api has NO API_URL — only the yaml-baked layer does
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{{ID: "e1", Key: "hostname", Content: "api"}}).
		WithAppVersionUserData("av-api", []platform.ServiceEnvVar{{Key: "API_URL", Content: "https://api.internal:3000"}})

	if _, err := EnvGenerateDotenv(context.Background(), mock, "proj-1", "app", tmpDir, EnvGenerateDotenvOptions{}); err != nil {
		t.Fatalf("should resolve sibling yaml-baked ref via app-version, got error: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if !strings.Contains(string(content), "UPSTREAM=https://api.internal:3000") {
		t.Errorf(".env should resolve ${api_API_URL} via app-version; got:\n%s", content)
	}
}

// TestEnvGenerateDotenv_AppVersionTransient_PropagatesTransientError is the RC2
// centerpiece for generate-dotenv (F4/E1): a LIVE sibling's app-version fetch
// failing transiently must surface as *RefResolveTransientError ("run zcli vpn
// up"), NOT a typo-class ErrInvalidParameter ("fix your yaml"). The slim fetch
// already did this; the app-version enrich at env_generate.go:228 did not (it
// silently discarded the error) until RC2.
func TestEnvGenerateDotenv_AppVersionTransient_PropagatesTransientError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	yml := `zerops:
  - setup: app
    run:
      envVariables:
        UPSTREAM: ${api_API_URL}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			// api is a LIVE sibling whose app-version fetch fails transiently.
			{ID: "svc-api", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
				ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-api"}},
		}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{{ID: "e1", Key: "hostname", Content: "api"}}).
		WithError("GetAppVersionUserData", errors.New("vpn down"))

	_, err := EnvGenerateDotenv(context.Background(), mock, "proj-1", "app", tmpDir, EnvGenerateDotenvOptions{})
	if err == nil {
		t.Fatal("expected a transient error from the failed app-version fetch, got nil")
	}
	var transient *RefResolveTransientError
	if !errors.As(err, &transient) {
		t.Errorf("error must be *RefResolveTransientError (VPN-retry contract), got %T: %v", err, err)
	}
}

// TestEnvGenerateDotenv_NeverDeployedRuntimeSibling_RefKeptLiteralNotUnresolved
// pins the A2 keep-literal branch (env_generate.go:240-251): a ref to a RUNTIME
// sibling that has never been deployed (no active app version → its yaml-baked
// run.envVariables aren't on the platform yet) must stay literal in .env and
// MUST NOT fail generation. The live-runtime case is pinned by
// TestEnvGenerateDotenv_RuntimeSiblingYamlBaked; this is its never-deployed
// companion (was untested — P0 plan-fidelity back-fill).
func TestEnvGenerateDotenv_NeverDeployedRuntimeSibling_RefKeptLiteralNotUnresolved(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	yml := `zerops:
  - setup: app
    run:
      envVariables:
        UPSTREAM: ${api_API_URL}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			// api is a RUNTIME sibling with NO ActiveAppVersion → never-deployed.
			{ID: "svc-api", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{{ID: "e1", Key: "hostname", Content: "api"}})

	result, err := EnvGenerateDotenv(context.Background(), mock, "proj-1", "app", tmpDir, EnvGenerateDotenvOptions{})
	if err != nil {
		t.Fatalf("never-deployed runtime sibling ref must NOT fail .env generation, got: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if !strings.Contains(string(content), "UPSTREAM=${api_API_URL}") {
		t.Errorf("never-deployed sibling ref should stay literal; got:\n%s", content)
	}
	if result.Variables != 1 {
		t.Errorf("Variables = %d, want 1 (UPSTREAM rendered literal, not counted unresolved)", result.Variables)
	}
}

// TestEnvGenerateDotenv_PlatformInternalsFiltered pins that auto-injected
// platform keys (ZCP_API_KEY deploy token, *CdnUrl, env/ssh isolation,
// zeropsSubdomain* runtime placeholders) never land in the local .env.
// The user's typical fat-finger `git add -A` despite .gitignore would
// publish the deploy token. Verified live in suite 20260506-145922 —
// the .env contained ZCP_API_KEY pre-fix.
func TestEnvGenerateDotenv_PlatformInternalsFiltered(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_KEY: base64:secret
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{{
			ID: "svc-app", Name: "app", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		}}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "p1", Key: "ZCP_API_KEY", Content: "deploy-token-leak"},
			{ID: "p2", Key: "envIsolation", Content: "service"},
			{ID: "p3", Key: "sshIsolation", Content: "service"},
			{ID: "p4", Key: "apiCdnUrl", Content: "https://api.cdn.zerops.io"},
			{ID: "p5", Key: "staticCdnUrl", Content: "https://static.cdn.zerops.io"},
			{ID: "p6", Key: "storageCdnUrl", Content: "https://storage.cdn.zerops.io"},
			{ID: "p7", Key: "zeropsSubdomainHost", Content: "app-1234"},
			{ID: "p8", Key: "zeropsSubdomainString", Content: "app-1234.zerops.io"},
			{ID: "p9", Key: "USER_PROJECT_VAR", Content: "kept"},
			{ID: "p10", Key: "GIT_TOKEN", Content: "ghp_deploytoken_leak"},
			{ID: "p11", Key: "ZEROPS_TOKEN_PROD", Content: "staged-launch-token-leak"},
		})

	result, err := EnvGenerateDotenv(context.Background(), mock, "proj-1", "app", tmpDir, EnvGenerateDotenvOptions{})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	lines := strings.Split(string(body), "\n")
	denied := []string{
		"ZCP_API_KEY",
		"GIT_TOKEN",
		"ZEROPS_TOKEN_PROD",
		"envIsolation",
		"sshIsolation",
		"apiCdnUrl",
		"staticCdnUrl",
		"storageCdnUrl",
		"zeropsSubdomainHost",
		"zeropsSubdomainString",
	}
	for _, key := range denied {
		prefix := key + "="
		for _, line := range lines {
			if strings.HasPrefix(line, prefix) {
				t.Errorf(".env has denylisted line %q (full body:\n%s)", line, string(body))
			}
		}
	}
	// User-defined yaml var stays.
	if !strings.Contains(string(body), "APP_KEY=base64:secret") {
		t.Errorf(".env missing user yaml var APP_KEY; got:\n%s", string(body))
	}
	// User project-level var stays.
	if !strings.Contains(string(body), "USER_PROJECT_VAR=kept") {
		t.Errorf(".env missing user project var; got:\n%s", string(body))
	}

	// OmittedPlatformKeys exposes what was filtered for transparency.
	if len(result.OmittedPlatformKeys) != len(denied) {
		t.Errorf("OmittedPlatformKeys len = %d, want %d (got %v)", len(result.OmittedPlatformKeys), len(denied), result.OmittedPlatformKeys)
	}
	keysSet := make(map[string]bool, len(result.OmittedPlatformKeys))
	for _, k := range result.OmittedPlatformKeys {
		keysSet[k] = true
	}
	for _, key := range denied {
		if !keysSet[key] {
			t.Errorf("OmittedPlatformKeys missing %q (got %v)", key, result.OmittedPlatformKeys)
		}
	}
}

// TestEnvGenerateDotenv_YamlRefOverridesDenylist — when the user defines
// a denylisted key explicitly in their yaml `run.envVariables` they meant
// it (e.g. they want a local-only override of a platform-internal name).
// The denylist filters auto-appended project-level vars only.
func TestEnvGenerateDotenv_YamlRefOverridesDenylist(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        ZCP_API_KEY: my-explicit-override
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{{
			ID: "svc-app", Name: "app", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		}}).
		WithProjectEnv([]platform.ProjectEnvVar{
			{ID: "p1", Key: "ZCP_API_KEY", Content: "deploy-token-from-platform"},
		})

	result, err := EnvGenerateDotenv(context.Background(), mock, "proj-1", "app", tmpDir, EnvGenerateDotenvOptions{})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if !strings.Contains(string(body), "ZCP_API_KEY=my-explicit-override") {
		t.Errorf("yaml override missing; got:\n%s", string(body))
	}
	if strings.Contains(string(body), "deploy-token-from-platform") {
		t.Errorf("project ZCP_API_KEY leaked despite yaml override; got:\n%s", string(body))
	}
	// Yaml override means we did NOT filter the project key — it was simply
	// shadowed. Don't surface it in OmittedPlatformKeys.
	for _, k := range result.OmittedPlatformKeys {
		if k == "ZCP_API_KEY" {
			t.Errorf("OmittedPlatformKeys included %q despite yaml override", k)
		}
	}
}

// TestEnvGenerateDotenv_ListServices_CalledOncePerBatch pins the contract
// that ref expansion fetches the project's service list once at the start
// of EnvGenerateDotenv — not lazily per cache miss in the recursive
// expander. With three distinct cross-service refs the unfixed code called
// ListServices three times (one per cache miss inside expandRefs).
func TestEnvGenerateDotenv_ListServices_CalledOncePerBatch(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        CACHE_URL: ${cache_url}
        QUEUE_URL: ${queue_url}
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	services := []platform.ServiceStack{
		{ID: "svc-app", Name: "app", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-db", Name: "db", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
		{ID: "svc-cache", Name: "cache", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey@8"}},
		{ID: "svc-queue", Name: "queue", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nats@2"}},
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test", Status: statusActive}).
		WithServices(services).
		WithServiceEnv("svc-db", []platform.ServiceEnvVar{{ID: "d1", Key: "hostname", Content: "db"}}).
		WithServiceEnv("svc-cache", []platform.ServiceEnvVar{{ID: "c1", Key: "url", Content: "redis://cache:6379"}}).
		WithServiceEnv("svc-queue", []platform.ServiceEnvVar{{ID: "q1", Key: "url", Content: "nats://queue:4222"}})

	if _, err := EnvGenerateDotenv(context.Background(), mock, "proj-1", "app", tmpDir, EnvGenerateDotenvOptions{}); err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}

	if got := mock.CallCounts["ListServices"]; got != 1 {
		t.Errorf("ListServices called %d times, want exactly 1 per batch", got)
	}
}

// TestGenerateDotenv_SetupExplicit_PicksMatchingBlock pins that an
// explicit setup name selects the corresponding zerops.yaml block.
// This is Phase 0C's primary semantic — setup is the canonical
// selector for env generation, distinct from service hostname.
func TestGenerateDotenv_SetupExplicit_PicksMatchingBlock(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        WHICH_SETUP: app-block
  - setup: worker
    run:
      envVariables:
        WHICH_SETUP: worker-block
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		})

	result, err := EnvGenerateDotenv(context.Background(), mock, "p1", "worker", tmpDir, EnvGenerateDotenvOptions{})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}
	if result.Setup != "worker" {
		t.Errorf("result.Setup = %q, want %q", result.Setup, "worker")
	}
	body, _ := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if !strings.Contains(string(body), "WHICH_SETUP=worker-block") {
		t.Errorf(".env should reflect worker-block; got:\n%s", string(body))
	}
}

// TestGenerateDotenv_SetupMissing_SingleBlock_AutoPicks pins that
// empty setup against a single-block yaml auto-picks the only setup.
// Removes friction: agents with single-app projects don't need to
// know the setup name.
func TestGenerateDotenv_SetupMissing_SingleBlock_AutoPicks(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: only-block
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		})

	result, err := EnvGenerateDotenv(context.Background(), mock, "p1", "", tmpDir, EnvGenerateDotenvOptions{})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}
	if result.Setup != "app" {
		t.Errorf("result.Setup = %q, want %q (auto-picked)", result.Setup, "app")
	}
}

// TestGenerateDotenv_SetupMissing_MultipleBlocks_Refuses pins that
// empty setup against a multi-block yaml errors with the available
// setup names. Auto-picking would silently select the wrong setup;
// refusing forces the agent to disambiguate.
func TestGenerateDotenv_SetupMissing_MultipleBlocks_Refuses(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: app-block
  - setup: worker
    run:
      envVariables:
        APP_NAME: worker-block
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		})

	_, err := EnvGenerateDotenv(context.Background(), mock, "p1", "", tmpDir, EnvGenerateDotenvOptions{})
	if err == nil {
		t.Fatal("expected SetupRequiredError, got nil")
	}
	var setupErr *SetupRequiredError
	if !errors.As(err, &setupErr) {
		t.Fatalf("error type = %T, want *SetupRequiredError: %v", err, err)
	}
	if len(setupErr.Available) != 2 {
		t.Errorf("Available setups = %v, want 2 entries", setupErr.Available)
	}
}

// TestGenerateDotenv_LegacyHostname_StillWorks_WithWarning pins that
// legacy callers passing setup-name-via-positional argument keep
// working. The deprecation warning lives at the tool layer
// (TestEnv_GenerateDotenv_LegacyHostname_AddsWarning); ops level
// just verifies the value is accepted as setup.
func TestGenerateDotenv_LegacyHostname_StillWorks_WithWarning(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: legacy-call-site
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		})

	// Legacy callers pass the setup name via the same positional arg
	// that used to be `hostname`. After Phase 0C the parameter is
	// `setup`; back-compat is preserved because the meaning matched
	// in the common case (recipe / classic single-runtime where the
	// hostname IS the setup name).
	result, err := EnvGenerateDotenv(context.Background(), mock, "p1", "app", tmpDir, EnvGenerateDotenvOptions{})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}
	if result.Setup != "app" {
		t.Errorf("result.Setup = %q, want %q", result.Setup, "app")
	}
}

// TestEnvGenerateDotenv_UsesEnvPlanInternally pins the Phase 0B
// refactor: EnvGenerateDotenv routes through BuildEnvPlan, so a
// .env.local overlay in CWD must merge into the rendered .env. Before
// 0B, EnvGenerateDotenv only resolved yaml + project envs — overlay
// support is the BuildEnvPlan-level behavior the wrapper now inherits.
func TestEnvGenerateDotenv_UsesEnvPlanInternally(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_ENV: production
        DB_HOST: ${db_hostname}
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	const overlay = `# user override
APP_ENV=local
LOG_LEVEL=debug
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".env.local"), []byte(overlay), 0600); err != nil {
		t.Fatalf("write .env.local: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
			{ID: "svc-db", Name: "db", ProjectID: "p1", Status: "RUNNING"},
		}).
		WithServiceEnv("svc-db", []platform.ServiceEnvVar{
			{ID: "e1", Key: "hostname", Content: "db"},
		})

	if _, err := EnvGenerateDotenv(context.Background(), mock, "p1", "app", tmpDir, EnvGenerateDotenvOptions{}); err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	got := string(body)

	// Overlay wins for APP_ENV.
	if !strings.Contains(got, "APP_ENV=local") {
		t.Errorf(".env should contain APP_ENV=local (overlay wins); got:\n%s", got)
	}
	if strings.Contains(got, "APP_ENV=production") {
		t.Errorf(".env should NOT contain APP_ENV=production after overlay merge; got:\n%s", got)
	}
	// Overlay-only key lands.
	if !strings.Contains(got, "LOG_LEVEL=debug") {
		t.Errorf(".env should contain LOG_LEVEL=debug from overlay; got:\n%s", got)
	}
	// Yaml-resolved cross-service ref still works.
	if !strings.Contains(got, "DB_HOST=db") {
		t.Errorf(".env should contain DB_HOST=db (yaml ref resolved); got:\n%s", got)
	}
	// New render header naming the three sources.
	if !strings.Contains(got, "project envVariables, zerops.yaml setup app") {
		t.Errorf(".env header should reference EnvPlan source list; got:\n%s", got)
	}
}

// TestGenerateDotenv_PreviewReturnsDiff pins Phase 0D's preview mode:
// Preview=true returns the plan + diff without writing. The .env on
// disk must remain untouched.
func TestGenerateDotenv_PreviewReturnsDiff(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: from-yaml
        DB_HOST: ${db_hostname}
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	// Pre-existing .env so the diff has Modified entries to surface.
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("APP_NAME=outdated\n"), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
			{ID: "svc-db", Name: "db", ProjectID: "p1", Status: "RUNNING"},
		}).
		WithServiceEnv("svc-db", []platform.ServiceEnvVar{
			{ID: "e1", Key: "hostname", Content: "db"},
		})

	result, err := EnvGenerateDotenv(context.Background(), mock, "p1", "app", tmpDir, EnvGenerateDotenvOptions{Preview: true})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}
	if !result.Preview {
		t.Errorf("result.Preview = false, want true")
	}
	if result.Diff == nil {
		t.Fatal("result.Diff is nil; preview must surface the diff")
	}
	if len(result.Diff.Added) == 0 {
		t.Errorf("Diff.Added should include DB_HOST (new in plan); got %v", result.Diff.Added)
	}
	if len(result.Diff.Modified) == 0 {
		t.Errorf("Diff.Modified should include APP_NAME (value changed); got %v", result.Diff.Modified)
	}

	// Critical: .env on disk is unchanged.
	got, _ := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if string(got) != "APP_NAME=outdated\n" {
		t.Errorf("preview should NOT write .env; got:\n%s", string(got))
	}
}

// TestGenerateDotenv_RefusesUnownedEdits pins the safety gate: when
// the existing .env contains keys not produced by any source (user
// manually edited), default write refuses and returns the diff with
// those keys in Unowned. .env stays unchanged. Caller must move them
// to .env.local or pass Force=true.
func TestGenerateDotenv_RefusesUnownedEdits(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: from-yaml
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	const existingEnv = `APP_NAME=from-yaml
USER_MANUAL_EDIT=should-not-be-clobbered
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(existingEnv), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		})

	result, err := EnvGenerateDotenv(context.Background(), mock, "p1", "app", tmpDir, EnvGenerateDotenvOptions{})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}
	if !result.Refused {
		t.Errorf("result.Refused = false, want true (unowned edits should refuse)")
	}
	if result.Diff == nil || len(result.Diff.Unowned) == 0 {
		t.Fatalf("Diff.Unowned should list USER_MANUAL_EDIT; got %v", result.Diff)
	}
	foundUnowned := false
	for _, k := range result.Diff.Unowned {
		if k == "USER_MANUAL_EDIT" {
			foundUnowned = true
		}
	}
	if !foundUnowned {
		t.Errorf("Diff.Unowned should include USER_MANUAL_EDIT; got %v", result.Diff.Unowned)
	}

	// .env unchanged on disk.
	got, _ := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if string(got) != existingEnv {
		t.Errorf("refused write should NOT modify .env; got:\n%s", string(got))
	}
}

// TestGenerateDotenv_ForceOverridesUnowned pins that Force=true
// bypasses the unowned-edit safety gate. The user-direct edits are
// silently dropped on write. Caller has explicitly confirmed they
// know what they're doing.
func TestGenerateDotenv_ForceOverridesUnowned(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: from-yaml
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("APP_NAME=outdated\nUSER_MANUAL=will-be-dropped\n"), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		})

	result, err := EnvGenerateDotenv(context.Background(), mock, "p1", "app", tmpDir, EnvGenerateDotenvOptions{Force: true})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}
	if result.Refused {
		t.Errorf("result.Refused = true with Force; force should bypass safety gate")
	}

	got, _ := os.ReadFile(filepath.Join(tmpDir, ".env"))
	body := string(got)
	if !strings.Contains(body, "APP_NAME=from-yaml") {
		t.Errorf(".env should contain new APP_NAME after force; got:\n%s", body)
	}
	if strings.Contains(body, "USER_MANUAL=will-be-dropped") {
		t.Errorf(".env should NOT contain unowned key after force; got:\n%s", body)
	}
}

// TestGenerateDotenv_PreviewWithUnowned_DoesNotRefuse pins that
// preview mode surfaces unowned edits via the diff but does NOT mark
// the result as refused — preview is read-only by design, so the
// safety gate doesn't apply. Caller inspects the diff and decides
// whether to run with Force=true or move keys to .env.local.
func TestGenerateDotenv_PreviewWithUnowned_DoesNotRefuse(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: from-yaml
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("APP_NAME=from-yaml\nUSER_MANUAL=foo\n"), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		})

	result, err := EnvGenerateDotenv(context.Background(), mock, "p1", "app", tmpDir, EnvGenerateDotenvOptions{Preview: true})
	if err != nil {
		t.Fatalf("EnvGenerateDotenv: %v", err)
	}
	if !result.Preview {
		t.Errorf("result.Preview = false, want true")
	}
	if result.Refused {
		t.Errorf("preview must not set Refused — preview is read-only")
	}
	if result.Diff == nil || len(result.Diff.Unowned) != 1 || result.Diff.Unowned[0] != "USER_MANUAL" {
		t.Errorf("Diff.Unowned = %v, want [USER_MANUAL]", result.Diff)
	}
}

// TestEnvPlan_DiffAgainstExisting_AbsentFile pins that diff against
// a non-existent .env treats every plan key as Added (no error).
// First-time generation flows depend on this — the absence of .env
// is the signal "fresh write", not a failure.
func TestEnvPlan_DiffAgainstExisting_AbsentFile(t *testing.T) {
	t.Parallel()
	plan := &EnvPlan{
		Setup: "app",
		Keys: []EnvKey{
			{Key: "FOO", Value: "1"},
			{Key: "BAR", Value: "2"},
		},
	}
	tmpDir := t.TempDir()
	diff, err := plan.DiffAgainstExisting(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("DiffAgainstExisting: %v", err)
	}
	if len(diff.Added) != 2 {
		t.Errorf("Added = %v, want both keys", diff.Added)
	}
	if len(diff.Modified) != 0 || len(diff.Unowned) != 0 {
		t.Errorf("Modified/Unowned should be empty; got %v", diff)
	}
}

// TestGenerateDotenv_VPNDown_LeavesPriorEnvIntact pins the Phase 0F
// invariant: when a ${svc_var} ref cannot resolve (mock returns an
// error from GetServiceEnv), generate-dotenv fails AND the existing
// .env stays untouched. The user's working .env is more valuable
// than a placeholder write.
func TestGenerateDotenv_VPNDown_LeavesPriorEnvIntact(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        DB_HOST: ${db_hostname}
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	const priorContent = "DB_HOST=prior-value\nAPP_ENV=local\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(priorContent), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	// Mock returns ServiceEnv error (simulates VPN-down / API failure).
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
			{ID: "svc-db", Name: "db", ProjectID: "p1", Status: "RUNNING"},
		}).
		WithError("GetServiceEnv", errors.New("connection refused (VPN down?)"))

	_, err := EnvGenerateDotenv(context.Background(), mock, "p1", "app", tmpDir, EnvGenerateDotenvOptions{})
	if err == nil {
		t.Fatal("expected error from VPN-down resolve, got nil")
	}

	// Critical: .env on disk is unchanged.
	got, _ := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if string(got) != priorContent {
		t.Errorf("prior .env should be untouched on resolve failure; got:\n%s", string(got))
	}
}

// TestGenerateDotenv_ConcurrentInvocations_Serialize pins the Phase 0E
// advisory lock: two concurrent generate-dotenv calls for the same
// setup serialize, neither corrupting the other's write. The lock is
// fairness-best-effort; the contract is "no torn writes, no
// race-induced data loss".
func TestGenerateDotenv_ConcurrentInvocations_Serialize(t *testing.T) {
	t.Parallel()

	const yaml = `zerops:
  - setup: app
    run:
      envVariables:
        APP_NAME: concurrent-test
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "zerops.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-app", Name: "app", ProjectID: "p1", Status: "RUNNING"},
		})

	const N = 4
	errs := make(chan error, N)
	for range N {
		go func() {
			_, err := EnvGenerateDotenv(context.Background(), mock, "p1", "app", tmpDir, EnvGenerateDotenvOptions{})
			errs <- err
		}()
	}
	for i := range N {
		if err := <-errs; err != nil {
			t.Errorf("concurrent invocation %d: %v", i, err)
		}
	}

	// Final .env should be intact (not torn / partial).
	body, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if !strings.Contains(string(body), "APP_NAME=concurrent-test") {
		t.Errorf(".env should contain expected value after concurrent writes; got:\n%s", string(body))
	}
}
