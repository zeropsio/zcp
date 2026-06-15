package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// prod_runtime_base.go — resolve a codebase's prod-time runtime base
// from its on-disk zerops.yaml.
//
// Why this exists. The bare Codebase carries one runtime identifier
// (BaseRuntime) which mainline recipes use uniformly — the build base
// equals the prod runtime base (NestJS builds on nodejs@22, runs on
// nodejs@22; Laravel builds on php-nginx@8.4, runs on php-nginx@8.4).
//
// Vite-SPA-style codebases break the symmetry. Their zerops.yaml prod
// setup builds with `base: nodejs@22` (toolchain) and runs with
// `base: static` (Nginx-served bundle). The deliverable import.yaml
// declares `services[].type` for the runtime the porter deploys
// against — that's the prod `run.base`, not the build base.
//
// Run-32 shipped Vite-SPA recipes whose stage import.yaml declared
// `type: nodejs@22 zeropsSetup: prod` while the codebase's prod
// runtime was `base: static`. The deploy then booted nodejs@22 with
// no `start` command, idled until readinessCheck timed out.
//
// This file parses the codebase's zerops.yaml, locates the prod setup
// (matched by Role.Contract().ZeropsSetupProd), follows the `extends:`
// chain when needed, and returns the resolved `run.base`. Stitch
// populates Codebase.ProdRuntimeBase before deliverable emit.

// parseProdRuntimeBaseFromYaml extracts the prod-time `run.base` from
// a codebase's zerops.yaml body. setupName is the prod setup name
// (typically "prod" — comes from Role.Contract().ZeropsSetupProd).
//
// Resolution rules:
//   - Locate the entry in the `zerops:` array whose `setup:` matches
//     setupName.
//   - If that entry has its own `run.base`, return it.
//   - Else if the entry declares `extends: <parentSetupName>`, locate
//     the parent setup and return its `run.base`.
//   - Else return empty string (callers fall back to BaseRuntime).
//
// The `run.base` value can be a scalar (e.g. "static",
// "php-nginx@8.4") or a sequence (multi-base — e.g.
// `[php@8.4, nodejs@22]`). For runtime semantics only single-scalar
// values make sense (you don't run a service on two bases); when the
// resolved node is a sequence we return the FIRST scalar element so
// callers who must pick one have a deterministic answer, but
// multi-base prod setups are atypical and the caller's fallback to
// BaseRuntime is the safer default.
//
// Returns empty string + nil error when the yaml parses but the prod
// setup / run.base / extends chain doesn't yield a value (the
// fallback path is correct for the common case where build base ==
// prod runtime base).
func parseProdRuntimeBaseFromYaml(yamlBody, setupName string) (string, error) {
	if strings.TrimSpace(yamlBody) == "" || setupName == "" {
		return "", nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlBody), &doc); err != nil {
		return "", fmt.Errorf("parse codebase zerops.yaml: %w", err)
	}
	root := docRoot(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return "", nil
	}
	zeropsSeq := mappingChild(root, "zerops")
	if zeropsSeq == nil || zeropsSeq.Kind != yaml.SequenceNode {
		return "", nil
	}
	// Build a setup-name → entry index for extends-chain resolution.
	setups := map[string]*yaml.Node{}
	for _, entry := range zeropsSeq.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		if name := scalarChild(entry, "setup"); name != "" {
			setups[name] = entry
		}
	}
	prodEntry, ok := setups[setupName]
	if !ok {
		return "", nil
	}
	// First, try this setup's own run.base.
	if base := runBaseScalar(prodEntry); base != "" {
		return base, nil
	}
	// Fall back to the extends chain. Single-step is enough for the
	// jetstream pattern (prod extends base); a defensive depth limit
	// prevents extends-cycle infinite loops.
	visited := map[string]bool{setupName: true}
	current := prodEntry
	for range 8 {
		parentName := scalarChild(current, "extends")
		if parentName == "" {
			return "", nil
		}
		if visited[parentName] {
			return "", nil // cycle
		}
		visited[parentName] = true
		parent, ok := setups[parentName]
		if !ok {
			return "", nil
		}
		if base := runBaseScalar(parent); base != "" {
			return base, nil
		}
		current = parent
	}
	return "", nil
}

// docRoot returns the document's first content node when the input is
// a yaml.DocumentNode wrapper, or the node itself when already a
// mapping.
func docRoot(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

// runBaseScalar returns the `run.base` value of a setup-entry mapping
// when present and resolvable to a scalar. Returns the FIRST scalar
// when run.base is a sequence (multi-base prod setups are atypical
// for runtime contexts; callers fall back to BaseRuntime when
// ambiguous). Empty string when no run.base or unresolvable.
func runBaseScalar(setup *yaml.Node) string {
	if setup == nil || setup.Kind != yaml.MappingNode {
		return ""
	}
	runNode := mappingChild(setup, "run")
	if runNode == nil || runNode.Kind != yaml.MappingNode {
		return ""
	}
	baseNode := mappingChild(runNode, "base")
	if baseNode == nil {
		return ""
	}
	switch baseNode.Kind {
	case yaml.ScalarNode:
		return strings.TrimSpace(baseNode.Value)
	case yaml.SequenceNode:
		for _, c := range baseNode.Content {
			if c.Kind == yaml.ScalarNode {
				return strings.TrimSpace(c.Value)
			}
		}
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
		// `run.base` is scalar or sequence by spec; document /
		// mapping / alias shapes don't appear in this position.
		// Falling through silently keeps the resolver conservative —
		// unparseable shapes return "" so callers fall back to
		// BaseRuntime (the safe default).
	}
	return ""
}

// scalarChild returns the scalar string under key if present.
func scalarChild(m *yaml.Node, key string) string {
	if m == nil || m.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && m.Content[i+1].Kind == yaml.ScalarNode {
			return strings.TrimSpace(m.Content[i+1].Value)
		}
	}
	return ""
}

// populateProdRuntimeBaseFromYaml reads each codebase's on-disk
// zerops.yaml at <SourceRoot>/zerops.yaml, parses, and writes the
// per-codebase ProdRuntimeBase. Idempotent — safe to call multiple
// times. Codebases without a SourceRoot or with an unreadable yaml
// retain whatever value was already set (typically empty, which makes
// the emitter fall back to BaseRuntime — the symmetric case).
//
// Called from stitchContent before regenerating tier yamls, so the
// deliverable emitter sees the resolved value.
func populateProdRuntimeBaseFromYaml(sess *Session) error {
	sess.mu.Lock()
	if sess.Plan == nil {
		sess.mu.Unlock()
		return nil
	}
	codebases := make([]Codebase, len(sess.Plan.Codebases))
	copy(codebases, sess.Plan.Codebases)
	sess.mu.Unlock()

	resolved := make([]string, len(codebases))
	for i, cb := range codebases {
		if cb.SourceRoot == "" {
			continue
		}
		yamlPath := filepath.Join(cb.SourceRoot, "zerops.yaml")
		body, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		contract, ok := cb.Role.Contract()
		if !ok || contract.ZeropsSetupProd == "" {
			continue
		}
		base, err := parseProdRuntimeBaseFromYaml(string(body), contract.ZeropsSetupProd)
		if err != nil {
			return fmt.Errorf("codebase %q: %w", cb.Hostname, err)
		}
		resolved[i] = base
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.Plan == nil {
		return nil
	}
	for i := range sess.Plan.Codebases {
		if i >= len(resolved) {
			break
		}
		// Only populate when parse yielded a value — empty stays empty
		// so emitter falls back to BaseRuntime (the symmetric case).
		// Do NOT clobber a previously-set value to "" on re-read; the
		// re-read might miss because the yaml moved on disk.
		if resolved[i] != "" {
			sess.Plan.Codebases[i].ProdRuntimeBase = resolved[i]
		}
	}
	return nil
}
