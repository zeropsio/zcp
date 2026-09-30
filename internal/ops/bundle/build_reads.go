package bundle

import (
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// What a runtime's build reads of other services is the one order a recipe
// has to keep: a storefront whose build pre-renders from its API must be
// built after that API is up, while what a service reads only once it runs
// waits for nothing. Both the recipe writer (the tier's priorities) and the
// stand-up (the order it deploys a new Mate's halves in) read it here.

// runtimeLiftPrefix is how a build names one of the runtime's own variables.
const runtimeLiftPrefix = "RUNTIME_"

// BuildReadValues returns the values a zerops.yaml's build reads for setup
// ("" for every setup): each of its build.envVariables, and for a
// `${RUNTIME_X}` among them the value X has in that setup's run.envVariables,
// else among serviceEnvs — a runtime variable the build lifts. A body that
// does not parse reads nothing.
func BuildReadValues(body, setup string, serviceEnvs map[string]string) []string {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	var doc struct {
		Zerops []struct {
			Setup string `yaml:"setup"`
			Build struct {
				EnvVariables map[string]any `yaml:"envVariables"`
			} `yaml:"build"`
			Run struct {
				EnvVariables map[string]any `yaml:"envVariables"`
			} `yaml:"run"`
		} `yaml:"zerops"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return nil
	}
	var values []string
	for _, s := range doc.Zerops {
		if setup != "" && s.Setup != setup {
			continue
		}
		for _, value := range sortedStringValues(s.Build.EnvVariables) {
			values = append(values, value)
			for _, name := range parseDollarBraceRefs(value) {
				lifted, ok := strings.CutPrefix(name, runtimeLiftPrefix)
				if !ok {
					continue
				}
				if v, ok := s.Run.EnvVariables[lifted].(string); ok {
					values = append(values, v)
				} else if v, ok := serviceEnvs[lifted]; ok {
					values = append(values, v)
				}
			}
		}
	}
	return values
}

// ReferencedHosts returns the hostnames among hosts that values reference as
// `${hostname_key}`, following a `${NAME}` that names a project variable into
// its value; sorted, each once. The longest hostname a reference opens with
// is the one it names (`${api_v2_port}` is api-v2's, not api's), and a
// hostname is matched as the platform's interpolator spells it (`my-db` as
// `my_db`).
func ReferencedHosts(values, hosts []string, projectEnvs map[string]string) []string {
	canonical := make(map[string]string, len(hosts))
	ordered := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h != "" {
			c := canonicalRefHost(h)
			if _, seen := canonical[c]; !seen {
				ordered = append(ordered, c)
			}
			canonical[c] = h
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if len(ordered[i]) != len(ordered[j]) {
			return len(ordered[i]) > len(ordered[j])
		}
		return ordered[i] < ordered[j]
	})
	found := map[string]bool{}
	visited := map[string]bool{}
	var walk func(value string)
	walk = func(value string) {
		for _, name := range parseDollarBraceRefs(value) {
			if h := longestHostPrefix(name, ordered); h != "" {
				found[canonical[h]] = true
				continue
			}
			if next, ok := projectEnvs[name]; ok && !visited[name] {
				visited[name] = true
				walk(next)
			}
		}
	}
	for _, v := range values {
		walk(v)
	}
	out := make([]string, 0, len(found))
	for h := range found {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// longestHostPrefix is the hostname (canonical, longest first) a reference
// name opens with, "" when none.
func longestHostPrefix(name string, hosts []string) string {
	for _, h := range hosts {
		if strings.HasPrefix(name, h+"_") && len(name) > len(h)+1 {
			return h
		}
	}
	return ""
}

// sortedStringValues is a variables block's string values, by key.
func sortedStringValues(vars map[string]any) []string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, k := range keys {
		if s, ok := vars[k].(string); ok {
			values = append(values, s)
		}
	}
	return values
}
