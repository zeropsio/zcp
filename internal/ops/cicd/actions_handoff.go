package cicd

import (
	"fmt"
	"strings"
)

// CICDTarget identifies whether the handoff targets the stage or
// production CI/CD path. Drives workflow filename, trigger shape,
// secret name, and where the token value comes from.
type CICDTarget int

const (
	// TargetStage composes the stage cicd handoff — triggered by
	// `action=git-push-setup` confirm (§2.5). Workflow file:
	// .github/workflows/zerops-stage.yml. Trigger: push to main.
	// Secret name: ZEROPS_TOKEN_STAGE. Token value sourced from the
	// runtime env (container: $ZCP_API_KEY; local: jq from .mcp.json).
	TargetStage CICDTarget = iota
	// TargetProd composes the production cicd handoff — triggered by
	// the launch terminal phase (§4.1) when the user picks Actions
	// over the webhook alternative. Workflow file:
	// .github/workflows/zerops-prod.yml. Trigger: push tag v*.*.*.
	// Secret name: ZEROPS_TOKEN_PROD. Token value pasted by user
	// (fresh project-scoped token generated on the prod project's
	// dashboard — distinct from the source project's ZCP_API_KEY).
	TargetProd
)

// EnvMode identifies whether ZCP runs in a Zerops container or on the
// user's local machine. Drives the gh-secret-set command shape — the
// stage path reads from $ZCP_API_KEY in container mode and from
// .mcp.json via jq in local mode. Production path always uses a
// user-pasted token regardless of env.
type EnvMode int

const (
	// EnvContainer means ZCP runs in a Zerops zcp@1 container with
	// ZCP_API_KEY auto-injected as a project env var.
	EnvContainer EnvMode = iota
	// EnvLocal means ZCP runs on the user's machine; ZCP_API_KEY
	// lives in .mcp.json under the zcp server's env block.
	EnvLocal
)

// ActionsHandoffInput feeds ComposeActionsHandoff. All fields are
// required except the EnvMode default — zero value is EnvContainer
// which is the production default.
type ActionsHandoffInput struct {
	// Target selects stage vs prod composition.
	Target CICDTarget
	// OwnerRepo is the "owner/repo" string derived from the git
	// remote URL via ops.ParseGitRemoteOwnerRepo. Used in both the
	// workflow YAML (no — it's not used there) and the gh-secret-set
	// command (-R flag). Empty value yields a `<owner>/<repo>`
	// placeholder + a warning in the response.
	OwnerRepo string
	// ServiceID is the target service-stack ID — stage service for
	// TargetStage, prod runtime service for TargetProd. Spliced
	// literally into the workflow YAML's `zcli push --service-id`
	// argument (not stored as a secret — the ID is not sensitive
	// and inlining keeps the workflow self-contained).
	ServiceID string
	// SetupName names the zerops.yaml setup block the build resolves
	// at deploy time. Conventionally "stage" for both stage cicd
	// (pair-keyed) and prod cicd (reuses the source repo's stage
	// build recipe; see §4.2). Caller overrides for divergent prod
	// build via the launch input's ProdSetupNameOverride field.
	SetupName string
	// Env controls the token-source expression in the gh-secret-set
	// command. Only meaningful for TargetStage; TargetProd always
	// uses a user-pasted token regardless of env.
	Env EnvMode
}

// ActionsHandoffOutput is the composer result. Three surfaces:
//   - WorkflowYAML: the body to write at WorkflowFilePath in the
//     user's git repo.
//   - SecretSetCommand: stdin form `echo "$VALUE" | gh secret set
//     NAME -R OWNER/REPO` — never leaks the value into shell history.
//   - GhPatRecommendation: prose pointing the user at the
//     recommended PAT scopes (single-repo blast radius).
//
// Plus the file path, secret name, and a flat Instructions slice the
// caller can render verbatim.
type ActionsHandoffOutput struct {
	WorkflowFilePath    string
	WorkflowYAML        string
	SecretName          string
	SecretSetCommand    string
	GhPatRecommendation string
	Instructions        []string
}

// ComposeActionsHandoff produces the stage or production GitHub
// Actions handoff payload. Pure composition — no I/O, no platform
// calls. Caller (handler in internal/tools/) supplies inputs probed
// upstream (service ID resolved via ops.LookupService, owner/repo
// parsed from meta.RemoteURL, etc.).
func ComposeActionsHandoff(in ActionsHandoffInput) ActionsHandoffOutput {
	ownerRepo := in.OwnerRepo
	if ownerRepo == "" {
		ownerRepo = "<owner>/<repo>"
	}
	serviceID := in.ServiceID
	if serviceID == "" {
		serviceID = "<service-id>"
	}
	setupName := in.SetupName
	if setupName == "" {
		setupName = "stage"
	}

	switch in.Target {
	case TargetStage:
		return composeStageHandoff(ownerRepo, serviceID, setupName, in.Env)
	case TargetProd:
		return composeProdHandoff(ownerRepo, serviceID, setupName)
	default:
		return composeStageHandoff(ownerRepo, serviceID, setupName, in.Env)
	}
}

// composeStageHandoff builds the stage cicd payload. Secret name
// ZEROPS_TOKEN_STAGE is distinct from the production secret so both
// can coexist in the same repo without overwriting each other.
func composeStageHandoff(ownerRepo, serviceID, setupName string, env EnvMode) ActionsHandoffOutput {
	yaml := stageWorkflowYAML(serviceID, setupName)
	tokenExpr := tokenValueExprForStage(env)
	cmd := fmt.Sprintf(`echo %s | gh secret set ZEROPS_TOKEN_STAGE -R %s`, tokenExpr, ownerRepo)
	return ActionsHandoffOutput{
		WorkflowFilePath: ".github/workflows/zerops-stage.yml",
		WorkflowYAML:     yaml,
		SecretName:       "ZEROPS_TOKEN_STAGE",
		SecretSetCommand: cmd,
		GhPatRecommendation: "Default to a fine-grained GitHub PAT scoped ONLY to " + ownerRepo +
			" with `Contents: Read and write`, `Secrets: Read and write`, `Actions: Read and write` " +
			"(single-repo blast radius). The same PAT works for stage CI/CD setup AND production CI/CD setup later " +
			"— create it once with all three scopes to avoid regenerating.",
		Instructions: []string{
			"Write the workflow body to " + ".github/workflows/zerops-stage.yml" + " in your repo.",
			"Run the gh-secret-set command above (stdin form — value is never shell-echoed).",
			"Commit the workflow file. From then on every push to main triggers a stage deploy via `zcli push`.",
		},
	}
}

// composeProdHandoff builds the production cicd payload. Secret name
// ZEROPS_TOKEN_PROD is distinct from the stage secret. Token value is
// pasted by the user (a fresh project-scoped token generated on the
// new prod project's dashboard — distinct from the source project's
// ZCP_API_KEY).
func composeProdHandoff(ownerRepo, serviceID, setupName string) ActionsHandoffOutput {
	yaml := prodWorkflowYAML(serviceID, setupName)
	cmd := fmt.Sprintf(`echo "<paste-prod-token>" | gh secret set ZEROPS_TOKEN_PROD -R %s`, ownerRepo)
	return ActionsHandoffOutput{
		WorkflowFilePath: ".github/workflows/zerops-prod.yml",
		WorkflowYAML:     yaml,
		SecretName:       "ZEROPS_TOKEN_PROD",
		SecretSetCommand: cmd,
		GhPatRecommendation: "Production CI/CD reuses the same fine-grained GitHub PAT used for stage CI/CD " +
			"(scope: " + ownerRepo + " — Contents R+W, Secrets R+W, Actions R+W). " +
			"The token wired into ZEROPS_TOKEN_PROD is a SEPARATE credential — a fresh project-scoped Zerops " +
			"token generated on the new prod project's dashboard. Don't reuse the source project's ZCP_API_KEY.",
		Instructions: []string{
			"Generate a fresh project-scoped Zerops token on the new prod project's dashboard (Custom access, Full access).",
			"Substitute the placeholder `<paste-prod-token>` in the secret-set command above with that token value, then run it.",
			"Write the workflow body to " + ".github/workflows/zerops-prod.yml" + " in your repo.",
			"Commit the workflow file. Push a tag (`git tag v0.1.0 && git push --tags`) to trigger the first prod deploy.",
		},
	}
}

// tokenValueExprForStage returns the shell expression the gh-secret-set
// command pipes from stdin. Container mode reads from the in-env
// ZCP_API_KEY; local mode extracts via jq from .mcp.json.
func tokenValueExprForStage(env EnvMode) string {
	if env == EnvLocal {
		return `"$(jq -r '.mcpServers.zcp.env.ZCP_API_KEY' .mcp.json)"`
	}
	return `"$ZCP_API_KEY"`
}

// quoteShellLiteral wraps s in double quotes for safe shell-argument
// embedding. Kept inline so the package stays self-contained.
func quoteShellLiteral(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// _ keeps strings imported even if quoteShellLiteral isn't used by
// other call sites yet — Phase 6b may use it.
var _ = strings.HasPrefix
