package init

import (
	"fmt"
	"os"

	"github.com/zeropsio/zcp/internal/init/adapters"
)

// commandRunner + vsCodeWorkDir are package-level for test affordances
// (export_test.go's Set/Reset* — see preserved API there). The init
// dispatcher reads these and threads them through adapters.Env so the
// adapter implementations stay stateless.
var (
	commandRunner = adapters.DefaultCommandRunner
	vsCodeWorkDir = adapters.DefaultVSCodeWorkDir
)

// defaultCommandRunner + defaultVSCodeWorkDir are the production values
// the test resetters restore. Aliased to adapters package's exported
// defaults so there's one source of truth.
var (
	defaultCommandRunner = adapters.DefaultCommandRunner
	defaultVSCodeWorkDir = adapters.DefaultVSCodeWorkDir
)

// registeredAdapters returns the per-agent adapters tried at container
// init. Order = log emission order; no functional dependency.
//
// Backward compat: every non-Claude adapter's Detect() probes a binary
// (codex / gemini / agy / cursor-agent|agent / grok) and returns false when
// absent. Containers that only ship Claude — every container before
// the multi-agent template — see the other adapters skip silently;
// behavior is identical to pre-multi-agent ZCP.
func registeredAdapters() []adapters.Adapter {
	return []adapters.Adapter{
		adapters.NewClaude(),
		adapters.NewCodex(),
		adapters.NewGemini(),
		adapters.NewAntigravity(),
		adapters.NewCursor(),
		adapters.NewGrok(),
	}
}

// runContainerAdapters dispatches each registered adapter through
// Detect → Validate → ContainerInit. Skipped adapters (Detect=false,
// Validate err) don't break the others — init proceeds for everyone
// detected and validated. ContainerInit errors ARE fatal for that
// adapter (returned to caller) since they indicate a write/merge
// problem the operator needs to see.
//
// GLC-4 invariant preserved: this function performs NO git setup.
// Container Claude install is pure file writes; git initialization for
// mounted dev services is ops.InitServiceGit's job at bootstrap time.
func runContainerAdapters(env adapters.Env) error {
	for _, a := range registeredAdapters() {
		if !a.Detect(env) {
			fmt.Fprintf(os.Stderr, "  → %s: not detected, skip\n", a.Name())
			continue
		}
		warnings, err := a.Validate(env)
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "  → %s warning: %s\n", a.Name(), w)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  → %s validate failed: %v (skip)\n", a.Name(), err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  → %s configs\n", a.Name())
		if err := a.ContainerInit(env); err != nil {
			return fmt.Errorf("%s: %w", a.Name(), err)
		}
	}
	return nil
}
