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

// CredentialMode names which single model-request credential kind a run
// descriptor carries (docs/spec-eval-farm.md §2.4, FM-16).
type CredentialMode string

const (
	// CredentialAPIKey emits ANTHROPIC_API_KEY (the default, spend-limited
	// farm key).
	CredentialAPIKey CredentialMode = "api-key"
	// CredentialOAuthToken emits CLAUDE_CODE_OAUTH_TOKEN (single-interactive-
	// run option).
	CredentialOAuthToken CredentialMode = "oauth-token"
)

// Credential is one model-request credential the run's Claude Code process
// authenticates with. A RunDescriptor carries a slice of these so that
// "both" (len==2) and "neither" (len==0) are both representable and
// rejected by ImportYAML — Claude Code lets an API key shadow an OAuth
// profile, so carrying both makes the credential mode ambiguous (§2.4).
type Credential struct {
	Mode  CredentialMode
	Value string
}

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
	Sink            Sink
	Credentials     []Credential
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

func (d RunDescriptor) credential() (Credential, error) {
	switch len(d.Credentials) {
	case 1:
		c := d.Credentials[0]
		switch c.Mode {
		case CredentialAPIKey, CredentialOAuthToken:
		default:
			return Credential{}, fmt.Errorf("run descriptor: unknown credential mode %q", c.Mode)
		}
		if c.Value == "" {
			return Credential{}, fmt.Errorf("run descriptor: credential mode %q has empty value", c.Mode)
		}
		return c, nil
	case 0:
		return Credential{}, fmt.Errorf("run descriptor: no credential given — exactly one of api-key or oauth-token is required")
	default:
		return Credential{}, fmt.Errorf("run descriptor: %d credentials given — exactly one of api-key or oauth-token is required, never both in one project (§2.4)", len(d.Credentials))
	}
}

func (d RunDescriptor) validate() error {
	if d.RunID == "" {
		return fmt.Errorf("run descriptor: RunID required")
	}
	if _, err := d.credential(); err != nil {
		return err
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
	cred, err := d.credential()
	if err != nil {
		return nil, err
	}

	envSecrets := []envKV{
		{"ZCP_VSCODE", "true"},
		{"ZCP_FARM_BATCH", d.BatchID},
		{"ZCP_FARM_RUN", d.RunID},
		{"ZCP_FARM_SCENARIO", d.ScenarioID},
		{"ZCP_FARM_EVALUATOR_SHA", d.EvaluatorSHA256},
		{"ZCP_FARM_CANDIDATE_SHA", d.CandidateSHA256},
		{"ZCP_FARM_S3_URL", d.Sink.URL},
		{"ZCP_FARM_S3_BUCKET", d.Sink.Bucket},
		{"ZCP_FARM_S3_KEY", d.Sink.Key},
		{"ZCP_FARM_S3_SECRET", d.Sink.Secret},
	}
	switch cred.Mode {
	case CredentialAPIKey:
		envSecrets = append(envSecrets, envKV{"ANTHROPIC_API_KEY", cred.Value})
	case CredentialOAuthToken:
		envSecrets = append(envSecrets, envKV{"CLAUDE_CODE_OAUTH_TOKEN", cred.Value})
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
