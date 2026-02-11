package content

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed workflows/*.md
var workflowFS embed.FS

//go:embed templates/*
var templateFS embed.FS

// GetWorkflow returns the content of a named workflow.
// The name should not include the .md extension or path prefix.
func GetWorkflow(name string) (string, error) {
	data, err := workflowFS.ReadFile("workflows/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("workflow %q not found: available workflows: %s",
			name, strings.Join(ListWorkflows(), ", "))
	}
	return string(data), nil
}

// GetTemplate returns the content of a named template file.
// The name should include the file extension (e.g., "claude.md", "mcp-config.json").
func GetTemplate(name string) (string, error) {
	data, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return "", fmt.Errorf("template %q not found", name)
	}
	return string(data), nil
}

// ListWorkflows returns sorted names of all available workflows (without extension).
func ListWorkflows() []string {
	entries, err := fs.ReadDir(workflowFS, "workflows")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if ext := filepath.Ext(name); ext == ".md" {
			names = append(names, strings.TrimSuffix(name, ext))
		}
	}
	sort.Strings(names)
	return names
}
