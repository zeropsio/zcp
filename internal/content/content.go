package content

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed atoms/*.md
var atomFS embed.FS

// GetTemplate returns the content of a named template file.
// The name should include the file extension (e.g., "claude_shared.md", "mcp-config.json").
func GetTemplate(name string) (string, error) {
	data, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return "", fmt.Errorf("template %q not found", name)
	}
	return string(data), nil
}

// ReadAllAtoms returns every embedded atom file as (filename, content) pairs,
// sorted by filename for deterministic load order. Filenames end in ".md".
func ReadAllAtoms() ([]AtomFile, error) {
	entries, err := fs.ReadDir(atomFS, "atoms")
	if err != nil {
		return nil, fmt.Errorf("read atoms dir: %w", err)
	}
	out := make([]AtomFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := atomFS.ReadFile("atoms/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read atom %s: %w", e.Name(), err)
		}
		out = append(out, AtomFile{Name: e.Name(), Content: string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// AtomFile is a raw atom markdown payload from the embedded content FS.
type AtomFile struct {
	Name    string
	Content string
}
