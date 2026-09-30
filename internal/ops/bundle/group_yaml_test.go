package bundle

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// tierMapping parses a tier's import.yaml into its top-level mapping node, the
// form that keeps the order its keys were written in.
func tierMapping(t *testing.T, body string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("tier does not parse: %v\n%s", err, body)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		t.Fatalf("tier is not one mapping:\n%s", body)
	}
	return doc.Content[0]
}

// mappingValue returns the value node under key, nil when the mapping has none.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// mappingKeys lists a mapping's keys in the order they are written.
func mappingKeys(m *yaml.Node) []string {
	keys := make([]string, 0, len(m.Content)/2)
	for i := 0; i+1 < len(m.Content); i += 2 {
		keys = append(keys, m.Content[i].Value)
	}
	return keys
}

// A tier is a document a person reads in the group repo and edits there, so
// it is laid out the way the published recipes are: two spaces, every
// service opening on its hostname and its type and then the rest in the order
// a person asks about it, a blank line between services, and a header saying
// what the tier is, where it came from, and that it is theirs to edit.
func TestBuildGroupRecipe_TierIsWrittenForPeople(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Runtimes[0].ServiceEnvs = []ProjectEnvVar{{Key: "LOG_LEVEL", Value: "debug"}}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	// readingOrder is the order a service's keys are written in; a key a
	// service does not carry is skipped, never reordered around.
	readingOrder := []string{
		"hostname", "type", "priority", "mode", "profile", "zeropsSetup", "buildFromGit",
		"enableSubdomainAccess", "objectStorageSize", "objectStoragePolicy",
		"minContainers", "maxContainers", "verticalAutoscaling", "envSecrets",
	}
	for _, tier := range layout.Tiers {
		t.Run(tier.Title, func(t *testing.T) {
			t.Parallel()
			body := tier.ImportYAML
			lines := strings.Split(body, "\n")

			// The header: what the tier is, who wrote it from where, and that a
			// person may change it — above the project, after the one line the
			// preprocessor needs first.
			header := strings.Join(headerComment(lines), " ")
			for _, want := range []string{tier.Title, "zcp", in.MateProjectName, "edit"} {
				if !strings.Contains(header, want) {
					t.Errorf("the header does not say %q:\n%s", want, header)
				}
			}

			// Two spaces: the project's keys sit two in, every service item
			// opens two in.
			projectAt := slices.Index(lines, "project:")
			if projectAt < 0 || !strings.HasPrefix(lines[projectAt+1], "  ") || strings.HasPrefix(lines[projectAt+1], "   ") {
				t.Errorf("the project block is not indented by two spaces:\n%s", body)
			}
			var items int
			for _, line := range lines {
				if strings.HasPrefix(strings.TrimLeft(line, " "), "- hostname:") {
					items++
					if !strings.HasPrefix(line, "  - hostname:") {
						t.Errorf("a service opens at %q, not two spaces in", line)
					}
				}
			}

			// A blank line before every service after the first.
			for i, line := range lines {
				if strings.HasPrefix(line, "  - hostname:") && i > 0 && !strings.HasPrefix(lines[i-1], "services:") {
					prev := i - 1
					for prev > 0 && strings.HasPrefix(lines[prev], "  #") {
						prev--
					}
					if strings.TrimSpace(lines[prev]) != "" {
						t.Errorf("no blank line before %q", line)
					}
				}
			}

			services := mappingValue(tierMapping(t, body), "services")
			if services == nil || len(services.Content) != items {
				t.Fatalf("services = %v, want %d items", services, items)
			}
			for _, item := range services.Content {
				keys := mappingKeys(item)
				if len(keys) < 2 || keys[0] != "hostname" || keys[1] != "type" {
					t.Errorf("a service opens on %v, want hostname then type", keys)
				}
				last := -1
				for _, key := range keys {
					at := slices.Index(readingOrder, key)
					if at < 0 {
						t.Errorf("service key %q is not in the reading order", key)
						continue
					}
					if at < last {
						t.Errorf("service keys %v are not in the reading order %v", keys, readingOrder)
						break
					}
					last = at
				}
			}
		})
	}
}

// headerComment is the comment block a tier opens with — the lines before the
// first key, the preprocessor's own line excepted.
func headerComment(lines []string) []string {
	var out []string
	for _, line := range lines {
		if line == strings.TrimSuffix(preprocessorHeader, "\n") {
			continue
		}
		if !strings.HasPrefix(line, "#") && strings.TrimSpace(line) != "" {
			break
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "#")))
	}
	return out
}

// The platform's import may read a tier as YAML 1.1, where `yes`, `on`, `y`,
// `~`, `1:30`, `0123` and a date are no strings: a value the recipe writes as
// it is must come back as the same string, so every scalar a YAML 1.1 reader
// would reinterpret is quoted — and nothing else is.
func TestTierDocument_QuotesWhatYAML11Reinterprets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value  string
		quoted bool
	}{
		{"yes", true}, {"Yes", true}, {"NO", true}, {"on", true}, {"OFF", true},
		{"y", true}, {"Y", true}, {"n", true}, {"N", true}, {"true", true}, {"False", true},
		{"~", true}, {"null", true}, {"Null", true}, {"NULL", true}, {"", true},
		{"1:30", true}, {"190:20:30", true}, {"0123", true}, {"0o17", true}, {"0x1F", true},
		{"0b101", true}, {"1_000", true}, {"1.5", true}, {".inf", true}, {"-.Inf", true}, {".NaN", true},
		{"<<", true}, {"=", true}, {"2026-09-30", true}, {"2026-09-30T12:00:00Z", true},
		{"production", false}, {"https://api.example.com/v1", false}, {"yesterday", false},
		{"only", false}, {"Europe/Prague", false}, {"v3.0.1", false}, {"medusa:*", false},
	}
	fields := make([]yamlField, 0, len(tests))
	for i, tt := range tests {
		fields = append(fields, yamlField{key: fmt.Sprintf("V%02d", i), value: tt.value})
	}
	body, err := tierDocument{project: []yamlField{{key: "name", value: "p"}, {key: "envVariables", value: fields}}}.render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	vars := mappingValue(mappingValue(tierMapping(t, body), "project"), "envVariables")
	for i, tt := range tests {
		node := mappingValue(vars, fmt.Sprintf("V%02d", i))
		quoted := node.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0
		if node.Value != tt.value || quoted != tt.quoted {
			t.Errorf("%q is written as %q (quoted %v), want quoted %v", tt.value, node.Value, quoted, tt.quoted)
		}
	}
}
