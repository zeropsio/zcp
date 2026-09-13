package topology

// PublicAccessIntent is the user's persisted intent for whether a runtime is
// publicly reachable — orthogonal to the live observed state (docs/spec-
// workflows.md §8 O3/E9). "" is NOT a valid PublicAccessIntent value; callers
// default an empty/absent value to PublicAccessAuto at the I/O boundary
// (plan JSON, ServiceMeta accessor) rather than treating empty as a fifth
// state.
type PublicAccessIntent string

const (
	// PublicAccessAuto is the default: zcp auto-enables the subdomain once
	// (PA-1) the first time a listener exists, then never touches it again.
	PublicAccessAuto PublicAccessIntent = "auto"
	// PublicAccessSubdomain records an explicit `zerops_subdomain enable` —
	// the user opted in past the one-time auto-enable.
	PublicAccessSubdomain PublicAccessIntent = "subdomain"
	// PublicAccessDomain means a custom domain routes to this service
	// (PA-3) — derived only, never planned; domains always win.
	PublicAccessDomain PublicAccessIntent = "domain"
	// PublicAccessNone means the runtime is internal-only by intent — either
	// an explicit `zerops_subdomain disable`, or the user switched the
	// subdomain off after zcp's one-time auto-enable stamp (PA-2).
	PublicAccessNone PublicAccessIntent = "none"
)

// IsValid reports whether i is one of the four defined PublicAccessIntent
// values. "" is deliberately excluded — see the type doc-comment.
func (i PublicAccessIntent) IsValid() bool {
	switch i {
	case PublicAccessAuto, PublicAccessSubdomain, PublicAccessDomain, PublicAccessNone:
		return true
	default:
		return false
	}
}

// ParsePublicAccessIntent parses a string into a PublicAccessIntent, ok=false
// for "" or any value IsValid rejects. I/O-boundary callers that want the
// empty-string-means-auto default do that themselves; this parser stays a
// strict round-trip of the four defined values.
func ParsePublicAccessIntent(s string) (PublicAccessIntent, bool) {
	i := PublicAccessIntent(s)
	if !i.IsValid() {
		return "", false
	}
	return i, true
}

// SubdomainState is the live-observed state of a service's Zerops subdomain
// (`GetService.SubdomainAccess`, REST-authoritative, plus an in-flight
// enable process) — never persisted, always read fresh (§8 O3).
type SubdomainState string

const (
	SubdomainOn       SubdomainState = "on"
	SubdomainOff      SubdomainState = "off"
	SubdomainEnabling SubdomainState = "enabling"
)

// PublicAccessObserved is the live state O3 reads per call — never stored.
// Domains and Listener are read from other platform surfaces (the project
// routing list, topology.IsDeferredStart); Subdomain from the service DTO.
type PublicAccessObserved struct {
	Subdomain SubdomainState
	Domains   []string // custom domains routed to this service
	Listener  bool     // an HTTP listener exists now (false for a deferred-start dev runtime before the dev server runs)
}

// PublicAccessRecord is what ServiceMeta persists per hostname (E9): the
// user's intent plus the one-time auto-enable stamp. Everything else about
// public access is read live (O3).
type PublicAccessRecord struct {
	Intent PublicAccessIntent `json:"intent"`
	// SubdomainEnabledByZcpAt is the RFC3339 timestamp of zcp's one-time
	// auto-enable (PA-1). Once set, zcp never auto-enables again for this
	// hostname (PA-2).
	SubdomainEnabledByZcpAt string `json:"subdomainEnabledByZcpAt,omitempty"`
}

// ReconcilePublicAccess applies PA-2/PA-3 to a stored record given a fresh
// live observation. Pure — no I/O, no clock reads. Returns the reconciled
// record and whether it differs from rec (so callers can skip a write when
// nothing changed).
//
// Rules, in order:
//  1. Domains ≠ ∅ ⇒ Intent = domain (PA-3) — domains win over everything.
//  2. Stamped ∧ observed off ∧ Intent ∈ {auto, subdomain} ⇒ Intent = none
//     (PA-2 — the user switched it off after zcp's one-time auto-enable).
//  3. Intent == domain ∧ no domains ⇒ Intent = auto (the user removed the
//     domain; the stamp, if any, still blocks a second auto-enable).
//  4. Otherwise unchanged.
func ReconcilePublicAccess(rec PublicAccessRecord, obs PublicAccessObserved) (PublicAccessRecord, bool) {
	out := rec
	switch {
	case len(obs.Domains) > 0:
		out.Intent = PublicAccessDomain
	case rec.SubdomainEnabledByZcpAt != "" && obs.Subdomain == SubdomainOff &&
		(rec.Intent == PublicAccessAuto || rec.Intent == PublicAccessSubdomain):
		out.Intent = PublicAccessNone
	case rec.Intent == PublicAccessDomain && len(obs.Domains) == 0:
		out.Intent = PublicAccessAuto
	}
	return out, out != rec
}

// ShouldAutoEnableSubdomain is PA-1's meta half — whether zcp should perform
// its one-time auto-enable. The mode allow-list (which runtime classes are
// eligible at all) stays the caller's job; this only evaluates the
// record/observation state.
func ShouldAutoEnableSubdomain(rec PublicAccessRecord, obs PublicAccessObserved) bool {
	return rec.Intent == PublicAccessAuto &&
		rec.SubdomainEnabledByZcpAt == "" &&
		len(obs.Domains) == 0 &&
		obs.Subdomain == SubdomainOff &&
		obs.Listener
}

// DeriveAdoptedIntent is PA-6's adopt rule: derive the persisted intent from
// observed state alone (an adopted service has no prior recorded intent).
// Domains win; else an observed-on subdomain becomes subdomain; else auto.
func DeriveAdoptedIntent(obs PublicAccessObserved) PublicAccessIntent {
	if len(obs.Domains) > 0 {
		return PublicAccessDomain
	}
	if obs.Subdomain == SubdomainOn {
		return PublicAccessSubdomain
	}
	return PublicAccessAuto
}
