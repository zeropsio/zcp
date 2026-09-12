// Package farm generates the disposable run-project import YAML the farm
// controller feeds to CreateAndImportProject/ImportServiceStack
// (docs/spec-eval-farm.md §2, FM-10). A run project is created in two
// steps: ProjectImportYAML creates an empty project shell (no services —
// the REST import route never injects a first-class ZCP_API_KEY the way
// the GUI's route does), then, once the controller has minted a
// project-scoped run token, ServiceImportYAML imports the single zcp@1
// service named "zcp" carrying the run descriptor and credentials —
// including that minted token — as sensitive service envs, plus an inline
// zeropsYaml boot sequence that fetches and starts the wrapper. Nothing
// else exists in the project at creation time.
package farm

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// Sink carries the bucket coordinates the run's wrapper reads to download
// the evaluator/candidate/scenario tree and upload its bundle (§1.1, §2.2).
type Sink struct {
	URL    string
	Bucket string
	Key    string
	Secret string
}

// RunDescriptor is the typed input to ProjectImportYAML/ServiceImportYAML:
// everything a run project needs to self-drive (FM-10, FM-12). It has no
// field for the account-wide API key the controller uses to create/delete
// projects — that key never enters a run project (FM-15).
type RunDescriptor struct {
	BatchID         string
	RunID           string
	ScenarioID      string
	EvaluatorSHA256 string
	CandidateSHA256 string
	// ScenariosDigest is the scenario tree digest the wrapper downloads
	// (scenarios/<digest>/, §1.1, FM-1), emitted as ZCP_FARM_SCENARIOS_DIGEST
	// (FM-12's sixth row).
	ScenariosDigest string
	// WrapperSHA256 is the pinned wrapper script's SHA-256 (R5, LAND
	// review): the run project's init line fetches
	// farm/wrapper/<sha>.sh (§1.1) and sha256sum-verifies it against this
	// value before chmod+exec — the same content-addressed-and-verified
	// discipline as the evaluator/candidate binaries, closing the
	// unpinned-wrapper code-execution hole every run's write-capable
	// bucket key otherwise leaves open. Emitted as ZCP_FARM_WRAPPER_SHA
	// (FM-12); required.
	WrapperSHA256 string
	Sink          Sink
	// OAuthToken is the run's sole model-request credential
	// (CLAUDE_CODE_OAUTH_TOKEN) — the agent credential is OAuth-only, no
	// api-key mode and no fallback (§2.4/FM-16, spec commit 79ced2cc);
	// required.
	OAuthToken string
	// LaunchKey, when non-empty, is the per-run ZCP_E2E_LAUNCH_KEY minted by
	// the controller for launch scenarios only (§2.4).
	LaunchKey string
	// RunToken is the project-scoped ZCP_API_KEY the controller mints
	// (platform.MintProjectScopedToken) after the project shell exists —
	// required at ServiceImportYAML time only, since the project-creation
	// step has no project id yet to scope a token to.
	RunToken string
}

// ProjectName derives the run project's name from RunID — never passed in
// separately (§2.1 "project zcp-farm-<runId>").
func (d RunDescriptor) ProjectName() string { return ProjectPrefix + d.RunID }

// serviceHostname is fixed: hostnames may not contain hyphens, so
// "zcp-farm-<runId>" cannot be the service hostname (verified live).
const serviceHostname = "zcp"

// validateCommon checks the fields both YAML halves require.
func (d RunDescriptor) validateCommon() error {
	if d.RunID == "" {
		return fmt.Errorf("run descriptor: RunID required")
	}
	return nil
}

// validateService additionally requires the fields only ServiceImportYAML
// needs — OAuthToken (§2.4/FM-16) and RunToken, which only exists once the
// project shell has been created and the controller has minted a
// project-scoped token for it.
func (d RunDescriptor) validateService() error {
	if err := d.validateCommon(); err != nil {
		return err
	}
	if d.OAuthToken == "" {
		return fmt.Errorf("run descriptor: OAuthToken required (§2.4 — CLAUDE_CODE_OAUTH_TOKEN is the run's sole credential)")
	}
	if d.RunToken == "" {
		return fmt.Errorf("run descriptor: RunToken required (the project-scoped ZCP_API_KEY minted after the project shell exists)")
	}
	if d.WrapperSHA256 == "" {
		return fmt.Errorf("run descriptor: WrapperSHA256 required (R5 — the init line verifies the wrapper against this digest before executing it)")
	}
	return nil
}

// envKV is one ordered envSecrets entry.
type envKV struct {
	Key   string
	Value string
}

// projectTemplateData is what projectImportYAMLTemplate renders.
type projectTemplateData struct {
	ProjectName string
}

// serviceTemplateData is what serviceImportYAMLTemplate renders.
// EnvSecretsYAML is pre-rendered in Go (not a template loop) so the byte
// layout is exactly what buildEnvSecretsYAML produces — no template
// whitespace-trim ambiguity to reason about when authoring the golden
// files by hand.
type serviceTemplateData struct {
	Hostname       string
	EnvSecretsYAML string
}

// buildEnvSecretsYAML renders each entry as "      KEY: "VALUE"\n", in
// order, with no trailing blank line (the caller's template supplies the
// newline that follows the block).
func buildEnvSecretsYAML(entries []envKV) string {
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("      ")
		b.WriteString(e.Key)
		b.WriteString(": ")
		b.WriteString(yamlDQ(e.Value))
	}
	return b.String()
}

// ProjectImportYAML renders the run project's shell-creation import YAML
// for d — project settings only, no services (CreateAndImportProject,
// §2.1 step 1). Output is byte-stable for a given descriptor (golden-file
// tested).
func ProjectImportYAML(d RunDescriptor) ([]byte, error) {
	if err := d.validateCommon(); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := projectImportYAMLTemplate.Execute(&buf, projectTemplateData{ProjectName: d.ProjectName()}); err != nil {
		return nil, fmt.Errorf("render project import yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// ServiceImportYAML renders the run project's service-import YAML for d —
// the single zcp@1 service carrying the run descriptor, credentials
// (including the minted RunToken as ZCP_API_KEY), and boot sequence
// (ImportServiceStack, §2.1 step 3). Output is byte-stable for a given
// descriptor (golden-file tested) — the same descriptor always renders the
// same bytes, in the fixed envSecrets order FM-12 lists, with LaunchKey
// appended only when non-empty.
func ServiceImportYAML(d RunDescriptor) ([]byte, error) {
	if err := d.validateService(); err != nil {
		return nil, err
	}

	envSecrets := []envKV{
		{"ZCP_API_KEY", d.RunToken},
		{"ZCP_VSCODE", "true"},
		{"ZCP_FARM_BATCH", d.BatchID},
		{"ZCP_FARM_RUN", d.RunID},
		{"ZCP_FARM_SCENARIO", d.ScenarioID},
		{"ZCP_FARM_EVALUATOR_SHA", d.EvaluatorSHA256},
		{"ZCP_FARM_CANDIDATE_SHA", d.CandidateSHA256},
		{"ZCP_FARM_SCENARIOS_DIGEST", d.ScenariosDigest},
		{"ZCP_FARM_WRAPPER_SHA", d.WrapperSHA256},
		{"ZCP_FARM_S3_URL", d.Sink.URL},
		{"ZCP_FARM_S3_BUCKET", d.Sink.Bucket},
		{"ZCP_FARM_S3_KEY", d.Sink.Key},
		{"ZCP_FARM_S3_SECRET", d.Sink.Secret},
		{"CLAUDE_CODE_OAUTH_TOKEN", d.OAuthToken},
	}
	if d.LaunchKey != "" {
		envSecrets = append(envSecrets, envKV{"ZCP_E2E_LAUNCH_KEY", d.LaunchKey})
	}

	data := serviceTemplateData{
		Hostname:       serviceHostname,
		EnvSecretsYAML: buildEnvSecretsYAML(envSecrets),
	}

	var buf bytes.Buffer
	if err := serviceImportYAMLTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render service import yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// yamlDQ renders s as a double-quoted YAML scalar, escaping backslash and
// double-quote. Values here are opaque tokens/ids/shas, never multiline.
func yamlDQ(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

var projectImportYAMLTemplate = template.Must(template.New("projectImportYAML").Parse(`project:
  name: {{.ProjectName}}
  tags: [zcp-farm, disposable]
  sshIsolation: "vpn project"
services: []
`))

var serviceImportYAMLTemplate = template.Must(template.New("serviceImportYAML").Parse(`services:
  - hostname: {{.Hostname}}
    type: zcp@1
    maxContainers: 1
    verticalAutoscaling: { minRam: 2 }
    envSecrets:
{{.EnvSecretsYAML}}
    zeropsYaml:
      zerops:
        - setup: zcp
          run:
            base: zcp@1
            initCommands:
              - curl -sSfL https://zerops.io/zcp/install.sh | sudo sh
              - zcp init
              - sudo -E zcp init nginx
              - |
                set -e
                curl -sSf --aws-sigv4 aws:amz:us-east-1:s3 --user "$(printenv ZCP_FARM_S3_KEY):$(printenv ZCP_FARM_S3_SECRET)" -o /tmp/farm-wrapper.sh "$(printenv ZCP_FARM_S3_URL)/$(printenv ZCP_FARM_S3_BUCKET)/farm/wrapper/$(printenv ZCP_FARM_WRAPPER_SHA).sh"
                echo "$(printenv ZCP_FARM_WRAPPER_SHA)  /tmp/farm-wrapper.sh" | sha256sum -c -
                chmod +x /tmp/farm-wrapper.sh
                nohup /tmp/farm-wrapper.sh >/tmp/farm-wrapper.log 2>&1 &
            ports: [ { port: 8080, httpSupport: true } ]
            startCommands:
              - { command: zcp service start nginx, name: nginx }
              - { command: zcp service start vscode, name: vscode }
`))
