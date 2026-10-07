package bundle

import (
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// Every import YAML zcp writes carries its values in the import schema's
// `vault:` blocks — the project's and each service's own — never the
// deprecated envVariables, envSecrets or dotEnvSecrets. A plain value is
// written `KEY: value`; a sensitive one `KEY: {value: …, sensitive: true}`,
// so the file says what it means to every reader.

// vaultValue is one value of a `vault:` block.
type vaultValue struct {
	value     string
	sensitive bool
}

// newVaultValue is a value as zcp writes it into a vault. The name rule
// decides (topology.DefaultSensitive); a value zcp itself judged a secret —
// generated, a placeholder, a value someone flagged sensitive — is sensitive
// whatever its name, but for a name readable by design (a public key, which a
// browser bundle ships anyway).
// A value made only of references is wiring and stays plain whatever was
// judged of its key: it holds no secret of its own.
func newVaultValue(key, value string, secret bool) vaultValue {
	sensitive := !topology.IsReferenceOnly(value) &&
		(topology.DefaultSensitive(key) || (secret && !topology.ReadableByDesign(key)))
	return vaultValue{value: value, sensitive: sensitive}
}

// MarshalYAML writes the value in the form the import schema reads.
func (v vaultValue) MarshalYAML() (any, error) {
	return vaultNode(v), nil
}

// vaultNode is a plain value's string scalar, or a sensitive value's flow
// mapping `{value: …, sensitive: true}`.
func vaultNode(v vaultValue) *yaml.Node {
	scalar := valueNode(v.value)
	if !v.sensitive {
		return scalar
	}
	return &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "value"}, scalar,
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "sensitive"}, {Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
	}}
}

// usesPreprocessorDirective reports a value carrying a `<@…>` directive, which
// the platform expands only under the preprocessor's first line.
func usesPreprocessorDirective(value string) bool {
	return strings.Contains(value, "<@") && strings.Contains(value, ")>")
}
