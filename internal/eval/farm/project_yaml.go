// Package farm generates the disposable run-project import YAML the farm
// controller feeds to CreateAndImportProject (docs/spec-eval-farm.md §2,
// FM-10). A run project contains exactly one zcp@1 service named "zcp"
// carrying the run descriptor and credentials as sensitive service envs,
// plus an inline zeropsYaml boot sequence that fetches and starts the
// wrapper. Nothing else exists in the project at creation time.
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

// RunDescriptor is the typed input to ImportYAML: everything a run project
// needs to self-drive (FM-10, FM-12). It has no field for the account-wide
// API key the controller uses to create/delete projects — that key never
// enters a run project (FM-15).
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
	Sink            Sink
	// OAuthToken is the run's sole model-request credential
	// (CLAUDE_CODE_OAUTH_TOKEN) — the agent credential is OAuth-only, no
	// api-key mode and no fallback (§2.4/FM-16, spec commit 79ced2cc);
	// required.
	OAuthToken string
	// LaunchKey, when non-empty, is the per-run ZCP_E2E_LAUNCH_KEY minted by
	// the controller for launch scenarios only (§2.4).
	LaunchKey string
}

// ProjectName derives the run project's name from RunID — never passed in
// separately (§2.1 "project zcp-farm-<runId>").
func (d RunDescriptor) ProjectName() string { return "zcp-farm-" + d.RunID }

// serviceHostname is fixed: hostnames may not contain hyphens, so
// "zcp-farm-<runId>" cannot be the service hostname (verified live).
const serviceHostname = "zcp"

func (d RunDescriptor) validate() error {
	if d.RunID == "" {
		return fmt.Errorf("run descriptor: RunID required")
	}
	if d.OAuthToken == "" {
		return fmt.Errorf("run descriptor: OAuthToken required (§2.4 — CLAUDE_CODE_OAUTH_TOKEN is the run's sole credential)")
	}
	return nil
}

// envKV is one ordered envSecrets entry.
type envKV struct {
	Key   string
	Value string
}

// templateData is what importYAMLTemplate renders. EnvSecretsYAML is
// pre-rendered in Go (not a template loop) so the byte layout is exactly
// what buildEnvSecretsYAML produces — no template whitespace-trim ambiguity
// to reason about when authoring the golden files by hand.
type templateData struct {
	ProjectName    string
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

// ImportYAML renders the run project's import YAML for d. Output is
// byte-stable for a given descriptor (golden-file tested) — the same
// descriptor always renders the same bytes, in the fixed envSecrets order
// FM-12 lists, with LaunchKey appended only when non-empty.
func ImportYAML(d RunDescriptor) ([]byte, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}

	envSecrets := []envKV{
		{"ZCP_VSCODE", "true"},
		{"ZCP_FARM_BATCH", d.BatchID},
		{"ZCP_FARM_RUN", d.RunID},
		{"ZCP_FARM_SCENARIO", d.ScenarioID},
		{"ZCP_FARM_EVALUATOR_SHA", d.EvaluatorSHA256},
		{"ZCP_FARM_CANDIDATE_SHA", d.CandidateSHA256},
		{"ZCP_FARM_SCENARIOS_DIGEST", d.ScenariosDigest},
		{"ZCP_FARM_S3_URL", d.Sink.URL},
		{"ZCP_FARM_S3_BUCKET", d.Sink.Bucket},
		{"ZCP_FARM_S3_KEY", d.Sink.Key},
		{"ZCP_FARM_S3_SECRET", d.Sink.Secret},
		{"CLAUDE_CODE_OAUTH_TOKEN", d.OAuthToken},
	}
	if d.LaunchKey != "" {
		envSecrets = append(envSecrets, envKV{"ZCP_E2E_LAUNCH_KEY", d.LaunchKey})
	}

	data := templateData{
		ProjectName:    d.ProjectName(),
		Hostname:       serviceHostname,
		EnvSecretsYAML: buildEnvSecretsYAML(envSecrets),
	}

	var buf bytes.Buffer
	if err := importYAMLTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render import yaml: %w", err)
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

var importYAMLTemplate = template.Must(template.New("importYAML").Parse(`project:
  name: {{.ProjectName}}
  tags: [zcp-farm, disposable]
  sshIsolation: "vpn project"
services:
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
                curl -sSf --aws-sigv4 aws:amz:us-east-1:s3 --user "$ZCP_FARM_S3_KEY:$ZCP_FARM_S3_SECRET" -o /tmp/farm-wrapper.sh "$ZCP_FARM_S3_URL/$ZCP_FARM_S3_BUCKET/farm/wrapper.sh"
                chmod +x /tmp/farm-wrapper.sh
                nohup /tmp/farm-wrapper.sh >/tmp/farm-wrapper.log 2>&1 &
            ports: [ { port: 8080, httpSupport: true } ]
            startCommands:
              - { command: zcp service start nginx, name: nginx }
              - { command: zcp service start vscode, name: vscode }
`))
