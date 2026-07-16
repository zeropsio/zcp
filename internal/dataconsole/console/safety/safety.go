// Package safety is the server-side write gate. It is the single choke point
// every mutating handler routes through.
//
// THE REAL BOUNDARY IS THE BEARER, not the per-op confirm. Any web page can POST
// to 127.0.0.1, so the per-process bearer token is what actually authorizes a
// caller; the X-Confirm header is a deliberate "did you mean it" INTENT signal
// (the client must opt each mutation in), NOT a security boundary — a holder of
// the bearer can set it freely. The two real gates are: (1) the --allow-writes
// launch posture (read-only by default), and (2) the engine-level enforcement in
// each provider (SQL Query in a READ ONLY transaction; KV by command allowlist),
// so a write cannot slip through even a mis-routed handler.
//
// Environment is the prod-discriminator hook: v1 has no neutral prod/stage signal
// from the local model, so confirm is required unconditionally; once a deployment
// surfaces an Environment, a "production" posture can gate harder (a typed
// re-confirm, a deny) without touching every call site.
package safety

import "github.com/zeropsio/zcp/internal/dataconsole/console/provider"

// Policy is the process-wide write posture, set once from the launch flag.
type Policy struct {
	AllowWrites bool
	// Environment is the prod-discriminator hook (e.g. "production"/"stage"); empty
	// in v1. It does not change v1 behavior — it is the seam a prod gate plugs into.
	Environment string
}

// AuthorizeWrite returns nil only when writes are enabled AND the caller supplied
// an explicit confirm. confirmed is the X-Confirm header presence — an intent
// signal, not the security boundary (that is the bearer + the engine-level gate).
func (p Policy) AuthorizeWrite(confirmed bool) error {
	if !p.AllowWrites {
		return provider.ErrReadOnly
	}
	if !confirmed {
		return provider.ErrNeedsConfirm
	}
	return nil
}
