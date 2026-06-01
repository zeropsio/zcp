package bundle

import (
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// ExportBundle is the generator output for the export-buildFromGit
// workflow. Self-referential single-repo shape per export plan §3.1:
// zerops-project-import.yaml + zerops.yaml + code, all checked into
// ONE git repo. ImportYAML and ZeropsYAML are file contents the agent
// writes at repo root before publishing via git-push.
//
// Atom corpus references-fields entry points:
//   - bundle.ExportBundle.ImportYAML
//   - bundle.ExportBundle.ZeropsYAML
//   - bundle.ExportBundle.Warnings
//
// Pinned by internal/workflow/atom_reference_field_integrity_test.go.
type ExportBundle struct {
	ImportYAML       string
	ZeropsYAML       string
	ZeropsYAMLSource string // "live" | "scaffolded"
	RepoURL          string
	TargetHostname   string
	SetupName        string
	Classifications  map[string]topology.SecretClassification
	Warnings         []string
	Errors           []schema.ValidationError
}

// LaunchBundle is the composer output for launch-production. Same
// general shape as ExportBundle but specialized for the launch flow:
// prod-tier transforms applied (HA promotion, DEDICATED cpuMode,
// minContainers), source snapshot hashes recorded for immutability
// guard.
//
// SourceSnapshot is the immutability guard substrate: BuildLaunch
// records a deterministic digest of source state at compose-time; the
// workflow handler re-computes before mutation and rejects on drift
// (P-LP-3).
type LaunchBundle struct {
	ImportYAML        string
	TargetProjectName string
	SourceProjectID   string
	SourceSnapshot    SourceSnapshot
	Classifications   map[string]topology.SecretClassification
	Warnings          []string
	Errors            []schema.ValidationError
}

// SourceSnapshot is a deterministic digest of source state at the
// moment BuildLaunch composed the bundle. Used by the workflow
// handler's source-immutability guard: re-compute these hashes before
// invoking ProjectAdminClient.CreateAndImportProject; if any field
// changed, publish is rejected with a `source-drift` blocker.
type SourceSnapshot struct {
	// GitCommitSHA is the source repo's HEAD commit when the bundle
	// was composed.
	GitCommitSHA string `json:"gitCommitSha,omitempty"`
	// ZeropsYAMLSHA256 is sha256 of the source zerops.yaml body
	// (including the appended setup: prod block as committed).
	ZeropsYAMLSHA256 string `json:"zeropsYamlSha256,omitempty"`
	// ProjectEnvsDigest is sha256 over sorted (key=value) lines of
	// the source project envs. Captures the env shape that
	// classification ran against.
	ProjectEnvsDigest string `json:"projectEnvsDigest,omitempty"`
	// ServiceListDigest is sha256 over sorted "hostname:type" lines
	// of the source service list. Captures the topology shape.
	ServiceListDigest string `json:"serviceListDigest,omitempty"`
}
