package workflow

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// mateTierStartKeys are the keys of a tier entry a start-empty import does
// not take from it: its build (buildFromGit, zeropsSetup), which a Mate's
// birth takes out of every runtime built from the application's repositories
// since the platform cannot clone them, and how it starts (startWithoutCode,
// override), which the import itself states.
var mateTierStartKeys = []string{"buildFromGit", "zeropsSetup", "startWithoutCode", "override"}

// MateTierStartEmpty is the tier's own entry for the runtime hostname as an
// import that brings it up running and empty: the entry as the tier writes it
// — its type, vault, scale and access — with its build taken out, as a Mate's
// birth takes it out, and startWithoutCode set. A re-import with override
// replaces the service, so live holds what the running service carries
// beyond its entry — its envIsolation and sshIsolation, which the birth sets
// on every service — and each is set where the entry states none. The tier's
// leading comments go first, its preprocessor line among them, so a vault
// value the tier generates is generated again, never stored as its text.
// Pure; an error when the tier does not read or names no such runtime.
func MateTierStartEmpty(importYAML, hostname string, live map[string]string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(importYAML), &doc); err != nil {
		return "", fmt.Errorf("%w: %w", ErrMateTierUnreadable, err)
	}
	services := mappingValue(documentRoot(&doc), "services")
	if services == nil || services.Kind != yaml.SequenceNode {
		return "", fmt.Errorf("%w: it declares no services", ErrMateTierUnreadable)
	}
	var tierEntry *yaml.Node
	for _, svc := range services.Content {
		if strings.TrimSpace(mappingScalar(svc, "hostname")) == hostname {
			tierEntry = svc
			break
		}
	}
	if tierEntry == nil {
		return "", fmt.Errorf("the AI Agent tier names no service %s", hostname)
	}

	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	stated := map[string]bool{}
	for i := 0; i+1 < len(tierEntry.Content); i += 2 {
		key := tierEntry.Content[i].Value
		if slices.Contains(mateTierStartKeys, key) {
			continue
		}
		stated[key] = true
		entry.Content = append(entry.Content, tierEntry.Content[i], tierEntry.Content[i+1])
	}
	entry.Content = append(entry.Content, yamlKey("startWithoutCode"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	keys := make([]string, 0, len(live))
	for key := range live {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if value := strings.TrimSpace(live[key]); value != "" && !stated[key] {
			entry.Content = append(entry.Content, yamlKey(key), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		}
	}

	out, err := yaml.Marshal(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		yamlKey("services"), {Kind: yaml.SequenceNode, Content: []*yaml.Node{entry}},
	}})
	if err != nil {
		return "", fmt.Errorf("start-empty import marshal: %w", err)
	}
	return leadingComments(importYAML) + string(out), nil
}

// yamlKey is a plain string scalar for a mapping key.
func yamlKey(key string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
}

// leadingComments are the comment and blank lines a document opens with.
func leadingComments(body string) string {
	var head strings.Builder
	for line := range strings.SplitAfterSeq(body, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		head.WriteString(line)
	}
	return head.String()
}
