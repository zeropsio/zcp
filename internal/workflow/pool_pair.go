package workflow

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/zeropsio/zcp/internal/topology"
)

// A pair's dev half may be a dev pool (docs/spec-workflows.md §13, §8 E8).
// The pool migration retires a part's dev service once a lease of the part's
// pool works, and re-keys the pair onto the pool first, so the stage is never
// left under a dead key: one meta per pair still (E8), keyed by the pool,
// marked by DevPool. The same shape serves a part provisioned as a pool.
//
// Every reader that would do container work on a meta's Hostname — ssh,
// mount, deploy from it, probe it — asks DevIsPool first; pure readers read
// ServiceSnapshot.DevIsPool. The pool half is never a deploy source or target
// of the develop flow: the stage deploys from a held lease of the pool.

// DevPoolRef marks a pair whose dev half is a dev pool.
type DevPoolRef struct {
	// ServiceID is the pool service's id, as the caller read it by id when it
	// keyed the pair on the pool: a pool re-created under the same hostname is
	// another service and holds nothing zcp wrote to this one (its GIT_TOKEN
	// above all). "" when unknown.
	ServiceID string `json:"serviceId,omitempty"`
	// MigratedFrom is the dev service the pair was keyed by before it moved
	// onto the pool; nil for a part provisioned as a pool.
	MigratedFrom *MigratedDev `json:"migratedFrom,omitempty"`
}

// MigratedDev is the dev service a pair was keyed by before the move, with
// the facts about that service's own container the move took off the pair.
// They are kept only so an aborted retire puts them back (RevertPoolPair);
// nothing reads them as the pool's, and the hostname is no key — a service
// that later takes it is never this pair's dev half.
type MigratedDev struct {
	Hostname string `json:"hostname"`
	// PrimarySetupName is the dev service's zerops.yml setup.
	PrimarySetupName string `json:"primarySetupName,omitempty"`
	// Repo is the dev service's adopt-time baseline (GLC-7): which appVersion
	// ran on that service at adoption.
	Repo *topology.Repo `json:"repo,omitempty"`
	// ServesHTTP is the dev service's own serves-HTTP record; nil for none.
	ServesHTTP *bool `json:"servesHttp,omitempty"`
	// PublicAccess is the dev service's own public-access record, its PA-1
	// stamp included (the stamp names the dev's subdomain, not the pool's);
	// nil when it had none.
	PublicAccess *topology.PublicAccessRecord `json:"publicAccess,omitempty"`
	// CredentialFingerprint names the Mate credential the dev service held as
	// GIT_TOKEN (HQRepoRef.CredentialFingerprint); "" for none.
	CredentialFingerprint string `json:"credentialFingerprint,omitempty"`
}

// DevIsPool reports whether the pair's dev half — m.Hostname — is a dev pool.
func (m *ServiceMeta) DevIsPool() bool {
	return m != nil && m.DevPool != nil
}

// PoolPairMove names a re-key of a pair from its dev service onto its pool.
type PoolPairMove struct {
	// Dev is the dev service the pair is keyed by before the move.
	Dev string
	// Pool is the dev pool it is keyed by after.
	Pool string
	// PoolServiceID is the pool service's id, read by id (DevPoolRef.ServiceID).
	PoolServiceID string
	// PoolSetup is the zerops.yml setup the pool's image deploys with; "" while
	// unknown (the setup cascade resolves it, as for any empty setup).
	PoolSetup string
}

// MigratePairToPool re-keys the pair keyed by move.Dev onto move.Pool, inside
// one .services.lock: the pool-keyed file is written, then the dev-keyed one
// removed. It carries every pair-wide fact — the stages, the lifecycle stamps,
// the HQ record, the stage's own records, the dev's public-access intent
// (moved under the pool's key, its stamp cleared) — and sets aside under
// DevPool.MigratedFrom what was the dev service's own container's: its setup
// (the pool's replaces it), its adopt-time baseline, its serves-HTTP record
// and the fingerprint of the credential it held (the next HQ pass writes the
// pool's afresh).
//
// It refuses a pair it cannot move — none keyed by move.Dev, a simple or local
// mode, a variant-B pair, an unfinished bootstrap, a pair already keyed by a
// pool — and a pool hostname another pair names, and changes nothing then. A
// move already done is a no-op returning the pool-keyed pair as it is now; a
// move cut between its write and its removal is finished. The caller holds
// the pair locks of both hostnames (docs/spec-workflows.md §13).
func MigratePairToPool(stateDir string, move PoolPairMove) (*ServiceMeta, error) {
	if move.Dev == "" || move.Pool == "" || move.Dev == move.Pool {
		return nil, fmt.Errorf("migrate pair to pool: dev %q and pool %q must be two hostnames", move.Dev, move.Pool)
	}
	out, err := rekeyServiceMeta(stateDir, move.Dev, move.Pool, func(devMeta, poolMeta *ServiceMeta) (*ServiceMeta, error) {
		if poolMeta != nil {
			if poolMeta.migratedFrom(move.Dev) {
				return poolMeta, nil
			}
			return nil, fmt.Errorf("a pair is already keyed by %q", move.Pool)
		}
		if devMeta == nil {
			return nil, fmt.Errorf("no pair is keyed by %q: %w", move.Dev, ErrServiceMetaNotFound)
		}
		if owner, err := otherPairNaming(stateDir, move.Pool, devMeta.Hostname); err != nil || owner != "" {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("the pair keyed by %q names %q", owner, move.Pool)
		}
		return devMeta.keyedOnPool(move)
	})
	if err != nil {
		return nil, fmt.Errorf("migrate pair %s to pool %s: %w", move.Dev, move.Pool, err)
	}
	return out, nil
}

// RevertPoolPair is MigratePairToPool's inverse, for a retire that aborts:
// the pool-keyed file is renamed back to move.Dev and the inverse applied to
// what it holds now — so whatever was written since the move (HQ's change,
// the pair's stamps) stays, and only the dev service's own facts come back
// from DevPool.MigratedFrom; nothing is restored from a snapshot. A pair
// already keyed by move.Dev is returned as it is. It refuses a pool-keyed
// pair that never was move.Dev's, and changes nothing then.
func RevertPoolPair(stateDir string, move PoolPairMove) (*ServiceMeta, error) {
	if move.Dev == "" || move.Pool == "" || move.Dev == move.Pool {
		return nil, fmt.Errorf("revert pool pair: dev %q and pool %q must be two hostnames", move.Dev, move.Pool)
	}
	out, err := rekeyServiceMeta(stateDir, move.Pool, move.Dev, func(poolMeta, devMeta *ServiceMeta) (*ServiceMeta, error) {
		if poolMeta == nil {
			if devMeta == nil {
				return nil, fmt.Errorf("no pair is keyed by %q or %q: %w", move.Pool, move.Dev, ErrServiceMetaNotFound)
			}
			return devMeta, nil
		}
		if !poolMeta.migratedFrom(move.Dev) {
			return nil, poolMeta.notMovedFrom(move.Dev)
		}
		return poolMeta.keyedOnDev()
	})
	if err != nil {
		return nil, fmt.Errorf("revert pool pair %s to %s: %w", move.Pool, move.Dev, err)
	}
	return out, nil
}

// migratedFrom reports whether m is a pool-keyed pair moved from dev.
func (m *ServiceMeta) migratedFrom(dev string) bool {
	return m.DevIsPool() && m.DevPool.MigratedFrom != nil && m.DevPool.MigratedFrom.Hostname == dev
}

// notMovedFrom says why m, the pair keyed by a pool's hostname, is not one
// moved from dev.
func (m *ServiceMeta) notMovedFrom(dev string) error {
	switch {
	case !m.DevIsPool():
		return fmt.Errorf("the pair keyed by %q is not a pool pair", m.Hostname)
	case m.DevPool.MigratedFrom == nil:
		return fmt.Errorf("the pair keyed by %q was never keyed by %q: it was provisioned as a pool", m.Hostname, dev)
	default:
		return fmt.Errorf("the pair keyed by %q was moved from %q, not %q", m.Hostname, m.DevPool.MigratedFrom.Hostname, dev)
	}
}

// otherPairNaming is the key of a pair other than self's that names host, ""
// for none.
func otherPairNaming(stateDir, host, self string) (string, error) {
	metas, err := ListServiceMetas(stateDir)
	if err != nil {
		return "", err
	}
	for _, m := range metas {
		if m != nil && m.Hostname != self && slices.Contains(m.Hostnames(), host) {
			return m.Hostname, nil
		}
	}
	return "", nil
}

// errNotMovable is the class of a pair the migration cannot key on a pool.
var errNotMovable = errors.New("the pair cannot move onto a pool")

// keyedOnPool is m re-keyed onto move.Pool (design 2): a copy, m untouched.
func (m *ServiceMeta) keyedOnPool(move PoolPairMove) (*ServiceMeta, error) {
	switch {
	case m.DevIsPool():
		return nil, fmt.Errorf("%w: %q is already keyed by a pool", errNotMovable, m.Hostname)
	case m.Mode != topology.PlanModeStandard && m.Mode != topology.PlanModeDev:
		return nil, fmt.Errorf("%w: %q is in %s mode; only a dev service in standard or dev mode moves", errNotMovable, m.Hostname, m.Mode)
	case m.CodeVolume != nil:
		return nil, fmt.Errorf("%w: %q keeps its code on a code volume (variant B), which does not move yet", errNotMovable, m.Hostname)
	case !m.IsComplete():
		return nil, fmt.Errorf("%w: %q has not finished its bootstrap", errNotMovable, m.Hostname)
	}
	if _, ok := m.StageFor(move.Pool); ok {
		return nil, fmt.Errorf("%w: %q is a stage of the pair", errNotMovable, move.Pool)
	}
	dev := m.Hostname
	out := m.clone()
	from := &MigratedDev{
		Hostname:         dev,
		PrimarySetupName: m.PrimarySetupName,
		Repo:             m.Repo,
		ServesHTTP:       m.ServesHTTPFor(dev),
	}
	if rec, ok := m.PublicAccess[dev]; ok {
		from.PublicAccess = &rec
	}
	if m.HQ != nil {
		from.CredentialFingerprint = m.HQ.CredentialFingerprint
		out.HQ.CredentialFingerprint = ""
	}

	out.Hostname = move.Pool
	out.PrimarySetupName = move.PoolSetup
	out.Repo = nil
	delete(out.ServesHTTPByHost, dev)
	delete(out.PublicAccess, dev)
	if from.PublicAccess != nil {
		out.SetPublicAccess(move.Pool, topology.PublicAccessRecord{Intent: from.PublicAccess.Intent})
	}
	out.DevPool = &DevPoolRef{ServiceID: move.PoolServiceID, MigratedFrom: from}
	out.dropEmptyMaps()
	return out, nil
}

// keyedOnDev is the inverse of keyedOnPool applied to m as it is now: a copy,
// m untouched. The dev's record takes the pool record's current intent with
// the dev's own stamp.
func (m *ServiceMeta) keyedOnDev() (*ServiceMeta, error) {
	if !m.DevIsPool() || m.DevPool.MigratedFrom == nil {
		return nil, fmt.Errorf("the pair keyed by %q was not moved onto it from a dev service", m.Hostname)
	}
	pool, from := m.Hostname, m.DevPool.MigratedFrom
	out := m.clone()
	out.Hostname = from.Hostname
	out.PrimarySetupName = from.PrimarySetupName
	out.Repo = from.Repo
	delete(out.ServesHTTPByHost, pool)
	if from.ServesHTTP != nil {
		out.SetServesHTTP(from.Hostname, *from.ServesHTTP)
	}
	cur, had := out.PublicAccess[pool]
	delete(out.PublicAccess, pool)
	if had || from.PublicAccess != nil {
		rec := topology.PublicAccessRecord{Intent: cur.Intent}
		if from.PublicAccess != nil {
			if !had {
				rec.Intent = from.PublicAccess.Intent
			}
			rec.SubdomainEnabledByZcpAt = from.PublicAccess.SubdomainEnabledByZcpAt
		}
		out.SetPublicAccess(from.Hostname, rec)
	}
	if out.HQ != nil {
		out.HQ.CredentialFingerprint = from.CredentialFingerprint
	}
	out.DevPool = nil
	out.dropEmptyMaps()
	return out, nil
}

// clone is a copy of m that shares no slice, map or record with it.
func (m *ServiceMeta) clone() *ServiceMeta {
	out := *m
	out.Stages = slices.Clone(m.Stages)
	out.ServesHTTPByHost = maps.Clone(m.ServesHTTPByHost)
	out.PublicAccess = maps.Clone(m.PublicAccess)
	out.ProdLaunches = slices.Clone(m.ProdLaunches)
	if m.HQ != nil {
		hq := *m.HQ
		out.HQ = &hq
	}
	if m.DevPool != nil {
		dp := *m.DevPool
		out.DevPool = &dp
	}
	return &out
}

// dropEmptyMaps leaves an emptied per-host map absent, as a meta that never
// recorded one.
func (m *ServiceMeta) dropEmptyMaps() {
	if len(m.ServesHTTPByHost) == 0 {
		m.ServesHTTPByHost = nil
	}
	if len(m.PublicAccess) == 0 {
		m.PublicAccess = nil
	}
}
