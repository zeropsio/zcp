package content

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
)

// BuildClaudeMD composes the env-rendered CLAUDE.md content from three
// embedded templates: claude_shared.md (env-agnostic body) plus exactly
// one env-specific preamble (claude_container.md or claude_local.md).
//
// Container preamble carries a {{.SelfHostname}} template var, resolved
// to rt.ServiceName at composition time. The composed output is wrapped
// in <!-- ZCP:BEGIN/END --> markers by the caller (init.generateCLAUDEMD).
//
// Render is install-time: zcp init detects rt.InContainer and freezes
// the env into the disk file. Subsequent zcp serve runs do not
// re-render. Env is stable per install; if the install moves between
// envs, zcp init must be re-run to refresh CLAUDE.md.
func BuildClaudeMD(rt runtime.Info) (string, error) {
	shared, err := GetTemplate("claude_shared.md")
	if err != nil {
		return "", fmt.Errorf("read claude_shared.md: %w", err)
	}

	var preamble string
	if rt.InContainer {
		tmpl, err := GetTemplate("claude_container.md")
		if err != nil {
			return "", fmt.Errorf("read claude_container.md: %w", err)
		}
		preamble = strings.ReplaceAll(tmpl, "{{.SelfHostname}}", rt.ServiceName)
	} else {
		tmpl, err := GetTemplate("claude_local.md")
		if err != nil {
			return "", fmt.Errorf("read claude_local.md: %w", err)
		}
		preamble = tmpl
	}

	return "# Zerops\n\n" +
		strings.TrimSpace(preamble) + "\n\n" +
		strings.TrimSpace(shared) + "\n", nil
}
