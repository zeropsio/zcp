package ops

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// preprocessorHeader is prepended verbatim as line 1 of import.yaml when
// any value contains a `<@...>` directive. Platform preprocessor skips
// expansion if this header is missing or not on line 1.
const preprocessorHeader = "#zeropsPreprocessor=on\n"

// importModeNonHA is the platform scaling mode for single-runtime
// bundle entries. Platform schema enforces `HA` / `NON_HA` only; the
// topology dev/stage/simple/local-only distinction is destination-
// project-bootstrap concern, not import.yaml content.
const importModeNonHA = "NON_HA"

const autoSecretPreprocessor = "<@generateRandomString(<32>)>"

// ExportBundle is the generator output for the export-buildFromGit
// workflow. Self-referential single-repo shape per plan §3.1:
// zerops-project-import.yaml + zerops.yaml + code, all checked into
// ONE git repo. ImportYAML and ZeropsYAML are file contents the agent
// writes at repo root before publishing via git-push (Phase C).
//
// Atom corpus references-fields entry points:
//   - ops.ExportBundle.ImportYAML
//   - ops.ExportBundle.ZeropsYAML
//   - ops.ExportBundle.Warnings
//
// pinned by `internal/workflow/atom_reference_field_integrity_test.go`.
type ExportBundle struct {
	// ImportYAML is the rendered zerops-project-import.yaml body.
	ImportYAML string
	// ZeropsYAML is the per-bundle zerops.yaml body — either copied from
	// the live container's /var/www/zerops.yaml ("live") or scaffolded
	// when the source was missing ("scaffolded", deferred to a future
	// phase per plan §10 anti-pattern).
	ZeropsYAML string
	// ZeropsYAMLSource records provenance: "live" | "scaffolded".
	ZeropsYAMLSource string
	// RepoURL is the resolved buildFromGit URL (live `git remote
	// get-url origin` from the chosen container, refreshed in Phase 6).
	RepoURL string
	// Variant records which half of a pair the bundle packages
	// (`dev` or `stage` for ModeStandard / ModeLocalStage; unset for
	// single-half source modes).
	Variant topology.ExportVariant
	// TargetHostname is the runtime hostname the bundle re-imports as.
	TargetHostname string
	// SetupName is the matched `setup:` block in the bundled zerops.yaml.
	SetupName string
	// Classifications echoes the per-env bucket map the agent supplied
	// — preserved on the bundle for downstream review tables.
	Classifications map[string]topology.SecretClassification
	// Warnings carries non-fatal hints (privacy-sensitive plain config,
	// empty external secrets that need user review, unclassified envs,
	// M2 indirect references, sentinel-pattern detections).
	Warnings []string
	// Errors carries blocking schema-validation failures from Phase 5.
	// When non-empty, the bundle MUST NOT be published as-is — the
	// generated import.yaml or zerops.yaml violates the published JSON
	// schemas and re-import would fail. The Phase 3 handler treats
	// any non-empty Errors as a "validation-failed" response status.
	Errors []schema.ValidationError
}

// BundleInputs captures the live state Phase A probes before BuildBundle
// composes the bundle. The handler resolves these via Discover + SSH +
// `git remote -v`; BuildBundle is pure composition over them.
type BundleInputs struct {
	// ProjectName is the source project's name — copied verbatim into
	// `project.name` so re-imports describe their lineage.
	ProjectName string
	// TargetHostname is the chosen runtime hostname (dev or stage half).
	TargetHostname string
	// SourceMode is the topology.Mode of the chosen runtime hostname.
	// Drives the import.yaml `mode:` mapping per §3.3 (β).
	SourceMode topology.Mode
	// ServiceType is the runtime's platform type tag, e.g. "nodejs@22".
	ServiceType string
	// SubdomainEnabled mirrors Discover's per-service `subdomainEnabled`.
	// When true, the import.yaml runtime entry carries
	// `enableSubdomainAccess: true` (lossy in platform export — must be
	// re-asserted from Discover per the legacy atom's L97-98 note).
	SubdomainEnabled bool
	// SetupName names the `setup:` block in the bundled zerops.yaml the
	// runtime should resolve at build time.
	SetupName string
	// ZeropsYAMLBody is the verbatim contents of /var/www/zerops.yaml
	// from the chosen container (Phase A SSH-read). BuildBundle verifies
	// the named setup exists; it does NOT rewrite the body.
	ZeropsYAMLBody string
	// RepoURL is the buildFromGit target — live `git remote get-url
	// origin` resolved in Phase A. Empty value is rejected by
	// BuildBundle (chain to setup-git-push at the handler level).
	RepoURL string
	// ProjectEnvs is the project-level envVariables snapshot from the
	// platform API (`client.GetProjectEnv`). Each entry is bucketed via
	// the classifications map.
	ProjectEnvs []ProjectEnvVar
	// ManagedServices lists managed deps the bundle must re-import so
	// `${db_*}` / `${redis_*}` references in zerops.yaml resolve in the
	// destination project. Empty slice = no managed services bundled.
	ManagedServices []ManagedServiceEntry
}

// ProjectEnvVar is a single project-level env var awaiting classification.
type ProjectEnvVar struct {
	Key   string
	Value string
}

// ManagedServiceEntry describes a managed dep to re-import alongside
// the runtime. Hostname / type / mode mirror Discover output; envs and
// envSecrets are intentionally absent — the platform regenerates managed
// credentials on import (existing atom L161 contract).
type ManagedServiceEntry struct {
	Hostname string
	Type     string
	Mode     string // "HA" / "NON_HA" / "" (object-storage and similar)
}

// BuildBundle composes the export bundle from pre-probed inputs +
// agent-resolved variant + per-env classification map. Pure composition
// — no I/O. The handler is responsible for SSH + Discover + git-remote
// reads upstream (Phase A) and for chaining to setup-git-push when
// inputs.RepoURL is empty.
func BuildBundle(
	inputs BundleInputs,
	variant topology.ExportVariant,
	classifications map[string]topology.SecretClassification,
) (*ExportBundle, error) {
	if inputs.TargetHostname == "" {
		return nil, fmt.Errorf("export bundle: target hostname required")
	}
	if inputs.RepoURL == "" {
		return nil, fmt.Errorf("export bundle: repo URL required (chain to setup-git-push)")
	}
	if inputs.SetupName == "" {
		return nil, fmt.Errorf("export bundle: zerops.yaml setup name required")
	}
	if inputs.ServiceType == "" {
		return nil, fmt.Errorf("export bundle: runtime service type required")
	}
	if classifications == nil {
		classifications = map[string]topology.SecretClassification{}
	}

	if err := verifyZeropsYAMLSetup(inputs.ZeropsYAMLBody, inputs.SetupName); err != nil {
		return nil, fmt.Errorf("verify zerops.yaml: %w", err)
	}

	importYAML, warnings, err := composeImportYAML(inputs, variant, classifications)
	if err != nil {
		return nil, fmt.Errorf("compose import.yaml: %w", err)
	}

	// Phase 5: schema-validate both YAMLs against the published JSON
	// schemas (embedded testdata copies — refresh cadence is a separate
	// concern). Errors are blocking; warnings stay non-fatal. The
	// validators are deterministic so duplicate-call dedupe in tests
	// holds across the validation step.
	importErrors := schema.ValidateImportYAML(importYAML)
	zeropsErrors := schema.ValidateZeropsYAML(inputs.ZeropsYAMLBody, inputs.SetupName)
	validationErrors := make([]schema.ValidationError, 0, len(importErrors)+len(zeropsErrors))
	validationErrors = append(validationErrors, importErrors...)
	validationErrors = append(validationErrors, zeropsErrors...)

	return &ExportBundle{
		ImportYAML:       importYAML,
		ZeropsYAML:       inputs.ZeropsYAMLBody,
		ZeropsYAMLSource: "live",
		RepoURL:          inputs.RepoURL,
		Variant:          variant,
		TargetHostname:   inputs.TargetHostname,
		SetupName:        inputs.SetupName,
		Classifications:  classifications,
		Warnings:         warnings,
		Errors:           validationErrors,
	}, nil
}

// verifyZeropsYAMLSetup confirms the body is a parseable zerops.yaml
// with a `zerops:` list containing an entry whose `setup:` matches the
// requested name. Empty bodies, parse failures, and missing setups are
// all errors — the handler chains to scaffold-zerops-yaml when the
// body is empty (per plan Q5 default refusal contract).
func verifyZeropsYAMLSetup(body, setupName string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("zerops.yaml body is empty (chain to scaffold-zerops-yaml)")
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return fmt.Errorf("parse zerops.yaml: %w", err)
	}
	setups, ok := doc["zerops"].([]any)
	if !ok {
		return fmt.Errorf("zerops.yaml missing top-level 'zerops:' list")
	}
	for _, item := range setups {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["setup"].(string); ok && name == setupName {
			return nil
		}
	}
	return fmt.Errorf("zerops.yaml does not contain setup %q (chain to scaffold-zerops-yaml or correct the setup name)", setupName)
}

// composeImportYAML produces the zerops-project-import.yaml body. Shape
// per plan §3.1: project block + ONE runtime service entry with
// buildFromGit + zeropsSetup + (optional) enableSubdomainAccess, plus
// any managed services the agent included so `${db_*}` / `${redis_*}`
// references in zerops.yaml resolve in the destination project.
//
// Surfaces two classes of warnings:
//   - per-env hints from composeProjectEnvVariables (unset / unknown
//     bucket / sentinel external secret).
//   - M2 indirect-reference warnings from detectIndirectInfraReferences
//     when the bundled zerops.yaml's run.envVariables references a
//     project-level env about to be dropped as Infrastructure.
func composeImportYAML(
	inputs BundleInputs,
	variant topology.ExportVariant,
	classifications map[string]topology.SecretClassification,
) (string, []string, error) {
	projectEnvs, warnings := composeProjectEnvVariables(inputs.ProjectEnvs, classifications)

	zeropsRefs := extractZeropsYAMLRunEnvRefs(inputs.ZeropsYAMLBody)
	warnings = append(warnings, detectIndirectInfraReferences(inputs.ProjectEnvs, classifications, zeropsRefs)...)

	runtimeEntry := map[string]any{
		"hostname":     inputs.TargetHostname,
		"type":         inputs.ServiceType,
		"mode":         runtimeImportMode(inputs.SourceMode),
		"buildFromGit": inputs.RepoURL,
		"zeropsSetup":  inputs.SetupName,
	}
	if inputs.SubdomainEnabled {
		runtimeEntry["enableSubdomainAccess"] = true
	}

	services := make([]any, 0, 1+len(inputs.ManagedServices))
	services = append(services, runtimeEntry)
	for _, m := range inputs.ManagedServices {
		entry := map[string]any{
			"hostname": m.Hostname,
			"type":     m.Type,
			"priority": 10,
		}
		if m.Mode != "" {
			entry["mode"] = m.Mode
		}
		services = append(services, entry)
	}

	project := map[string]any{
		"name": inputs.ProjectName,
	}
	if len(projectEnvs) > 0 {
		project["envVariables"] = projectEnvs
	}

	doc := map[string]any{
		"project":  project,
		"services": services,
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", nil, fmt.Errorf("marshal: %w", err)
	}
	body := string(out)
	body = addPreprocessorHeader(body, projectEnvs)

	_ = variant // recorded on bundle, mode mapping derives from SourceMode
	return body, warnings, nil
}

// composeProjectEnvVariables applies the four-category classification
// per plan §3.4 to the project envVariables snapshot. Returns the
// rendered map keyed by env name, plus per-env warnings for buckets
// that need explicit user review (empty external secrets, unclassified
// envs, unknown bucket values).
func composeProjectEnvVariables(
	envs []ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
) (map[string]string, []string) {
	out := map[string]string{}
	var warnings []string

	for _, env := range envs {
		bucket := classifications[env.Key]
		switch bucket {
		case topology.SecretClassInfrastructure:
			// Drop — re-imported managed services emit fresh ${...} values.
			continue
		case topology.SecretClassAutoSecret:
			out[env.Key] = autoSecretPreprocessor
		case topology.SecretClassExternalSecret:
			if env.Value == "" {
				out[env.Key] = ""
				warnings = append(warnings, fmt.Sprintf(
					"env %q: empty external secret — review before publish (plan §3.4 M4)", env.Key))
			} else {
				out[env.Key] = `<@pickRandom(["REPLACE_ME"])>`
				if isLikelySentinel(env.Value) {
					warnings = append(warnings, fmt.Sprintf(
						"env %q: external secret value %q matches a known sentinel/test pattern — verify classification (PlainConfig may be more appropriate; plan §3.4 M4)",
						env.Key, env.Value))
				}
			}
		case topology.SecretClassPlainConfig:
			out[env.Key] = env.Value
		case topology.SecretClassUnset:
			out[env.Key] = env.Value
			warnings = append(warnings, fmt.Sprintf(
				"env %q: not classified — emitted as plain-config; classify before publish (plan §3.4)", env.Key))
		default:
			out[env.Key] = env.Value
			warnings = append(warnings, fmt.Sprintf(
				"env %q: unknown classification %q — emitted as plain-config", env.Key, bucket))
		}
	}

	return out, warnings
}

// runtimeImportMode returns the platform scaling-mode (`HA` / `NON_HA`)
// for the bundle's runtime service entry. Platform schema enforces this
// as the closed enum; the topology-level dev / stage / simple / local-only
// shape lives in the destination project's bootstrap meta after re-import,
// not in import.yaml.
//
// Single-runtime bundles always emit `NON_HA` — the bundle composes one
// runtime entry without scaling fields, so HA (multi-container) would
// require additional `verticalAutoscaling` / `minContainers` declarations
// the agent has not committed to yet. NON_HA is the safe re-import shape;
// the destination project's owner can switch to HA in dashboard later.
func runtimeImportMode(source topology.Mode) string {
	_ = source // topology Mode preserved on bundle.Variant for metadata; doesn't drive the platform mode
	return importModeNonHA
}

// addPreprocessorHeader prepends `#zeropsPreprocessor=on\n` to body
// when any project envVariable carries a `<@...>` directive (per plan
// §3.4 emit shapes for AutoSecret + ExternalSecret). Header MUST be
// line 1 or the platform preprocessor skips expansion on import.
func addPreprocessorHeader(body string, projectEnvs map[string]string) string {
	for _, v := range projectEnvs {
		if strings.Contains(v, "<@") && strings.Contains(v, ")>") {
			return preprocessorHeader + body
		}
	}
	return body
}
