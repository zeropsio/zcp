package tools

// StaleMetaSetupResponse is the canonical wire shape returned when a
// pre-deploy validator (platform `PostServiceStackZeropsYamlValidation`
// returning `zeropsYamlSetupNotFound`, or a local sanity check)
// detects that ServiceMeta.PrimarySetupName / StageSetupName references
// a setup-block name no longer present in the live zerops.yaml.
//
// Three recovery options are ALWAYS present per plan §staleMetaSetup —
// the response shape MUST NOT be conditional on which recovery the
// caller thinks is most appropriate. The agent picks one based on its
// understanding of the user's intent.
type StaleMetaSetupResponse struct {
	Status         string                   `json:"status"`                   // always "blocked"
	Reason         string                   `json:"reason"`                   // always "staleMetaSetup"
	Service        string                   `json:"service"`                  // hostname whose meta is stale
	MetaSetup      string                   `json:"metaSetup"`                // value currently in meta
	LiveYamlSetups []string                 `json:"liveYamlSetups,omitempty"` // setup names present in current yaml
	Recovery       StaleMetaSetupRecoveries `json:"recovery"`                 // three-option choice
}

// StaleMetaSetupRecoveries groups the three deterministic recovery
// options. All three populated on every emit — the agent picks based
// on user intent. No conditional shapes allowed.
type StaleMetaSetupRecoveries struct {
	Options []StaleMetaSetupRecoveryOption `json:"options"`
}

// StaleMetaSetupRecoveryOption is one of the three concrete actions
// the agent can take. Tool / Action / Args are populated when the
// option corresponds to a ZCP MCP call; Description-only options
// (e.g. "edit zerops.yaml manually") leave Tool empty so wire-level
// schema parsers don't trip on partial recovery shapes.
type StaleMetaSetupRecoveryOption struct {
	Label       string         `json:"label"`
	Description string         `json:"description,omitempty"`
	Tool        string         `json:"tool,omitempty"`
	Action      string         `json:"action,omitempty"`
	Args        map[string]any `json:"args,omitempty"`
}

// buildStaleMetaSetupResponse projects the (hostname, metaSetup,
// liveYamlSetups) tuple into the wire shape. The three recovery
// options are deterministic — they don't vary based on which
// liveYamlSetups exist. The agent's job is to pick; the wire shape's
// job is to enumerate.
//
// suggestedNewSetup is the value the helper recommends in the
// set-default-setup args payload — caller passes the most-plausible
// candidate from liveYamlSetups (e.g. first entry, or the entry whose
// suffix-stripped name matches the old metaSetup). When empty, the
// payload uses a placeholder so the agent fills it in.
func buildStaleMetaSetupResponse(hostname, metaSetup string, liveYamlSetups []string, suggestedNewSetup string) StaleMetaSetupResponse {
	if suggestedNewSetup == "" {
		suggestedNewSetup = "<choose-from-liveYamlSetups>"
	}
	return StaleMetaSetupResponse{
		Status:         "blocked",
		Reason:         "staleMetaSetup",
		Service:        hostname,
		MetaSetup:      metaSetup,
		LiveYamlSetups: liveYamlSetups,
		Recovery: StaleMetaSetupRecoveries{
			Options: []StaleMetaSetupRecoveryOption{
				{
					Label:       "Restore yaml block name to match meta",
					Description: "Edit zerops.yaml to use `setup: " + metaSetup + "` so live yaml matches the recorded meta — useful when the yaml was renamed accidentally and the meta is the canonical record.",
				},
				{
					Label:  "Update meta to match yaml (permanent)",
					Tool:   "zerops_workflow",
					Action: "set-default-setup",
					Args: map[string]any{
						"targetService": hostname,
						"setup":         suggestedNewSetup,
					},
				},
				{
					Label:  "One-shot deploy with override",
					Tool:   "zerops_deploy",
					Action: "",
					Args: map[string]any{
						"targetService": hostname,
						"setup":         suggestedNewSetup,
					},
				},
			},
		},
	}
}
