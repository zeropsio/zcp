package ops

import (
	"fmt"
	"maps"
	"slices"

	"gopkg.in/yaml.v3"
)

// maxExtendsDepth is how deep the platform follows an extends chain.
const maxExtendsDepth = 25

// resolveExtends returns zerops.yaml's setups as the platform resolves
// `extends` before it builds (zerops-docs zerops-yaml/specification.mdx
// "extends"): a setup is a copy of its resolved parents — a list of them
// applied in order, later ones over earlier — with its own keys merged on top,
// recursively key by key; lists are replaced whole and a key set to null is
// removed. A parent that is unknown, already on the chain (a cycle) or past
// maxExtendsDepth is skipped: the platform's own validation names that error.
func resolveExtends(setups []map[string]any) []map[string]any {
	byName := map[string]map[string]any{}
	for _, s := range setups {
		if name, ok := s["setup"].(string); ok && name != "" {
			byName[name] = s
		}
	}
	// path holds the setups being resolved, so a cycle ends.
	var resolve func(s map[string]any, path []string) map[string]any
	resolve = func(s map[string]any, path []string) map[string]any {
		out := map[string]any{}
		name, _ := s["setup"].(string)
		path = append(path, name)
		if len(path) <= maxExtendsDepth {
			for _, parent := range extendsNames(s["extends"]) {
				if p, ok := byName[parent]; ok && !slices.Contains(path, parent) {
					out = mergeYAML(out, resolve(p, path))
				}
			}
		}
		out = mergeYAML(out, s)
		delete(out, "extends")
		return out
	}
	resolved := make([]map[string]any, len(setups))
	for i, s := range setups {
		resolved[i] = s
		if s["extends"] != nil {
			resolved[i] = resolve(s, nil)
		}
	}
	return resolved
}

// extendsNames names the setups an extends value names: a scalar or a list.
func extendsNames(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// mergeYAML applies over on top of a copy of base: maps merge key by key, a
// null removes the key, anything else replaces.
func mergeYAML(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	maps.Copy(out, base)
	for k, v := range over {
		if v == nil {
			delete(out, k)
			continue
		}
		bm, bok := out[k].(map[string]any)
		om, ook := v.(map[string]any)
		if bok && ook {
			out[k] = mergeYAML(bm, om)
			continue
		}
		out[k] = v
	}
	return out
}

// decodeResolved decodes data's setups with extends resolved into doc.
func decodeResolved(data []byte, doc *ZeropsYmlDoc) error {
	var raw struct {
		Zerops []map[string]any `yaml:"zerops"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("read setups: %w", err)
	}
	b, err := yaml.Marshal(map[string]any{"zerops": resolveExtends(raw.Zerops)})
	if err != nil {
		return fmt.Errorf("resolve extends: %w", err)
	}
	doc.Zerops = nil
	if err := yaml.Unmarshal(b, doc); err != nil {
		return fmt.Errorf("resolve extends: %w", err)
	}
	return nil
}
