package adapters

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// trustedDomainsKey is code-server's own CLI-flag name
// (--link-protection-trusted-domains), also honored as a config.yaml key.
// Live-verified against code-server 4.129.0 (VS Code 1.129.0): at process
// startup code-server reads this list and appends product.json's own
// linkProtectionTrustedDomains ([]string{"https://open-vsx.org"}), then
// serves the union to every browser session as the web workbench's static
// trusted-domains list — the one VS Code's link-protection dialog checks
// BEFORE ever prompting. This is server-side and per-instance: it is
// unrelated to the separate, per-user "click Trust on this link" list VS
// Code Web keeps in IndexedDB, scoped to the browser's own origin/profile —
// that list has no server-side file in code-server's web-workbench
// architecture (confirmed: no state.vscdb / SQLite-backed globalStorage
// exists for the web client; ApplicationStorage is
// browser-IndexedDB-backed, see workbench.web.main.internal.js's
// createApplicationStorage), so it cannot be pre-seeded from the container.
// Because config.yaml is parsed once at process start, a domain added here
// takes effect on the NEXT code-server (re)start, not the current session.
const trustedDomainsKey = "link-protection-trusted-domains"

// DefaultTrustedDomains is the ZCP-managed allowlist EnsureTrustedDomains
// merges into code-server's config.yaml. Suppresses the "Do you want to
// open the external website?" prompt for the Zerops site (app./docs.
// covered by the wildcard), Zerops' YouTube channel, and GitHub.
var DefaultTrustedDomains = []string{
	"https://zerops.io",
	"https://*.zerops.io",
	"https://www.youtube.com",
	"https://github.com",
}

// EnsureTrustedDomains idempotently merges DefaultTrustedDomains into
// code-server's config.yaml under home. Skips silently (nil error) when no
// code-server config directory exists — `zcp init` also runs on plain
// laptops with no code-server install. Any other failure (malformed
// existing YAML, a write error) is returned to the caller rather than
// risking a corrupted config; callers that must never fail `zcp init` over
// this cosmetic feature are expected to downgrade the error to a warning
// (see init.generateTrustedDomains).
func EnsureTrustedDomains(home string) error {
	path := codeServerConfigPath(home)

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		if os.IsNotExist(err) {
			return nil // no code-server on this host — nothing to do
		}
		return fmt.Errorf("stat %s: %w", filepath.Dir(path), err)
	}

	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	merged, changed, err := mergeTrustedDomains(raw, DefaultTrustedDomains)
	if err != nil {
		return fmt.Errorf("merge %s: %w", path, err)
	}
	if !changed {
		return nil
	}
	if err := writeConfigPreservingMode(path, merged); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// codeServerConfigPath resolves code-server's config.yaml using the same
// precedence code-server's own readConfigFile applies: $CODE_SERVER_CONFIG
// first, then $XDG_CONFIG_HOME/code-server/config.yaml, then
// ~/.config/code-server/config.yaml.
func codeServerConfigPath(home string) string {
	if p := os.Getenv("CODE_SERVER_CONFIG"); p != "" {
		return p
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "code-server", "config.yaml")
}

// mergeTrustedDomains is the pure merge core. It parses raw (nil/empty for
// a not-yet-existing config.yaml) as YAML, upserts trustedDomainsKey with
// the union of its existing entries plus want — existing entries and their
// order preserved, want appended only when missing — and re-marshals.
// Returns changed=false and the original bytes verbatim when every wanted
// domain was already present, so the caller can skip the write entirely.
func mergeTrustedDomains(raw []byte, want []string) (out []byte, changed bool, err error) {
	var data map[string]any
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &data); err != nil {
			return nil, false, fmt.Errorf("parse yaml: %w", err)
		}
	}
	if data == nil {
		data = map[string]any{}
	}

	existing := stringList(data[trustedDomainsKey])
	present := make(map[string]bool, len(existing))
	for _, d := range existing {
		present[d] = true
	}

	merged := existing
	added := false
	for _, w := range want {
		if present[w] {
			continue
		}
		merged = append(merged, w)
		present[w] = true
		added = true
	}
	if !added {
		return raw, false, nil
	}

	data[trustedDomainsKey] = merged
	out, err = yaml.Marshal(data)
	if err != nil {
		return nil, false, fmt.Errorf("marshal yaml: %w", err)
	}
	return out, true, nil
}

// stringList normalizes a YAML-decoded value at trustedDomainsKey into a
// []string, tolerating shapes a hand-edited config.yaml might carry:
// absent/nil → empty; []any of strings (canonical) → converted; a bare
// scalar string → wrapped to a single-entry list (preserved, not dropped).
// Any other shape is treated as absent — code-server itself expects
// string[] here, so a config with e.g. a nested map at this key is already
// broken for code-server's own parser; the next merge simply replaces it
// with the ZCP-managed list rather than crashing.
func stringList(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
