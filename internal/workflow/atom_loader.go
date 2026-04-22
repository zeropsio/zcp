package workflow

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"text/template"

	"github.com/zeropsio/zcp/internal/content"
)

// RecipeProjectRoot is the SSHFS mount point recipe sub-agents write
// under. Hard-coded here rather than taken from WorkflowState because
// atom rendering is a pure function — the render context binds the
// literal path, dispatch prompts embed concrete locations, and the
// agent sees "/var/www/apidev/README.md" not a templated placeholder.
const RecipeProjectRoot = "/var/www"

// AtomRenderContext carries every template field the recipe atom
// corpus can reference. Bound by Go's text/template package at load
// time via LoadAtomBodyRendered. The B-22 lint (tools/lint/atom_template_vars)
// validates that every `{{.Field}}` in every atom names a field on
// this struct — dispatch time fails loudly instead of silently
// producing `{{.UnknownField}}` in a sub-agent prompt (the F-9
// failure mode v36 suffered before Cx-ENVFOLDERS-WIRED landed).
type AtomRenderContext struct {
	ProjectRoot  string
	Slug         string
	Tier         string
	Framework    string
	Hostnames    []string
	Hostname     string // per-codebase dispatch (scaffold atoms); "" in plan-wide context
	EnvFolders   []string
	FactsLogPath string
	ManifestPath string
	PlanPath     string
}

// RenderContextFromPlan builds an AtomRenderContext from an active
// recipe plan. nil plan → zero-value context (LoadAtomBodyRendered
// then returns the raw atom body unchanged so pre-session debug
// fetches still work). hostname scopes scaffold-atom renders to one
// codebase; pass "" for the plan-wide (writer/editorial) path.
//
// Hostnames are extracted from runtime targets that own their own
// codebase — non-worker runtimes OR workers with SharesCodebaseWith
// unset. Managed/utility services are excluded because the writer
// never authors docs for them.
func RenderContextFromPlan(plan *RecipePlan, hostname string) AtomRenderContext {
	if plan == nil {
		return AtomRenderContext{}
	}
	var hostnames []string
	for _, t := range plan.Targets {
		if !IsRuntimeType(t.Type) {
			continue
		}
		if t.IsWorker && t.SharesCodebaseWith != "" {
			continue
		}
		hostnames = append(hostnames, t.Hostname)
	}
	return AtomRenderContext{
		ProjectRoot: RecipeProjectRoot,
		Slug:        plan.Slug,
		Tier:        plan.Tier,
		Framework:   plan.Framework,
		Hostnames:   hostnames,
		Hostname:    hostname,
		EnvFolders:  CanonicalEnvFolders(),
	}
}

// isZeroRenderContext reports whether a context carries no plan data.
// Used by LoadAtomBodyRendered to short-circuit to raw-body delivery
// in pre-session debug paths.
func isZeroRenderContext(ctx AtomRenderContext) bool {
	return ctx.ProjectRoot == "" && len(ctx.Hostnames) == 0 && len(ctx.EnvFolders) == 0 &&
		ctx.Slug == "" && ctx.Tier == "" && ctx.Framework == "" &&
		ctx.Hostname == "" && ctx.FactsLogPath == "" && ctx.ManifestPath == "" && ctx.PlanPath == ""
}

// LoadAtomBodyRendered loads the atom body and performs Go text/
// template substitution using ctx. On zero-value contexts the raw
// body is returned verbatim (same contract as LoadAtomBody) so pre-
// session debug fetches still work.
//
// `missingkey=error` means a `{{.X}}` reference to a map key that
// doesn't exist fails at execution — combined with the B-22 build-
// time lint, this forecloses F-9-class regressions. An atom that
// slips an unknown `{{.FakeField}}` past the lint blows up at
// dispatch rather than shipping to a sub-agent as literal text.
func LoadAtomBodyRendered(id string, ctx AtomRenderContext) (string, error) {
	raw, err := LoadAtomBody(id)
	if err != nil {
		return "", err
	}
	if isZeroRenderContext(ctx) {
		return raw, nil
	}
	tmpl, err := template.New(id).Option("missingkey=error").Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse atom %q: %w", id, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("render atom %q: %w", id, err)
	}
	return buf.String(), nil
}

// atomEmbedPrefix is the path prefix inside content.RecipeAtomsFS. The
// embed directive preserves relative paths from the content package root,
// so atom paths declared as "phases/research/entry.md" are read from
// "workflows/recipe/phases/research/entry.md" inside the FS.
const atomEmbedPrefix = "workflows/recipe/"

// LoadAtom reads the atom with the given manifest ID and returns its body.
// Returns an error if the ID is not registered or the file is missing from
// the embedded tree. Callers treat the error as a hard failure — every
// atom ID in the manifest must be backed by an embedded file.
func LoadAtom(id string) (string, error) {
	path, ok := AtomPath(id)
	if !ok {
		return "", fmt.Errorf("atom %q not registered in manifest", id)
	}
	return loadAtomByPath(path)
}

// LoadAtomBody is a defensive wrapper that returns the atom body without
// any trailing newline stripping. Use this when concatenating atoms at
// stitch time so consumers see content bodies as authored.
func LoadAtomBody(id string) (string, error) {
	body, err := LoadAtom(id)
	if err != nil {
		return "", err
	}
	// Strip at most one trailing newline so successive stitched atoms
	// separated by "\n---\n" don't accumulate blank lines between them.
	return strings.TrimSuffix(body, "\n"), nil
}

// loadAtomByPath is the internal path-based reader. Exported helpers go
// through AtomPath first so unregistered paths are rejected consistently.
func loadAtomByPath(path string) (string, error) {
	fullPath := atomEmbedPrefix + path
	data, err := fs.ReadFile(content.RecipeAtomsFS, fullPath)
	if err != nil {
		return "", fmt.Errorf("read atom %q: %w", path, err)
	}
	return string(data), nil
}

// AtomExists reports whether the atom ID resolves to an embedded file.
// Used by tests + the dry-run harness (C-14) to cross-check manifest
// against filesystem state without raising a hard error per missing file.
func AtomExists(id string) bool {
	path, ok := AtomPath(id)
	if !ok {
		return false
	}
	_, err := fs.ReadFile(content.RecipeAtomsFS, atomEmbedPrefix+path)
	return err == nil
}

// concatAtoms loads the named atom IDs and concatenates their bodies with
// "\n---\n" separators. Empty atom IDs are skipped (used by tier-branching
// callers that pass "" when an atom doesn't apply). Returns the first
// load error encountered.
func concatAtoms(ids ...string) (string, error) {
	var parts []string
	for _, id := range ids {
		if id == "" {
			continue
		}
		body, err := LoadAtomBody(id)
		if err != nil {
			return "", err
		}
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}
