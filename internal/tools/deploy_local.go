package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DeployLocalInput is the input type for zerops_deploy in local mode.
// No sourceService — code lives locally, not on a remote service.
//
// IncludeGit is FlexBool so stringified boolean forms go through —
// same reasoning as DiscoverInput/EnvInput.
type DeployLocalInput struct {
	TargetService string   `json:"targetService"`
	Setup         string   `json:"setup,omitempty"`
	WorkingDir    string   `json:"workingDir,omitempty"`
	IncludeGit    FlexBool `json:"includeGit,omitempty"`
}

func deployLocalInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"targetService": {Type: "string", Description: "Hostname of the Zerops service to deploy to."},
		"setup":         {Type: "string", Description: "zerops.yaml setup block name — matches a `setup:` key in the file's `zerops:` array. Setup names are user-defined identifiers; recipes conventionally use `dev`/`prod` (and `worker` for shared-codebase worker recipes that pack the host service's dev/prod plus the worker setup into one zerops.yaml). Required whenever zerops.yaml declares more than one setup — the tool cannot guess which block to build. Recipes always ship multiple setups, so `setup` is effectively required in recipe workflows: `targetService=apidev setup=dev`, `targetService=apistage setup=prod` (a cross-deploy from apidev→apistage uses `setup=prod` because `setup` names the zerops.yaml block, not the deploy source). Omit only when zerops.yaml has a single setup AND its name matches the target hostname (bootstrap workflows only)."},
		"workingDir":    {Type: "string", Description: "Local path to push from. Default: current directory."},
		"includeGit":    flexBoolSchema("Include .git directory in the push (-g flag)."),
	}, "targetService")
}

// RegisterDeployLocal registers the zerops_deploy tool for local mode.
// Uses zcli push instead of SSH to deploy code from the user's machine.
func RegisterDeployLocal(
	srv *mcp.Server,
	client platform.Client,
	projectID string,
	authInfo *auth.Info,
	logFetcher platform.LogFetcher,
	stateDir string,
	engine *workflow.Engine,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "zerops_deploy",
		Description: "Push local code to Zerops — blocks until build completes. " +
			"Requires zerops.yaml and zcli installed. " +
			"Set targetService to the Zerops service hostname.",
		InputSchema: deployLocalInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy code to a service",
			DestructiveHint: boolPtr(true),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DeployLocalInput) (*mcp.CallToolResult, any, error) {
		// Gate: target must be adopted by ZCP.
		if blocked := requireAdoption(stateDir, input.TargetService); blocked != nil {
			return blocked, nil, nil
		}

		// Pre-flight validation (harness). v8.85 — pre-flight echoes the
		// effective setup so zcli always invokes with --setup=<resolved>.
		resolvedSetup, pfResult, pfErr := deployPreFlight(ctx, client, projectID, stateDir, input.TargetService, input.Setup)
		if pfErr != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Pre-flight validation error: %v", pfErr),
				"Check zerops.yaml and service configuration")), nil, nil
		}
		if pfResult != nil && !pfResult.Passed {
			return jsonResult(pfResult), nil, nil
		}
		if resolvedSetup != "" {
			input.Setup = resolvedSetup
		}

		attempt := workflow.DeployAttempt{
			AttemptedAt: time.Now().UTC().Format(time.RFC3339),
			Setup:       input.Setup,
		}

		result, err := ops.DeployLocal(ctx, client, projectID, *authInfo,
			input.TargetService, input.Setup, input.WorkingDir, input.IncludeGit.Bool())
		if err != nil {
			attempt.Error = err.Error()
			_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)
			return convertError(err), nil, nil
		}

		onProgress := buildProgressCallback(ctx, req)
		pollDeployBuild(ctx, client, projectID, result, onProgress, logFetcher, nil)

		if result != nil && result.Status == statusDeployed {
			attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
		} else if result != nil {
			attempt.Error = fmt.Sprintf("deploy status %s", result.Status)
		}
		_ = workflow.RecordDeployAttempt(stateDir, input.TargetService, attempt)

		return jsonResult(result), nil, nil
	})
}
