package check

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// symbol-contract-env-consistency walks each `{host}dev/` under
// --mount-root and flags env-var tokens that diverge from the canonical
// names in the plan's SymbolContract. Without --plan-json=<path> the
// predicate runs against an empty contract and short-circuits to a
// vacuous pass — the "degraded mode" per HANDOFF-to-I4 §C-7e design
// decision #3. When --plan-json is provided, the shim loads the plan
// and passes its SymbolContract to the predicate.
func init() {
	registerCheck("symbol-contract-env-consistency", runSymbolContractEnvConsistency)
}

func runSymbolContractEnvConsistency(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("symbol-contract-env-consistency", stderr)
	cf := addCommonFlags(fs)
	mountRoot := fs.String("mount-root", "", "recipe project root (alias for --path)")
	planJSON := fs.String("plan-json", "", "path to a RecipePlan JSON file (optional; absent => degraded pass)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	root := resolveMountRoot(cf.path, *mountRoot)
	var contract workflow.SymbolContract
	if *planJSON != "" {
		data, err := os.ReadFile(*planJSON)
		if err != nil {
			fmt.Fprintf(stderr, "symbol-contract-env-consistency: reading %s: %v\n", *planJSON, err)
			return 1
		}
		var plan workflow.RecipePlan
		if err := json.Unmarshal(data, &plan); err != nil {
			fmt.Fprintf(stderr, "symbol-contract-env-consistency: parsing %s: %v\n", *planJSON, err)
			return 1
		}
		contract = plan.SymbolContract
	}
	checks := opschecks.CheckSymbolContractEnvVarConsistency(ctx, root, contract)
	return emitResults(stdout, cf.json, checks)
}
