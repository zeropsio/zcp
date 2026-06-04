package tools

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// ImportInput is the input type for zerops_import.
//
// Override is FlexBool so the MCP schema accepts both booleans and
// stringified forms — same rationale as every other MCP-boundary
// boolean input (some LLM agents serialize primitives as quoted
// strings, and the raw-bool schema would reject those at the protocol
// layer with a non-actionable error).
//
// ConfirmDestructive is the diagnose-before-destruct ack required when
// override=true would REPLACE a service that has failed appVersion
// history (plan v4 §3.2). Absent on the first call → handler returns
// ErrDiagnosisRequired with a structured wouldDestroy payload describing
// the targets and loss; second call must echo the operation +
// acknowledgedTargets to proceed.
type ImportInput struct {
	Content            string          `json:"content,omitempty"`
	FilePath           string          `json:"filePath,omitempty"`
	Override           FlexBool        `json:"override,omitempty"`
	ConfirmDestructive *DestructiveAck `json:"confirmDestructive,omitempty"`
}

const importOverrideOperation = "import-override"

// importInputSchema is the explicit InputSchema for zerops_import. Lives
// here rather than on struct tags so `override` can declare the
// `oneOf: [boolean, string]` shape needed by stringified-boolean agents.
func importInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"content": {
			Type:        "string",
			Description: "Inline import YAML content. Provide either content or filePath.",
		},
		"filePath": {
			Type:        "string",
			Description: "Path to a YAML file containing the import definition. Provide either filePath or content.",
		},
		"override": flexBoolSchema("Set override: true on every imported service so the API replaces existing service stacks with matching hostnames. DESTRUCTIVE: replacement tears down the previous container, deployed code, env vars, and the SSHFS mount on those services — back up any uncommitted work first. The response Warnings name the replaced hostnames so the destruction is never silent. Required when re-importing a service that already exists (e.g. to transition READY_TO_DEPLOY to ACTIVE by adding startWithoutCode: true)."),
		"confirmDestructive": {
			Type:        "object",
			Description: "Acknowledgment that override=true may proceed. REQUIRED on the second call when the first call refused with code=DIAGNOSIS_REQUIRED + a structured wouldDestroy payload (services with failed appVersion history). Set operation to wouldDestroy.operation and acknowledgedTargets to the same hostname set wouldDestroy.targets carries. Read zerops_logs / zerops_events for the affected services before acknowledging.",
			Properties: map[string]*jsonschema.Schema{
				"operation": {
					Type:        "string",
					Description: "Must equal wouldDestroy.operation from the first-call refusal (e.g. \"import-override\").",
				},
				"acknowledgedTargets": {
					Type:        "array",
					Items:       &jsonschema.Schema{Type: "string"},
					Description: "Service hostnames being acknowledged for destruction. Must match wouldDestroy.targets as a set (order-insensitive, no extras, no missing).",
				},
				"diagnosedFailureClass": {
					Type:        "string",
					Description: "Optional: the topology FailureClass observed via zerops_events (e.g. \"build\", \"start\"). Future-proofing for stricter enforcement.",
				},
			},
		},
	})
}

// RegisterImport registers the zerops_import tool.
//
// Takes no client-side type catalog — the Zerops API is the single
// validator for everything the import YAML declares. Field / mode / type
// errors come back with structured apiMeta via the error surface
// established by the validation-plumbing plan.
func RegisterImport(srv *mcp.Server, client platform.Client, projectID string, engine *workflow.Engine, stateDir string, recipeProbe RecipeSessionProbe) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_import",
		Description: "REQUIRES active workflow (zerops_recipe for recipe authoring, or zerops_workflow bootstrap/develop). Import services from YAML into the project. The Zerops API validates fields, modes, types, and hostnames server-side and returns structured apiMeta on the error response when anything is wrong. Blocks until all processes complete; returns final statuses (FINISHED/FAILED).",
		InputSchema: importInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Import services from YAML",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ImportInput) (*mcp.CallToolResult, any, error) {
		if blocked := requireWorkflowContext(engine, stateDir, recipeProbe); blocked != nil {
			return blocked, nil, nil
		}
		if input.Override.Bool() {
			if blocked, gateErr := gateOverrideOnFailedHistory(ctx, client, projectID, input); gateErr != nil {
				return convertError(gateErr, WithRecoveryStatus()), nil, nil
			} else if blocked != nil {
				return blocked, nil, nil
			}
		}
		result, err := ops.Import(ctx, client, projectID, input.Content, input.FilePath, input.Override.Bool())
		if err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollImportProcesses(ctx, client, result, onProgress)

		return jsonResult(result), nil, nil
	})
}

// gateOverrideOnFailedHistory enforces plan v4 §3.2: when override=true
// would REPLACE a service that has failed appVersion history, refuse the
// first call with ErrDiagnosisRequired + a structured wouldDestroy payload.
// Returns (blocked, nil) when the gate fires (caller emits blocked verbatim);
// (nil, nil) when the gate passes (no failed targets OR matching ack);
// (nil, err) when target identification itself fails.
func gateOverrideOnFailedHistory(
	ctx context.Context,
	client platform.Client,
	projectID string,
	input ImportInput,
) (*mcp.CallToolResult, error) {
	overrideTargets, err := ops.IdentifyOverrideTargets(ctx, client, projectID, input.Content, input.FilePath)
	if err != nil {
		return nil, err
	}

	var failedTargets []string
	envVarsByService := make(map[string][]string)
	for _, hostname := range overrideTargets {
		failed, err := ops.LatestFailedAppVersionContext(ctx, client, nil, projectID, hostname)
		if err != nil {
			return nil, err
		}
		// Look up the live service once — used by both the destructive-risk
		// decision below and the env-var snapshot. Best-effort: a lookup error
		// leaves svc nil (the classified-failure gate still fires on `failed`).
		svc, _ := ops.LookupService(ctx, client, projectID, hostname)

		gated := failed != nil
		if !gated {
			// No CLASSIFIED failure context, but a service with any prior
			// deploy/build attempt (e.g. WAITING_TO_BUILD whose build process
			// failed at 0s — the recover-failed case) still holds buildFromGit
			// code/config worth preserving; override would silently wipe it.
			// LatestFailedAppVersionContext misses this because WAITING_TO_BUILD
			// has no FailurePhaseFromStatus mapping (Wave-1 gate-bypass that let
			// the override wipe the source under diagnosis). Uses the SAME
			// HasPriorDeployAttempt signal the recovery hint keys on, so the
			// read-first recovery and the destruct gate can't drift.
			//
			// Skip the backstop ONLY when we can POSITIVELY confirm the service
			// is currently healthy (RUNNING/ACTIVE) — a legit reconfigure-
			// override. A non-running status OR an unknown status (svc==nil
			// because lookup failed) falls through to the history check rather
			// than failing OPEN on a destructive op (Codex review: a lookup
			// error must not silently bypass the gate).
			healthy := svc != nil &&
				(svc.Status == platform.ServiceStatusRunning || svc.Status == platform.ServiceStatusActive)
			if !healthy {
				prior, priorErr := ops.HasPriorDeployAttempt(ctx, client, projectID, hostname)
				if priorErr != nil {
					return nil, priorErr
				}
				gated = prior
			}
		}
		if !gated {
			continue
		}
		failedTargets = append(failedTargets, hostname)
		// Snapshot the live env-var keys so wouldDestroy.envVars reflects
		// what override would actually erase. Best-effort: a lookup or
		// fetch failure leaves the key list empty for this host (the
		// gate still fires on the failed history alone). Keys only —
		// values stay on the platform.
		if svc == nil {
			continue
		}
		envs, fetchErr := ops.FetchServiceEnv(ctx, client, svc.ID)
		if fetchErr != nil {
			continue
		}
		keys := make([]string, 0, len(envs))
		for _, e := range envs {
			keys = append(keys, e.Key)
		}
		envVarsByService[hostname] = keys
	}

	if len(failedTargets) == 0 {
		// Gate bypassed: all override targets are healthy or healthy-after-
		// success. The standard B10 warning in ops.Import still fires.
		return nil, nil //nolint:nilnil // gate-passed sentinel: caller proceeds with import
	}

	expected := DiagnosedDestruction{
		Operation: importOverrideOperation,
		Targets:   failedTargets,
		Loss: DestructionLoss{
			ServiceStacks: failedTargets,
			EnvVars:       collectEnvVarKeys(envVarsByService, failedTargets),
		},
	}

	if validateErr := ValidateDestructiveAck(input.ConfirmDestructive, expected); validateErr != nil {
		return convertError(validateErr,
			WithWouldDestroy(&expected),
			WithRecovery(&RecoveryHint{
				Tool:   "zerops_logs",
				Action: "fetch",
				Args: map[string]string{
					"serviceHostname": failedTargets[0],
					"since":           "15m",
				},
			}),
		), nil
	}
	return nil, nil //nolint:nilnil // gate-passed sentinel: matching ack, proceed with import
}

// collectEnvVarKeys flattens the per-service env-var keys for the wire
// payload. Empty list when none of the targets had a usable env vars
// snapshot — the gate fires regardless; this just enriches the
// wouldDestroy payload when keys are available.
func collectEnvVarKeys(envVarsByService map[string][]string, targets []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, t := range targets {
		for _, k := range envVarsByService[t] {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

// pollImportProcesses polls each import process until completion, updating
// the result's process statuses and summary in-place.
func pollImportProcesses(
	ctx context.Context,
	client platform.Client,
	result *ops.ImportResult,
	onProgress ops.ProgressCallback,
) {
	finished := 0
	failed := 0
	for i := range result.Processes {
		proc := &result.Processes[i]
		if proc.ProcessID == "" {
			continue
		}
		finalProc, err := ops.PollProcess(ctx, client, proc.ProcessID, onProgress)
		if err != nil {
			// On timeout/error, keep original status.
			continue
		}
		proc.Status = finalProc.Status
		proc.FailReason = finalProc.FailReason
		switch finalProc.Status {
		case statusFinished:
			finished++
		case statusFailed:
			failed++
		}
	}

	total := len(result.Processes)
	if total == 0 {
		return
	}
	if failed > 0 {
		result.Summary = fmt.Sprintf("%d/%d processes completed, %d failed", finished, total, failed)
		result.NextActions = nextActionImportPartial
	} else {
		result.Summary = fmt.Sprintf("All %d processes completed successfully", total)
		result.NextActions = nextActionImportSuccess
	}
}
