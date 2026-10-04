package platform

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// markerOnlyProjectTags filters the import's effective project.tags, including
// YAML aliases and merges. Project metadata belongs in Mate HQ; Zerops carries
// only the exact mate marker. Other import fields retain their values, even
// when they alias project fields.
func markerOnlyProjectTags(input string) (string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(input), &document); err != nil {
		return "", fmt.Errorf("read project import YAML: %w", err)
	}
	var top map[string]yaml.Node
	if err := document.Decode(&top); err != nil {
		return "", fmt.Errorf("read project import fields: %w", err)
	}
	project, found := top["project"]
	if !found {
		return input, nil
	}
	var fields map[string]yaml.Node
	if err := project.Decode(&fields); err != nil {
		return "", fmt.Errorf("read import project: %w", err)
	}
	tagsNode, found := fields["tags"]
	if !found {
		return input, nil
	}
	var tags []string
	if err := tagsNode.Decode(&tags); err != nil {
		return "", fmt.Errorf("read import project tags: %w", err)
	}
	marker := false
	dropped := 0
	for _, tag := range tags {
		if tag == "mate" {
			marker = true
		} else {
			dropped++
		}
	}
	if dropped == 0 && len(tags) == 1 {
		return input, nil
	}
	// Materialize aliases independently before changing their former targets.
	// Otherwise removing an anchor dangles aliases, or filtering it changes an
	// unrelated service field. Cyclic aliases cannot be imported safely.
	remaining := 10000
	expanded, err := independentYAMLNodes(&document, make(map[*yaml.Node]bool), &remaining)
	if err != nil {
		return "", fmt.Errorf("read project import aliases: %w", err)
	}
	document = *expanded
	if err := document.Decode(&top); err != nil {
		return "", fmt.Errorf("read expanded import fields: %w", err)
	}
	project = top["project"]
	if err := project.Decode(&fields); err != nil {
		return "", fmt.Errorf("read expanded import project: %w", err)
	}
	tagsNode = fields["tags"]
	if marker {
		if err := tagsNode.Encode([]string{"mate"}); err != nil {
			return "", fmt.Errorf("encode project marker: %w", err)
		}
		fields["tags"] = tagsNode
	} else {
		delete(fields, "tags")
	}
	if err := project.Encode(fields); err != nil {
		return "", fmt.Errorf("encode import project: %w", err)
	}
	root := document.Content[0]
	replaced := false
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "project" {
			root.Content[i+1] = &project
			replaced = true
			break
		}
	}
	if !replaced {
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "project"}, &project)
	}
	output, err := yaml.Marshal(&document)
	if err != nil {
		return "", fmt.Errorf("encode project import YAML: %w", err)
	}
	if dropped > 0 {
		fmt.Fprintf(os.Stderr, "[zcp] project import: dropped %d project tags; Mate metadata facts must be stored in Mate HQ, keyed by project id; Zerops keeps only the exact mate marker\n", dropped)
	}
	return string(output), nil
}

// independentYAMLNodes resolves aliases into distinct values and removes anchor
// labels that are no longer referenced, while keeping mapping order and styles.
// A shared node budget bounds both depth and exponential alias expansion.
func independentYAMLNodes(node *yaml.Node, ancestors map[*yaml.Node]bool, remaining *int) (*yaml.Node, error) {
	if *remaining == 0 {
		return nil, fmt.Errorf("YAML alias expansion limit exceeded")
	}
	*remaining--
	if node == nil || ancestors[node] {
		return nil, fmt.Errorf("cyclic or missing YAML alias")
	}
	ancestors[node] = true
	defer delete(ancestors, node)
	if node.Kind == yaml.AliasNode {
		return independentYAMLNodes(node.Alias, ancestors, remaining)
	}
	copyNode := *node
	copyNode.Anchor = ""
	copyNode.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		copyChild, err := independentYAMLNodes(child, ancestors, remaining)
		if err != nil {
			return nil, err
		}
		copyNode.Content[i] = copyChild
	}
	return &copyNode, nil
}
