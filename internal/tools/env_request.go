package tools

// WithVaultAsks records each ask zerops_env action=request opens under
// stateDir, so a second request for a key and vault whose ask is still open
// shows the person no second prompt (docs/spec-mate.md §5.8). Only a Mate's
// client draws an ask, so only a Mate's zcp records them.
func WithVaultAsks(stateDir string) EnvOption {
	return func(o *envOptions) { o.vaultAsks = stateDir }
}
