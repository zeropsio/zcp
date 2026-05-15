// Live end-to-end test of Phase 2 launch-production composer + ProjectAdminClient
// against the real eval-zcp platform. Composes the bundle via the same path
// ZCP's handler uses (ops.BuildLaunchBundle + envclass-mediated classifications),
// then mutates against the real platform with a one-shot LaunchKey. Verifies
// F19/F20/F21 fixes in the resulting project, then cleans up.
//
// Required env:
//
//	ZCP_E2E_LAUNCH_KEY     account-wide one-shot LaunchKey (canCreateProjects)
//
// Run: go run ./cmd/zcp-launch-live/
//
// This is a one-off operator tool — not part of the test suite. Living
// outside /eval/ deliberately so it doesn't pollute the eval matrix.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/envclass"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

const apiHost = "api.app-prg1.zerops.io"

// Hardcoded Laravel showcase source state (mirrors
// eval/behavioral/scenarios/fixtures/laravel-showcase-deployed.yaml after
// Phase 2 fixture fix). We don't read eval-zcp live because behavioral
// cleanup wiped the services — the launch handler's real flow reads from a
// running source, but this live test is verifying the composer + live API
// integration, not the source-state read path (that's behavioral coverage).
const laravelZeropsYAML = `zerops:
  - setup: app
    build:
      base:
        - php@8.4
      os: alpine
      buildCommands:
        - composer install --ignore-platform-reqs
      deployFiles: ./
      cache:
        - vendor
        - composer.lock
    deploy:
      readinessCheck:
        httpGet:
          port: 80
          path: /up
    run:
      base: php-nginx@8.4
      os: alpine
      envVariables:
        APP_NAME: "Laravel Phase2 Live"
        DB_HOST: ${db_hostname}
      ports:
        - port: 80
          httpSupport: true
`

func main() {
	if err := run(); err != nil {
		log.Fatalf("FATAL: %v", err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	launchKey := os.Getenv("ZCP_E2E_LAUNCH_KEY")
	if launchKey == "" {
		return fmt.Errorf("ZCP_E2E_LAUNCH_KEY required (account-wide one-shot LaunchKey)")
	}

	// 1. Verify LaunchKey capability.
	checkClient, err := platform.NewZeropsClient(launchKey, apiHost)
	if err != nil {
		return fmt.Errorf("verify client: %w", err)
	}
	info, err := checkClient.GetUserInfo(ctx)
	if err != nil {
		return fmt.Errorf("verify GetUserInfo: %w", err)
	}
	log.Printf("LaunchKey verified: userID=%s clientUserID=%s", info.ID, info.ClientUserID)

	// 2. Mirror the fixture's source state (Laravel showcase pair).
	managed := []bundle.ManagedServiceEntry{
		{Hostname: "db", Type: "postgresql@18", Mode: "NON_HA"},
		{Hostname: "cache", Type: "valkey@7.2", Mode: "NON_HA"},
		{Hostname: "storage", Type: "object-storage", Mode: ""},
	}

	// Source project envs that would be on eval-zcp after Laravel seed.
	// SYSTEM envs (auto-injected by platform) get envclass-Drop; USER envs
	// pass through to classification.
	projectEnvs := []platform.ProjectEnvVar{
		{Key: "zeropsSubdomainHost", Content: "abc.zerops.app", Type: platform.ProjectEnvSystem},
		{Key: "staticCdnUrl", Content: "https://static.cdn.example", Type: platform.ProjectEnvSystem},
		{Key: "apiCdnUrl", Content: "https://api.cdn.example", Type: platform.ProjectEnvSystem},
		{Key: "storageCdnUrl", Content: "https://storage.cdn.example", Type: platform.ProjectEnvSystem},
		{Key: "envIsolation", Content: "project", Type: platform.ProjectEnvSystem, Editable: true},
		{Key: "sshIsolation", Content: "service@zcp", Type: platform.ProjectEnvSystem, Editable: true},
		{Key: "APP_NAME", Content: "Laravel Phase2 Live", Type: platform.ProjectEnvUser, Editable: true},
		{Key: "APP_ENV", Content: "production", Type: platform.ProjectEnvUser, Editable: true},
		{Key: "JWT_SECRET", Content: "verify-jwt-secret-live", Type: platform.ProjectEnvUser, Editable: true},
	}

	// 3. envclass-driven classification (mirrors handler).
	var bundleEnvs []ops.ProjectEnvVar
	classifications := map[string]topology.SecretClassification{}
	dropped := 0
	for _, e := range projectEnvs {
		res := envclass.ClassifyProjectEnv(e)
		if res.Decision != envclass.PromptUser {
			dropped++
			continue
		}
		bundleEnvs = append(bundleEnvs, ops.ProjectEnvVar{Key: e.Key, Value: e.Content})
		classifications[e.Key] = res.Bias
	}
	log.Printf("envclass dropped %d SYSTEM envs (F19); %d USER envs classified", dropped, len(bundleEnvs))
	for _, e := range bundleEnvs {
		log.Printf("  USER env: %s -> bias=%s", e.Key, classifications[e.Key])
	}

	// 4. Compose launch bundle (Phase 2b ServiceTypeRules active).
	prodName := fmt.Sprintf("phase2-live-%d", time.Now().Unix())
	inputs := ops.LaunchBundleInputs{
		SourceProjectID:   "waAzEFn6SBaysG4YE4rv7A",
		TargetProjectName: prodName,
		TargetHostname:    "appdev",
		ServiceType:       "php-nginx@8.4",
		SetupName:         "app",
		RepoURL:           "https://github.com/zeropsio/recipe-laravel-minimal",
		ZeropsYAMLBody:    laravelZeropsYAML,
		GitCommitSHA:      "live-test-" + time.Now().Format("20060102"),
		ProjectEnvs:       bundleEnvs,
		ManagedServices:   managed,
	}
	launchBundle, err := ops.BuildLaunchBundle(inputs, classifications)
	if err != nil {
		return fmt.Errorf("compose bundle: %w", err)
	}
	if len(launchBundle.Errors) > 0 {
		return fmt.Errorf("schema validation errors: %v", launchBundle.Errors)
	}
	log.Printf("\n===== composed yaml (%d bytes) =====\n%s\n===== end =====", len(launchBundle.ImportYAML), launchBundle.ImportYAML)

	// Static assertions on composed YAML — F19/F20/F21 evidence pre-platform.
	yaml := launchBundle.ImportYAML
	for _, system := range []string{"zeropsSubdomainHost", "staticCdnUrl", "apiCdnUrl", "storageCdnUrl", "envIsolation", "sshIsolation"} {
		if strings.Contains(yaml, system) {
			return fmt.Errorf("F19 regression: SYSTEM env %q leaked into composed yaml", system)
		}
	}
	log.Printf("F19 verified: no SYSTEM envs in composed yaml")

	if storageBlock := extractServiceBlock(yaml, "storage"); storageBlock != "" {
		if strings.Contains(storageBlock, "mode:") {
			return fmt.Errorf("F20 regression: storage entry contains `mode:`\n%s", storageBlock)
		}
		if !strings.Contains(storageBlock, "objectStorageSize:") {
			return fmt.Errorf("F21 regression: storage entry missing `objectStorageSize:`\n%s", storageBlock)
		}
		log.Printf("F20+F21 verified: storage entry has objectStorageSize, no mode\n%s", strings.TrimRight(storageBlock, "\n"))
	} else {
		log.Printf("WARN: storage entry not found in yaml; F20/F21 not asserted at compose")
	}

	// 5. Construct ProjectAdminClient with real LaunchKey.
	admin, err := platform.NewProjectAdminClient(launchKey, apiHost)
	if err != nil {
		return fmt.Errorf("project admin client: %w", err)
	}
	defer admin.Close()
	log.Printf("ProjectAdminClient ready; clientUserID=%s", admin.ClientUserID())

	// 6. Real mutation: CreateAndImportProject.
	log.Printf("\n*** calling admin.CreateAndImportProject(prodName=%s) ***", prodName)
	result, err := admin.CreateAndImportProject(ctx, launchBundle.ImportYAML, platform.CreateOpts{})
	if err != nil {
		return fmt.Errorf("CreateAndImportProject: %w", err)
	}
	log.Printf("CREATED -- projectID=%s name=%q", result.ProjectID, result.ProjectName)
	for _, s := range result.ServiceStacks {
		errStr := ""
		if s.Error != nil {
			errStr = " ERROR=" + s.Error.Message
		}
		log.Printf("  service: id=%s name=%s%s", s.ID, s.Name, errStr)
	}

	// 7. Verify new project shape via real API.
	newSvcs, err := admin.ListServices(ctx, result.ProjectID)
	if err != nil {
		log.Printf("WARN: ListServices on new project: %v", err)
	} else {
		log.Printf("\n*** new project listing -- %d services ***", len(newSvcs))
		for _, s := range newSvcs {
			typ := s.ServiceStackTypeInfo.ServiceStackTypeVersionName
			log.Printf("  %s  type=%s  status=%s  mode=%s", s.Name, typ, s.Status, s.Mode)
		}
	}

	// 8. Cleanup -- delete the test project (regardless of success above).
	log.Printf("\n*** cleanup: admin.DeleteProject(%s) ***", result.ProjectID)
	delProc, err := admin.DeleteProject(ctx, result.ProjectID)
	if err != nil {
		log.Printf("WARN: DeleteProject failed: %v (project remains; remove manually via dashboard)", err)
	} else if delProc != nil {
		log.Printf("delete process initiated: id=%s status=%s", delProc.ID, delProc.Status)
	}

	log.Printf("\n*** PHASE 2 LIVE TEST COMPLETE ***")
	return nil
}

// extractServiceBlock returns the yaml indented block for a service entry
// matching hostname=h.
func extractServiceBlock(yaml, h string) string {
	for _, prefix := range []string{fmt.Sprintf("    - hostname: %s\n", h), fmt.Sprintf("  - hostname: %s\n", h)} {
		idx := strings.Index(yaml, prefix)
		if idx < 0 {
			continue
		}
		rest := yaml[idx:]
		end := strings.Index(rest[len(prefix):], "- hostname:")
		if end < 0 {
			return rest
		}
		return rest[:end+len(prefix)]
	}
	return ""
}
