package content

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

//go:embed workflows/*.md
var workflowFS embed.FS

//go:embed templates/*
var templateFS embed.FS

//go:embed atoms/*.md
var atomFS embed.FS

// RecipeAtomsFS exposes the zcprecipator2 atomic content tree under
// internal/content/workflows/recipe/. Consumed by internal/workflow's
// atom-loader. Read-only — callers must not mutate.
//
//go:embed all:workflows/recipe
var RecipeAtomsFS embed.FS

// ExamplesFS exposes the annotated content-surface example bank under
// internal/content/examples/. Consumed by internal/workflow's example
// sampler (examples.go). Each .md file carries frontmatter declaring
// its surface / verdict / reason; see the examples/README.md for the
// schema. Read-only.
//
//go:embed examples/*.md
var ExamplesFS embed.FS

// Workflow files live in an embed.FS, so a "read" is really a copy out of the
// embed table plus a string() conversion — ~116 KB per call for recipe.md.
// On a hot path (every zerops_workflow MCP tool invocation, multiplied by
// retries per session) that's measurable garbage. The cache loads every
// workflow file once at first use and serves all subsequent reads from an
// immutable map — zero behavior change, strict allocation reduction.
var (
	workflowCacheInit    sync.Once
	workflowCacheMu      sync.RWMutex
	workflowCache        map[string]string
	errWorkflowCacheInit error
)

func initWorkflowCache() {
	cache := make(map[string]string)
	entries, err := fs.ReadDir(workflowFS, "workflows")
	if err != nil {
		errWorkflowCacheInit = err
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := workflowFS.ReadFile("workflows/" + e.Name())
		if err != nil {
			errWorkflowCacheInit = err
			return
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		cache[name] = string(b)
	}
	workflowCacheMu.Lock()
	workflowCache = cache
	workflowCacheMu.Unlock()
}

// GetWorkflow returns the content of a named workflow.
// The name should not include the .md extension or path prefix.
func GetWorkflow(name string) (string, error) {
	workflowCacheInit.Do(initWorkflowCache)
	if errWorkflowCacheInit != nil {
		return "", errWorkflowCacheInit
	}
	workflowCacheMu.RLock()
	s, ok := workflowCache[name]
	workflowCacheMu.RUnlock()
	if !ok {
		return "", fmt.Errorf("workflow %q not found: available workflows: %s",
			name, strings.Join(ListWorkflows(), ", "))
	}
	return s, nil
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
