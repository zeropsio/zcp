package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// launchShapeAppVersionSourceNone mirrors internal/ops's unexported
// appVersionSourceNone constant: the Source value the platform stamps on a
// startWithoutCode bootstrap appVersion (no real build, Build == nil).
const launchShapeAppVersionSourceNone = "NONE"

// launchShapeRowID builds the stable row id for one O6 launch-shape field
// (docs/spec-eval-farm.md §4.4 O6): `launch_shape/<field>`.
func launchShapeRowID(field string) string {
	return "launch_shape/" + field
}

// evaluateLaunchShapeRows evaluates the O6 launch-shape oracle
// (docs/spec-eval-farm.md §4.4 O6, docs/spec-workflows.md §10 pipeline-first
// launch + §10.2b single-token lifecycle): the prod project named by
// cfg.ProdProject exists on the account, every runtime service in it was
// imported with startWithoutCode (no buildFromGit), the first release on
// each runtime is its first build, and the launch token never appears
// verbatim in the transcript or captured tool-call argument text.
//
// transcriptText/toolCallTexts and launchTokenSHA256 are supplied by the
// caller when available. There is currently no wiring from
// internal/eval/farm's live run (the launch token value, the run
// transcript, and the captured MCP tool-call text) into this call site —
// that integration is out of this slice's write-set (verification.go /
// scenario.go only; internal/eval/farm/** excluded). With an empty
// launchTokenSHA256 the token_not_in_transcript row reports blocked rather
// than silently passing or being omitted, so a scenario that declares
// launchShape always gets five rows.
func evaluateLaunchShapeRows(
	ctx context.Context,
	cfg *LaunchShapeConfig,
	client platform.Client,
	transcriptText string,
	toolCallTexts []string,
	launchTokenSHA256 string,
) []RequiredCheck {
	if cfg == nil {
		return nil
	}
	var rows []RequiredCheck

	scope := launchShapeScope(cfg)
	prodProject, projectRow := resolveLaunchProdProject(ctx, client, cfg)
	rows = append(rows, projectRow)
	if prodProject == nil {
		reason := "prod project not found or not resolvable: " + projectRow.Message
		rows = append(rows,
			notRunLaunchShapeRow("runtimes_start_without_code", scope, reason),
			notRunLaunchShapeRow("no_build_from_git", scope, reason),
			notRunLaunchShapeRow("first_release_is_first_build", scope, reason),
		)
	} else {
		rows = append(rows, evaluateLaunchProdRuntimeRows(ctx, client, prodProject.ID)...)
	}

	rows = append(rows, evaluateTokenNotInTranscriptRow(transcriptText, toolCallTexts, launchTokenSHA256))
	return rows
}

// notRunLaunchShapeRow builds a not-run row for a launch_shape field that
// could not be evaluated because the prod project itself was not found.
func notRunLaunchShapeRow(field, scope, message string) RequiredCheck {
	return RequiredCheck{
		ID: launchShapeRowID(field), Check: "launch_shape", Scope: scope,
		Result: CheckNotRun, Message: message,
	}
}

// launchShapeScope returns the row Scope identifier for a launch shape
// config: the prod project NAME when resolving by name, or the env VAR
// NAME when resolving by id (cfg.ProdProjectIDEnv) — never the id value
// itself, which may be a caller-supplied secret-adjacent identifier.
func launchShapeScope(cfg *LaunchShapeConfig) string {
	if cfg.ProdProjectIDEnv != "" {
		return cfg.ProdProjectIDEnv
	}
	return cfg.ProdProject
}

// resolveLaunchProdProject dispatches to name-based or id-based prod
// project resolution depending on which of cfg.ProdProject /
// cfg.ProdProjectIDEnv is set (validate() guarantees exactly one).
func resolveLaunchProdProject(ctx context.Context, client platform.Client, cfg *LaunchShapeConfig) (*platform.Project, RequiredCheck) {
	if cfg.ProdProjectIDEnv != "" {
		return findLaunchProdProjectByIDEnv(ctx, client, cfg.ProdProjectIDEnv)
	}
	return findLaunchProdProject(ctx, client, cfg.ProdProject)
}

// findLaunchProdProjectByIDEnv resolves launchShape.prodProjectIdEnv
// (docs/spec-eval-farm.md §4.1/§4.4 O6): reads the project id from the
// named environment variable and resolves it via a direct
// client.GetProject read (not a name search, unlike findLaunchProdProject).
// The project_exists row's Scope is the env NAME, never the id value. An
// unset/empty env var, or a GetProject failure, blocks the row rather than
// failing it — the id was never available to check.
func findLaunchProdProjectByIDEnv(ctx context.Context, client platform.Client, envName string) (*platform.Project, RequiredCheck) {
	id := launchShapeRowID("project_exists")
	if client == nil {
		return nil, RequiredCheck{ID: id, Check: "launch_shape", Scope: envName, Result: CheckBlocked, Message: "no platform client available"}
	}
	projectID := os.Getenv(envName)
	if projectID == "" {
		return nil, RequiredCheck{
			ID: id, Check: "launch_shape", Scope: envName, Result: CheckBlocked,
			Message: fmt.Sprintf("resource %s missing", envName),
		}
	}
	project, err := client.GetProject(ctx, projectID)
	if err != nil {
		return nil, RequiredCheck{
			ID: id, Check: "launch_shape", Scope: envName, Result: CheckBlocked, Source: "GetProject",
			Message: fmt.Sprintf("GetProject failed: %v", err),
		}
	}
	return project, RequiredCheck{
		ID: id, Check: "launch_shape", Scope: envName, Result: CheckPassed,
		Expected: "exists", Observed: "found", Source: "GetProject",
		Message: fmt.Sprintf("project resolved by id from env %s", envName),
	}
}

// findLaunchProdProject resolves cfg.ProdProject against the account's
// project list (GetUserInfo for the clientID, then ListProjects) and
// returns the matching project plus the project_exists row. A read failure
// (including a 403 from a run-project-scoped key that cannot list account
// projects) blocks the row rather than failing it.
func findLaunchProdProject(ctx context.Context, client platform.Client, name string) (*platform.Project, RequiredCheck) {
	id := launchShapeRowID("project_exists")
	if client == nil {
		return nil, RequiredCheck{ID: id, Check: "launch_shape", Scope: name, Result: CheckBlocked, Message: "no platform client available"}
	}
	info, err := client.GetUserInfo(ctx)
	if err != nil {
		return nil, RequiredCheck{
			ID: id, Check: "launch_shape", Scope: name, Result: CheckBlocked, Source: "GetUserInfo",
			Message: fmt.Sprintf("GetUserInfo failed: %v", err),
		}
	}
	projects, err := client.ListProjects(ctx, info.ID)
	if err != nil {
		return nil, RequiredCheck{
			ID: id, Check: "launch_shape", Scope: name, Result: CheckBlocked, Source: "ListProjects",
			Message: fmt.Sprintf("ListProjects failed: %v", err),
		}
	}
	for i := range projects {
		if projects[i].Name == name {
			return &projects[i], RequiredCheck{
				ID: id, Check: "launch_shape", Scope: name, Result: CheckPassed,
				Expected: "exists", Observed: "found", Source: "ListProjects",
				Message: fmt.Sprintf("project %q found", name),
			}
		}
	}
	return nil, RequiredCheck{
		ID: id, Check: "launch_shape", Scope: name, Result: CheckFailed,
		Expected: "exists", Observed: "not found", Source: "ListProjects",
		Message: fmt.Sprintf("prod project %q not found in account project list", name),
	}
}

// evaluateLaunchProdRuntimeRows evaluates the three runtime-shape rows
// (runtimes_start_without_code, no_build_from_git,
// first_release_is_first_build) against the prod project's runtime
// services and their appVersion history. Uses ListServicesDirect (lag-free)
// plus SearchAppVersions (ES-backed; only read after ListServicesDirect
// resolved the project, and only to classify history — CLAUDE.md's ES-lag
// caveat applies, so an appVersion for a just-created service may be
// briefly absent).
func evaluateLaunchProdRuntimeRows(ctx context.Context, client platform.Client, projectID string) []RequiredCheck {
	services, err := client.ListServicesDirect(ctx, projectID)
	if err != nil {
		msg := fmt.Sprintf("ListServicesDirect failed: %v", err)
		return []RequiredCheck{
			blockedLaunchShapeRow("runtimes_start_without_code", projectID, "ListServicesDirect", msg),
			blockedLaunchShapeRow("no_build_from_git", projectID, "ListServicesDirect", msg),
			blockedLaunchShapeRow("first_release_is_first_build", projectID, "ListServicesDirect", msg),
		}
	}

	var runtimes []platform.ServiceStack
	for _, svc := range services {
		if svc.IsSystem() {
			continue
		}
		if topology.IsRuntimeType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			runtimes = append(runtimes, svc)
		}
	}
	if len(runtimes) == 0 {
		msg := "prod project has no runtime services"
		return []RequiredCheck{
			blockedLaunchShapeRow("runtimes_start_without_code", projectID, "ListServicesDirect", msg),
			blockedLaunchShapeRow("no_build_from_git", projectID, "ListServicesDirect", msg),
			blockedLaunchShapeRow("first_release_is_first_build", projectID, "ListServicesDirect", msg),
		}
	}

	appVersions, err := client.SearchAppVersions(ctx, projectID, 0)
	if err != nil {
		msg := fmt.Sprintf("SearchAppVersions failed: %v", err)
		return []RequiredCheck{
			blockedLaunchShapeRow("runtimes_start_without_code", projectID, "SearchAppVersions", msg),
			blockedLaunchShapeRow("no_build_from_git", projectID, "SearchAppVersions", msg),
			blockedLaunchShapeRow("first_release_is_first_build", projectID, "SearchAppVersions", msg),
		}
	}
	byService := groupAppVersionsByService(appVersions)

	var noCodeFails, buildFails []string
	for _, svc := range runtimes {
		history := byService[svc.ID]
		sort.SliceStable(history, func(i, j int) bool { return history[i].Created < history[j].Created })

		hasGitBuild := false
		for i, av := range history {
			if av.Source == "GIT" || av.PublicGitSource != nil {
				// O7 notes a CLI push also populates Build (finding E4), so
				// a build carrying neither is the only shape that proves an
				// independent buildFromGit build happened on a prod runtime.
				hasGitBuild = true
			}
			if i == 0 && av.Build == nil && av.Source != launchShapeAppVersionSourceNone {
				// A first appVersion carrying neither a build nor the
				// startWithoutCode marker is evidence outside the
				// documented shape (appVersionSourceNone, internal/ops).
				noCodeFails = append(noCodeFails, fmt.Sprintf("%s: first appVersion source=%q has no startWithoutCode marker", svc.Name, av.Source))
			}
		}
		if len(history) == 0 {
			noCodeFails = append(noCodeFails, fmt.Sprintf("%s: no appVersion history found", svc.Name))
		} else if history[0].Build != nil {
			noCodeFails = append(noCodeFails, fmt.Sprintf("%s: first appVersion already carries a build", svc.Name))
		}
		if hasGitBuild {
			buildFails = append(buildFails, svc.Name)
		}
	}

	rows := []RequiredCheck{
		launchShapeRuntimeRow("runtimes_start_without_code", projectID, noCodeFails, "every runtime imported with startWithoutCode"),
		launchShapeRuntimeRow("no_build_from_git", projectID, buildFails, "no prod runtime carries a build (buildFromGit was not used at import)"),
		launchShapeRuntimeRow("first_release_is_first_build", projectID, firstReleaseNotFirstBuildFails(runtimes, byService), "the earliest built appVersion is the earliest ACTIVE one"),
	}
	return rows
}

// firstReleaseNotFirstBuildFails checks, per runtime, that the earliest
// appVersion carrying a build is also the earliest one reported ACTIVE —
// i.e. the pipeline's first release IS the first build (docs/spec-workflows.md §10).
func firstReleaseNotFirstBuildFails(runtimes []platform.ServiceStack, byService map[string][]platform.AppVersionEvent) []string {
	var fails []string
	for _, svc := range runtimes {
		history := byService[svc.ID]
		sort.SliceStable(history, func(i, j int) bool { return history[i].Created < history[j].Created })
		var firstBuildIdx, firstActiveIdx = -1, -1
		for i, av := range history {
			if av.Build != nil && firstBuildIdx == -1 {
				firstBuildIdx = i
			}
			if av.Status == "ACTIVE" && firstActiveIdx == -1 {
				firstActiveIdx = i
			}
		}
		if firstBuildIdx == -1 {
			continue // no build yet; nothing to violate
		}
		if firstActiveIdx == -1 || firstActiveIdx != firstBuildIdx {
			fails = append(fails, fmt.Sprintf("%s: first build is not the first ACTIVE appVersion", svc.Name))
		}
	}
	return fails
}

func groupAppVersionsByService(events []platform.AppVersionEvent) map[string][]platform.AppVersionEvent {
	out := map[string][]platform.AppVersionEvent{}
	for _, e := range events {
		out[e.ServiceStackID] = append(out[e.ServiceStackID], e)
	}
	return out
}

func blockedLaunchShapeRow(field, scope, source, message string) RequiredCheck {
	return RequiredCheck{ID: launchShapeRowID(field), Check: "launch_shape", Scope: scope, Result: CheckBlocked, Source: source, Message: message}
}

func launchShapeRuntimeRow(field, scope string, fails []string, expected string) RequiredCheck {
	if len(fails) == 0 {
		return RequiredCheck{
			ID: launchShapeRowID(field), Check: "launch_shape", Scope: scope,
			Result: CheckPassed, Expected: expected, Observed: "none", Source: "SearchAppVersions",
			Message: "no violation found",
		}
	}
	return RequiredCheck{
		ID: launchShapeRowID(field), Check: "launch_shape", Scope: scope,
		Result: CheckFailed, Expected: expected, Observed: strings.Join(fails, "; "), Source: "SearchAppVersions",
		Message: strings.Join(fails, "; "),
	}
}

// evaluateTokenNotInTranscriptRow evaluates the token_not_in_transcript row
// (docs/spec-workflows.md §10.2b): every candidate token of transcriptText
// plus every string in toolCallTexts is sha256-hashed and compared against
// launchTokenSHA256 — the value itself is never handled here, only its
// digest. Finding E3: the transcript is JSONL, so a leaked token typically
// appears as `"ZCP_LAUNCH_TOKEN=abc…"` or `"abc…",` — splitting only on
// whitespace never isolates the token from its surrounding quotes/`=`/`,`,
// so the digest never matched and a leak silently passed. launchTokenTokens
// splits on every character outside [A-Za-z0-9_-] instead.
func evaluateTokenNotInTranscriptRow(transcriptText string, toolCallTexts []string, launchTokenSHA256 string) RequiredCheck {
	id := launchShapeRowID("token_not_in_transcript")
	if launchTokenSHA256 == "" {
		return RequiredCheck{
			ID: id, Check: "launch_shape", Scope: "launch_token", Result: CheckBlocked,
			Message: "no launch token sha256 supplied to the verifier (no farm-layer wiring for the token value yet)",
		}
	}
	texts := make([]string, 0, len(toolCallTexts)+1)
	if transcriptText != "" {
		texts = append(texts, transcriptText)
	}
	texts = append(texts, toolCallTexts...)

	for _, text := range texts {
		for _, tok := range launchTokenTokens(text) {
			if sha256Hex(tok) == launchTokenSHA256 {
				return RequiredCheck{
					ID: id, Check: "launch_shape", Scope: "launch_token", Result: CheckFailed,
					Expected: "token never appears verbatim", Observed: "matching token found",
					Message: "a token in the transcript/tool-call text hashes to the launch token's sha256",
				}
			}
		}
	}
	return RequiredCheck{
		ID: id, Check: "launch_shape", Scope: "launch_token", Result: CheckPassed,
		Expected: "token never appears verbatim", Observed: "not found",
		Message: "no transcript/tool-call token matched the launch token's sha256",
	}
}

// launchTokenTokens splits text into candidate token substrings on every
// character outside [A-Za-z0-9_-] (finding E3) — a leaked launch token
// embedded in JSONL (a quoted string value, or following `=` with no
// surrounding whitespace) is isolated the same way a whitespace-only split
// would isolate a token in plain text.
func launchTokenTokens(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return !isLaunchTokenRune(r)
	})
}

func isLaunchTokenRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
