// Package console is the Data Console engine: the Host seam, the type→Family
// classification, and the registry that lazily builds one provider per managed
// service. It imports only its own provider sub-package + stdlib — never zcp
// core — so it lifts to its own repo with a `git mv`. The zcp adapter implements
// Host (internal/dataconsole/zcpadapter); when extracted, only the adapter is
// rewritten and this subtree moves byte-for-byte.
package console

import (
	"context"
	"fmt"
	"sync"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
)

// Host is the only seam to whatever hosts the engine. The implementation
// (zcpadapter) forwards the neutral service type and a typed connection
// descriptor; the engine owns the type→Family classification.
type Host interface {
	Project(ctx context.Context) (ProjectRef, error)
	ManagedServices(ctx context.Context) ([]ManagedServiceRef, error)
	ConnectionInfo(ctx context.Context, serviceID string) (ConnectionInfo, error)
}

// ProjectRef identifies the project, neutrally.
type ProjectRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ManagedServiceRef is a discovered managed service, carrying the neutral
// type string — the engine classifies it, the adapter does not.
type ManagedServiceRef struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Type     string `json:"type"`
	Status   string `json:"status"`
}

// ConnectionInfo is the resolved, neutral connection contract. Descriptor is a
// family-specific typed payload with connection facts only — no host env-key
// names, no templated connectionString, and never serialized to a DTO.
type ConnectionInfo struct {
	Type       string                        `json:"-"`
	Family     provider.Family               `json:"-"`
	Descriptor provider.ConnectionDescriptor `json:"-"` // secret — never serialized to a DTO
}

// Factory builds a provider for a service from its connection info + the write
// policy. The cmd composition root registers one factory per supported family.
// The policy is immutable after construction, shared by pointer; a factory reads
// only ArmingPermitted() — the launch ceiling that fixes each provider's
// engine-level read-only posture at build time. The RUNTIME write gate is the
// per-request write-token check in the route middleware, NOT the provider layer.
type Factory func(ci ConnectionInfo, policy *safety.Policy) (provider.Provider, error)

// ServiceView is the engine's classified, UI-facing view of one service. It
// carries no secret. Actions is the connection-free operation contract the SPA
// renders affordances against (single owner: provider.ServiceActions).
type ServiceView struct {
	Hostname string            `json:"hostname"`
	Type     string            `json:"type"`
	Family   provider.Family   `json:"family"`
	Support  provider.Support  `json:"support"`
	Actions  []provider.Action `json:"actions"`
	Status   string            `json:"status"`
	ID       string            `json:"-"`
}

// Engine discovers + classifies managed services and lazily builds a provider
// for each supported one.
type Engine struct {
	host      Host
	policy    *safety.Policy
	factories map[provider.Family]Factory

	mu       sync.Mutex
	services []ServiceView
	built    map[string]provider.Provider // hostname -> provider
}

// NewEngine wires the engine; factories registers one builder per family the
// running binary supports (S1 registers object; S3/S4 add tabular/kv).
func NewEngine(host Host, policy *safety.Policy, factories map[provider.Family]Factory) *Engine {
	return &Engine{
		host:      host,
		policy:    policy,
		factories: factories,
		built:     map[string]provider.Provider{},
	}
}

// Project returns the host's project identity.
func (e *Engine) Project(ctx context.Context) (ProjectRef, error) { return e.host.Project(ctx) }

// Refresh re-discovers + re-classifies the managed services (single owner: the
// live host discovery, never a hardcoded type list).
func (e *Engine) Refresh(ctx context.Context) error {
	refs, err := e.host.ManagedServices(ctx)
	if err != nil {
		return fmt.Errorf("discover managed services: %w", err)
	}
	views := make([]ServiceView, 0, len(refs))
	for _, r := range refs {
		fam := provider.Classify(r.Type)
		sup := provider.SupportFor(r.Type)
		// A supported/view-only family with no registered factory in this binary
		// is downgraded to "not yet" so the UI never promises an absent provider.
		if sup == provider.SupportFull || sup == provider.SupportViewOnly {
			if _, ok := e.factories[fam]; !ok {
				sup = provider.SupportNotYet
			}
		}
		views = append(views, ServiceView{
			Hostname: r.Hostname, Type: r.Type, Family: fam,
			Support: sup, Actions: provider.ServiceActions(fam, sup, e.policy.ArmingPermitted()),
			Status: r.Status, ID: r.ID,
		})
	}
	e.mu.Lock()
	e.services = views
	// Drop any built providers no longer present.
	for host, p := range e.built {
		if !containsHost(views, host) {
			_ = p.Close()
			delete(e.built, host)
		}
	}
	e.mu.Unlock()
	return nil
}

// Services returns the classified view list (secret-free).
func (e *Engine) Services() []ServiceView {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]ServiceView, len(e.services))
	copy(out, e.services)
	return out
}

// Policy exposes the write posture (for handlers). It is the shared, immutable
// instance the mutating-route middleware calls AuthorizeWrite on, per request.
func (e *Engine) Policy() *safety.Policy { return e.policy }

// ProviderFor lazily builds + caches the provider for a hostname. Returns
// ErrUnsupported if the service has no registered factory.
func (e *Engine) ProviderFor(ctx context.Context, hostname string) (provider.Provider, ServiceView, error) {
	e.mu.Lock()
	view, ok := findView(e.services, hostname)
	if !ok {
		e.mu.Unlock()
		return nil, ServiceView{}, fmt.Errorf("service %q: %w", hostname, provider.ErrNotFound)
	}
	if p, ok := e.built[hostname]; ok {
		e.mu.Unlock()
		return p, view, nil
	}
	factory, ok := e.factories[view.Family]
	e.mu.Unlock()
	if !ok {
		return nil, view, fmt.Errorf("service %q (%s): %w", hostname, view.Family, provider.ErrUnsupported)
	}

	ci, err := e.host.ConnectionInfo(ctx, view.ID)
	if err != nil {
		return nil, view, fmt.Errorf("connection info for %q: %w", hostname, err)
	}
	p, err := factory(ci, e.policy)
	if err != nil {
		return nil, view, fmt.Errorf("build provider for %q: %w", hostname, err)
	}
	// Preflight: prove reachable + authorized before caching. A provider built
	// while the VPN is down (or with a dropped connection) is NOT cached, so a
	// later retry after `zcli vpn up` rebuilds it cleanly — and the classified
	// error (ErrUnreachable vs ErrUpstream) drives an honest UI gate, never a
	// VPN hint on an auth failure.
	if herr := p.Health(ctx); herr != nil {
		_ = p.Close()
		return nil, view, fmt.Errorf("preflight %q: %w", hostname, herr)
	}
	e.mu.Lock()
	e.built[hostname] = p
	e.mu.Unlock()
	return p, view, nil
}

// Close releases every built provider.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range e.built {
		_ = p.Close()
	}
	e.built = map[string]provider.Provider{}
}

func containsHost(vs []ServiceView, host string) bool {
	_, ok := findView(vs, host)
	return ok
}

func findView(vs []ServiceView, host string) (ServiceView, bool) {
	for _, v := range vs {
		if v.Hostname == host {
			return v, true
		}
	}
	return ServiceView{}, false
}
