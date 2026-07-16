// Package safety is the server-side write gate: the single choke point every
// mutating handler routes through. It turns a valid, caller-bound WRITE CAPABILITY
// into permission to mutate — and refuses everything else.
//
// THE BOUNDARY IS THE PER-REQUEST WRITE TOKEN. Writes are refused by default;
// authorizing one requires the request to present the process's write token — an
// INDEPENDENT secret minted (only under --allow-writes) alongside the read bearer
// and handed ONLY to the trusted embed host. A caller holding just the read bearer
// — the standalone SPA, which receives the bearer via the URL fragment and never
// sees the write token — can read everything but can never mutate. Write authority
// is therefore CALLER-BOUND (per request), NOT a process-global flag: one console
// process serves both the embed host and standalone tabs, and only the host, which
// alone holds the write token, can write. There is no runtime "arm" step to flip —
// removing it is what closes the standalone-write gap.
//
// The X-Confirm header stays a per-action INTENT signal (the client opts each
// mutation in), NOT the boundary: a bearer holder can set it freely, so it can
// never be what authorizes a write. Two more gates back this up: the --allow-writes
// launch posture (a process launched read-only mints no write token and can NEVER
// authorize a write), and the engine-level read-only build in each provider when
// arming is not permitted (SQL in a READ ONLY transaction; KV by command
// allowlist) — defense-in-depth, NOT the runtime boundary (that is the write-token
// check in the route middleware).
//
// Policy is immutable after construction: it carries no mutable runtime state, so
// AuthorizeWrite is safe from concurrent request handlers with no lock.
package safety

import (
	"crypto/subtle"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// Policy is the process write posture: a read-only default that a request lifts,
// per call, by presenting the process's write token. One instance per process,
// shared by pointer; it is immutable after construction.
type Policy struct {
	// armingPermitted is the launch ceiling (--allow-writes). False means the
	// process can NEVER authorize a write — no write token is minted — and providers
	// are built hard read-only at the engine level. It is NOT "writes are live";
	// each write is authorized per request by the write token below.
	armingPermitted bool
	// writeToken is the caller-bound write capability: an independent secret minted
	// per process (only under --allow-writes) alongside the read bearer and handed
	// ONLY to the trusted embed host. A request authorizes a mutation by presenting
	// it (constant-time compared); the standalone SPA never receives it, so a holder
	// of the read bearer alone can never write. Empty when arming is not permitted.
	writeToken string
	// environment is the prod-discriminator seam (e.g. "production"/"stage"); empty
	// in v1. Unexported so it cannot be mutated after construction (a TOCTOU seam);
	// read via Environment(). Reserved: a future production posture gates harder off it.
	environment string
}

// NewPolicy builds the process write policy. armingPermitted comes from the
// --allow-writes launch flag; writeToken is the per-process write capability (empty
// when arming is not permitted — AuthorizeWrite refuses regardless).
func NewPolicy(armingPermitted bool, writeToken, environment string) *Policy {
	return &Policy{armingPermitted: armingPermitted, writeToken: writeToken, environment: environment}
}

// ArmingPermitted reports the launch ceiling. It drives the engine-level provider
// read-only gate and the SPA's affordance list — NOT whether a given request may
// write (that is AuthorizeWrite, per request).
func (p *Policy) ArmingPermitted() bool { return p.armingPermitted }

// Environment reports the prod-discriminator seam (empty in v1). Reserved for a
// future production posture; exposed read-only so the value cannot be mutated after
// construction.
func (p *Policy) Environment() string { return p.environment }

// AuthorizeWrite returns nil only when the caller presents the process write token
// (writeCap) AND an explicit per-action confirm. It refuses with ErrReadOnly when
// arming is not permitted, when no write token was minted, or when writeCap does
// not match the process write token (constant-time) — a single, uniform error on
// every capability failure so a bearer-only caller can never probe WHICH condition
// failed (no oracle). confirmed is the X-Confirm header presence: an intent signal,
// never the boundary.
func (p *Policy) AuthorizeWrite(writeCap string, confirmed bool) error {
	if !p.armingPermitted || p.writeToken == "" ||
		subtle.ConstantTimeCompare([]byte(writeCap), []byte(p.writeToken)) != 1 {
		return provider.ErrReadOnly
	}
	if !confirmed {
		return provider.ErrNeedsConfirm
	}
	return nil
}
