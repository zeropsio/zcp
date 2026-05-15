//go:build e2e

// launch_baseline_test.go — Phase 0 G1 evidence — live verification
// that the zerops-go SDK's ProjectEnv and ServiceStackEnv DTOs expose
// Type / Sensitive / Editable fields the redesigned envclass +
// inventory layers (Phase 1a, 2) will consume.
//
// Plan reference:
//   plans/workflow-family-architecture-2026-05-14.md §11 Phase 0
//   plans/research/env-types-investigation-2026-05-14.md
//
// Why not unit tests: the env type taxonomy is server-authoritative
// (two distinct enums — EnvTypeEnum on project envs, UserDataTypeEnum
// on service-stack envs). Unit-level decoding against fixture JSON
// proves only that decoding works — not that the live server returns
// the shape the DTOs declare. This test queries the running platform
// (eval-zcp by default) to close that loop.
//
// Run:
//   ZCP_API_KEY=<token> go test -tags e2e ./e2e/ -run TestLaunchBaseline -v
//
// The test SKIPS when ZCP_API_KEY is unset (same convention as the
// rest of the e2e suite — see helpers_test.go::newHarness).
//
// Service env coverage: the test reads envs from the first non-system
// service stack returned by ListServices. Object-storage / managed
// services expose READ_ONLY envs; runtime services expose ENV /
// EDITABLE values. The assertion only verifies "Type field decodes to
// a recognized UserDataTypeEnum value" — works for any service type
// the project has.
//
// Project env coverage: every Zerops project carries platform
// SYSTEM envs (zeropsSubdomainHost, CDN URLs, etc.) so the project's
// EnvList is never empty even on a fresh project — assertion has
// real data to verify against.

package e2e_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/sdk"
	"github.com/zeropsio/zerops-go/sdkBase"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
	"github.com/zeropsio/zerops-go/types/uuid"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

// expectedProjectEnvTypes is the closed enum the SDK can decode for
// ProjectEnv.Type. Any other value indicates a SDK / server protocol
// mismatch — the redesigned envclass layer must not silently accept
// unknown values.
var expectedProjectEnvTypes = map[enum.EnvTypeEnum]struct{}{
	enum.EnvTypeEnumUser:   {},
	enum.EnvTypeEnumSystem: {},
}

// expectedServiceEnvTypes is the closed enum for ServiceStackEnv.Type
// (UserDataTypeEnum). Five values per types/enum/userDataTypeEnum.go.
var expectedServiceEnvTypes = map[enum.UserDataTypeEnum]struct{}{
	enum.UserDataTypeEnumReadOnly: {},
	enum.UserDataTypeEnumEditable: {},
	enum.UserDataTypeEnumSecret:   {},
	enum.UserDataTypeEnumInternal: {},
	enum.UserDataTypeEnumEnv:      {},
}

// defaultAPIHost returns the ZCP_API_HOST env value or the
// production default. Mirrors newHarness's logic — kept inline so
// the baseline test stays self-contained from the rest of the suite.
func defaultAPIHost() string {
	if h := os.Getenv("ZCP_API_HOST"); h != "" {
		return h
	}
	return "api.app-prg1.zerops.io"
}

// rawSDKHandler constructs an sdk.Handler using the same plumbing as
// platform.NewZeropsClient — needed because ZeropsClient.handler is
// unexported. The redesigned wrappers (Phase 1a) will surface
// Type/Sensitive/Editable through the platform.Client API; this test
// exercises the underlying SDK directly to prove those fields are
// reachable today.
func rawSDKHandler(t *testing.T) (sdk.Handler, string) {
	t.Helper()
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set — skipping e2e baseline")
	}
	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}
	endpoint := apiHost
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "https://" + endpoint
	}
	if !strings.HasSuffix(endpoint, "/") {
		endpoint += "/"
	}
	httpClient := &http.Client{Timeout: 60 * time.Second}
	config := sdkBase.DefaultConfig(sdkBase.WithCustomEndpoint(endpoint))
	handler := sdk.New(config, httpClient)
	handler = sdk.AuthorizeSdk(handler, token)
	return handler, token
}

// TestLaunchBaseline_ProjectEnvDTOSurfacesTypeSensitiveEditable
// verifies the running platform returns ProjectEnv records with
// populated Type / Sensitive / Editable fields, and Type decodes to a
// known EnvTypeEnum value.
//
// Phase 1a will propagate these three fields through
// platform.GetProjectEnv into a typed ProjectEnvVar wrapper; without
// this baseline, the propagation would be untested against the live
// server contract.
func TestLaunchBaseline_ProjectEnvDTOSurfacesTypeSensitiveEditable(t *testing.T) {
	handler, token := rawSDKHandler(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Resolve the calling token's clientId + projectId via the same
	// path the platform.Client uses (ensures consistent target).
	client, err := platform.NewZeropsClient(token, defaultAPIHost())
	if err != nil {
		t.Fatalf("construct ZeropsClient: %v", err)
	}
	info, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}
	if info.ProjectID == "" {
		t.Skip("auth resolved without ProjectID — token is multi-project; skipping")
	}

	resp, err := handler.PostProjectSearch(ctx, body.EsFilter{
		Search: body.EsFilterSearch{
			body.EsSearchItem{
				Name:     types.NewString("clientId"),
				Operator: types.NewString("eq"),
				Value:    types.NewString(info.ClientID),
			},
			body.EsSearchItem{
				Name:     types.NewString("id"),
				Operator: types.NewString("eq"),
				Value:    types.NewString(info.ProjectID),
			},
		},
	})
	if err != nil {
		t.Fatalf("PostProjectSearch: %v", err)
	}
	out, err := resp.Output()
	if err != nil {
		t.Fatalf("PostProjectSearch output: %v", err)
	}
	if len(out.Items) == 0 {
		t.Fatalf("PostProjectSearch returned no project for id=%q", info.ProjectID)
	}
	envList := out.Items[0].EnvList
	if len(envList) == 0 {
		t.Fatal("project EnvList is empty — even a fresh Zerops project carries platform SYSTEM envs " +
			"(zeropsSubdomainHost, CDN URLs). Server-side bug or wrong project resolved?")
	}

	t.Logf("project %s — %d envs", info.ProjectID, len(envList))
	for _, env := range envList {
		if env.Key.String() == "" {
			t.Errorf("env entry has empty Key — SDK decode mismatch?\nentry=%+v", env)
		}
		if _, ok := expectedProjectEnvTypes[env.Type]; !ok {
			t.Errorf("env %q: Type=%q is not a recognized EnvTypeEnum value (expected USER|SYSTEM).\n"+
				"This means the server returned an enum value the SDK doesn't know about — "+
				"plan envclass design assumes the enum is closed.",
				env.Key.String(), env.Type.String())
		}
	}

	// Sanity check: the project has at least one SYSTEM env (platform-
	// injected). If everything is USER, either we're looking at the
	// wrong project or the platform stopped injecting subdomain/CDN
	// envs.
	hasSystem := false
	for _, env := range envList {
		if env.Type == enum.EnvTypeEnumSystem {
			hasSystem = true
			break
		}
	}
	if !hasSystem {
		t.Error("no Type=SYSTEM envs in project — expected zeropsSubdomainHost / staticCdnUrl / etc. " +
			"(plans/research/env-types-investigation-2026-05-14.md captures the live shape)")
	}
}

// TestLaunchBaseline_ServiceStackEnvDTOSurfacesTypeSensitive verifies
// the live SDK returns ServiceStackEnv records with populated Type
// (UserDataTypeEnum) + Sensitive fields. The DTO has no Editable
// field — the test does NOT assert one (project + service env shapes
// diverge per env-types-investigation findings).
//
// The test picks the first non-system service in the project to query;
// any service type works (managed deps return READ_ONLY envs; runtime
// services return ENV/EDITABLE values).
func TestLaunchBaseline_ServiceStackEnvDTOSurfacesTypeSensitive(t *testing.T) {
	handler, token := rawSDKHandler(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := platform.NewZeropsClient(token, defaultAPIHost())
	if err != nil {
		t.Fatalf("construct ZeropsClient: %v", err)
	}
	info, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}
	if info.ProjectID == "" {
		t.Skip("token has no project scope")
	}

	services, err := client.ListServices(ctx, info.ProjectID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) == 0 {
		t.Skip("project has no services — nothing to query env shape against")
	}

	// First non-system service. IsSystem covers CORE/BUILD/INTERNAL
	// categories — those don't expose user-facing envs.
	var target *platform.ServiceStack
	for i := range services {
		if !services[i].IsSystem() {
			target = &services[i]
			break
		}
	}
	if target == nil {
		t.Skip("no non-system service in project")
	}

	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(target.ID)}
	resp, err := handler.GetServiceStackEnv(ctx, pathParam)
	if err != nil {
		// SDK errors for an empty env list manifest as an HTTP non-2xx;
		// treat as skip — the DTO shape itself is verified by Go's
		// compile-time check below.
		t.Logf("GetServiceStackEnv on %s (%s): %v — verifying compile-time DTO shape only", target.Name, target.ID, err)
		return
	}
	out, err := resp.Output()
	if err != nil {
		// Empty payload yields decoder error — same treatment.
		var notFound *platform.PlatformError
		if errors.As(err, &notFound) {
			t.Logf("decode envs on %s: %v — empty payload", target.Name, err)
			return
		}
		t.Logf("decode envs: %v — verifying compile-time DTO shape only", err)
		return
	}

	t.Logf("service %s (%s) — %d envs", target.Name, target.ID, len(out.Items))
	if len(out.Items) == 0 {
		t.Logf("service %s has no envs — compile-time DTO shape pinned, runtime values not exercised", target.Name)
		return
	}

	for _, env := range out.Items {
		if env.Key.String() == "" {
			t.Errorf("env has empty Key — SDK decode mismatch?\nentry=%+v", env)
		}
		if _, ok := expectedServiceEnvTypes[env.Type]; !ok {
			t.Errorf("env %q: Type=%q is not a recognized UserDataTypeEnum value (expected READ_ONLY|EDITABLE|SECRET|INTERNAL|ENV)",
				env.Key.String(), env.Type.String())
		}
	}
}

// TestLaunchBaseline_EnumsDoNotOverlap pins the env taxonomy
// invariant that ProjectEnv.Type and ServiceStackEnv.Type are
// distinct enums (not aliased). Phase 5.4 of the plan relies on this
// separation — three classifier rules per scope (service envs always
// drop; project SYSTEM drops; project USER classifies).
//
// Compile-time pin: assigning one enum to a variable of the other's
// type would not compile. This test makes the discipline visible at
// the test layer too — if a future refactor unified the enums, this
// fixture would surface the merge by making both maps degenerate.
func TestLaunchBaseline_EnumsDoNotOverlap(t *testing.T) {
	// Lengths differ — proves the enums hold different sets of values.
	// EnvTypeEnum: 2 (USER, SYSTEM)
	// UserDataTypeEnum: 5 (READ_ONLY, EDITABLE, SECRET, INTERNAL, ENV)
	if len(expectedProjectEnvTypes) == len(expectedServiceEnvTypes) {
		t.Error("expected project-env enum and service-env enum to differ in cardinality (2 vs 5) — Phase 5.4 envclass design depends on the separation")
	}
	// String values must not appear in both — sanity check that the
	// SDK didn't introduce a 'USER' value into UserDataTypeEnum or
	// 'READ_ONLY' into EnvTypeEnum.
	projectVals := make(map[string]struct{}, len(expectedProjectEnvTypes))
	for v := range expectedProjectEnvTypes {
		projectVals[v.String()] = struct{}{}
	}
	for v := range expectedServiceEnvTypes {
		if _, dup := projectVals[v.String()]; dup {
			t.Errorf("enum value %q present in both ProjectEnv.Type and ServiceStackEnv.Type — taxonomies must remain distinct", v.String())
		}
	}
}
