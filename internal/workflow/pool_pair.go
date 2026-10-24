package workflow

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

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
	// Parts are the pools a dev service serving several parts was split
	// into, one per part, in the order of the dev pair's stages; empty for a
	// single part. Every part's pair carries the same list: a split reverts
	// only as a whole (RevertPoolPairs).
	Parts []string `json:"parts,omitempty"`
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
// and the fingerprint of the credential it held (the next HQ pass renews the
// pool, taking over DP-16's per-pool record of it when that record names
// DevPool.ServiceID).
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
		if parts := poolMeta.DevPool.MigratedFrom.Parts; len(parts) > 1 {
			return nil, fmt.Errorf("the pair keyed by %q is one part of %q, split into %s: the split reverts as a whole", move.Pool, move.Dev, strings.Join(parts, ", "))
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

// otherPairNaming is the key of a pair that names host, other than the pairs
// keyed by selves; "" for none.
func otherPairNaming(stateDir, host string, selves ...string) (string, error) {
	metas, err := ListServiceMetas(stateDir)
	if err != nil {
		return "", err
	}
	for _, m := range metas {
		if m != nil && !slices.Contains(selves, m.Hostname) && slices.Contains(m.Hostnames(), host) {
			return m.Hostname, nil
		}
	}
	return "", nil
}

// errNotMovable is the class of a pair the migration cannot key on a pool.
var errNotMovable = errors.New("the pair cannot move onto a pool")

// keyedOnPool is m re-keyed onto move.Pool (design 2): a copy, m untouched.
// A dev service serving several parts never moves onto one pool: each part
// takes a pool of its own (partOnPool).
func (m *ServiceMeta) keyedOnPool(move PoolPairMove) (*ServiceMeta, error) {
	if err := m.movable(move.Pool); err != nil {
		return nil, err
	}
	if len(m.Stages) > 1 {
		return nil, fmt.Errorf("%w: %q serves several parts (stages %s); each part moves onto a pool of its own", errNotMovable, m.Hostname, strings.Join(m.StageHostnames(), ", "))
	}
	return m.onPool(move), nil
}

// movable says why m cannot move onto the pools named, nil when it can.
func (m *ServiceMeta) movable(pools ...string) error {
	switch {
	case m.DevIsPool():
		return fmt.Errorf("%w: %q is already keyed by a pool", errNotMovable, m.Hostname)
	case m.Mode != topology.PlanModeStandard && m.Mode != topology.PlanModeDev:
		return fmt.Errorf("%w: %q is in %s mode; only a dev service in standard or dev mode moves", errNotMovable, m.Hostname, m.Mode)
	case m.CodeVolume != nil:
		return fmt.Errorf("%w: %q keeps its code on a code volume (variant B), which does not move yet", errNotMovable, m.Hostname)
	case !m.IsComplete():
		return fmt.Errorf("%w: %q has not finished its bootstrap", errNotMovable, m.Hostname)
	}
	for _, pool := range pools {
		if _, ok := m.StageFor(pool); ok {
			return fmt.Errorf("%w: %q is a stage of the pair", errNotMovable, pool)
		}
	}
	return nil
}

// onPool is m keyed by move.Pool, carrying every pair-wide fact and setting
// the dev service's own aside: a copy, m untouched.
func (m *ServiceMeta) onPool(move PoolPairMove) *ServiceMeta {
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
	return out
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
		if dp.MigratedFrom != nil {
			from := *dp.MigratedFrom
			from.Parts = slices.Clone(from.Parts)
			dp.MigratedFrom = &from
		}
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

// PoolPartMove names one part of a dev service serving several moving onto
// its own pool: the part's stage, and the pool keying the part's pair after.
type PoolPartMove struct {
	Stage         string
	Pool          string
	PoolServiceID string
	PoolSetup     string
}

func (p PoolPartMove) move(dev string) PoolPairMove {
	return PoolPairMove{Dev: dev, Pool: p.Pool, PoolServiceID: p.PoolServiceID, PoolSetup: p.PoolSetup}
}

// partPools are the parts' pools, in the parts' order.
func partPools(parts []PoolPartMove) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p.Pool)
	}
	return out
}

// checkPartMoves refuses a split that names no part, a part without its stage
// or pool, two parts of one stage or one pool, or a pool named like the dev.
func checkPartMoves(dev string, parts []PoolPartMove) error {
	if dev == "" || len(parts) == 0 {
		return fmt.Errorf("dev %q and at least one part are needed", dev)
	}
	stages, pools := map[string]bool{}, map[string]bool{}
	for _, p := range parts {
		switch {
		case p.Stage == "" || p.Pool == "":
			return fmt.Errorf("a part needs its stage and its pool: %+v", p)
		case p.Pool == dev:
			return fmt.Errorf("the pool %q is the dev service's hostname", p.Pool)
		case stages[p.Stage]:
			return fmt.Errorf("the stage %q is named by two parts", p.Stage)
		case pools[p.Pool]:
			return fmt.Errorf("the pool %q is named by two parts: each part moves onto a pool of its own", p.Pool)
		}
		stages[p.Stage], pools[p.Pool] = true, true
	}
	return nil
}

// splitFrom reports whether m is the pair a split of dev keyed by its part of
// stage.
func (m *ServiceMeta) splitFrom(dev, stage string) bool {
	return m.migratedFrom(dev) && len(m.Stages) == 1 && m.Stages[0].Hostname == stage
}

// MigratePairToPools splits the pair keyed by dev — one dev service serving
// several parts — into one pool-keyed pair per part, inside one
// .services.lock: every part's file is written, then the dev-keyed one
// removed, so a cut leaves the dev's file and the next call finishes the
// split. Each part's pair is the dev's re-keyed onto the part's pool as
// MigratePairToPool keys a single part's (every pair-wide fact carried, the
// dev's own facts set aside under DevPool.MigratedFrom, which also names
// every part's pool), holding only its own stage and that stage's records.
//
// It refuses — changing nothing — what MigratePairToPool refuses, a stage of
// the pair no part names (it would stay under the dead dev's key), a part
// naming no stage of the pair, and a pool another pair keys or names. A part
// already written by a cut split keeps what its file holds; a split already
// done is a no-op returning the parts' pairs as they are now. The caller holds
// the pair locks of the dev and of every pool (docs/spec-workflows.md §13).
func MigratePairToPools(stateDir, dev string, parts []PoolPartMove) ([]*ServiceMeta, error) {
	if err := checkPartMoves(dev, parts); err != nil {
		return nil, fmt.Errorf("split pair %s onto pools: %w", dev, err)
	}
	pools := partPools(parts)
	out, err := rekeyServiceMetas(stateDir, append([]string{dev}, pools...), func(read map[string]*ServiceMeta) ([]*ServiceMeta, error) {
		devMeta, partMetas, err := readSplit(read, dev, parts)
		if err != nil {
			return nil, err
		}
		if devMeta == nil {
			return finishedSplit(dev, parts, partMetas)
		}
		if err := devMeta.movable(pools...); err != nil {
			return nil, err
		}
		if err := devMeta.coveredBy(parts); err != nil {
			return nil, err
		}
		for _, p := range parts {
			owner, err := otherPairNaming(stateDir, p.Pool, append([]string{dev}, pools...)...)
			if err != nil {
				return nil, err
			}
			if owner != "" {
				return nil, fmt.Errorf("the pair keyed by %q names %q", owner, p.Pool)
			}
		}
		next := make([]*ServiceMeta, len(parts))
		for i, p := range parts {
			next[i] = partMetas[i]
			if next[i] == nil {
				next[i] = devMeta.partOnPool(dev, p, pools)
			}
		}
		return next, nil
	})
	if err != nil {
		return nil, fmt.Errorf("split pair %s onto pools %s: %w", dev, strings.Join(pools, ", "), err)
	}
	return out, nil
}

// readSplit reads the pair keyed by dev and each part's pool-keyed pair as
// they are now (any may be absent), refusing a pool keyed by a pair that is
// not this split's part of it.
func readSplit(read map[string]*ServiceMeta, dev string, parts []PoolPartMove) (*ServiceMeta, []*ServiceMeta, error) {
	devMeta := read[dev]
	if devMeta != nil && devMeta.DevIsPool() {
		return nil, nil, fmt.Errorf("%w: %q is already keyed by a pool", errNotMovable, dev)
	}
	partMetas := make([]*ServiceMeta, len(parts))
	for i, p := range parts {
		m := read[p.Pool]
		if m != nil && !m.splitFrom(dev, p.Stage) {
			return nil, nil, fmt.Errorf("a pair is already keyed by %q", p.Pool)
		}
		partMetas[i] = m
	}
	return devMeta, partMetas, nil
}

// finishedSplit answers a split whose dev file is gone: every part's pair as
// it is, or why the split cannot be finished.
func finishedSplit(dev string, parts []PoolPartMove, partMetas []*ServiceMeta) ([]*ServiceMeta, error) {
	var missing []string
	for i, m := range partMetas {
		if m == nil {
			missing = append(missing, parts[i].Pool)
		}
	}
	switch {
	case len(missing) == len(parts):
		return nil, fmt.Errorf("no pair is keyed by %q: %w", dev, ErrServiceMetaNotFound)
	case len(missing) > 0:
		return nil, fmt.Errorf("no pair is keyed by %q any more, and %s hold no part of it: nothing is left to carry those parts from", dev, strings.Join(missing, ", "))
	}
	return partMetas, nil
}

// coveredBy says which stage of m no part names, or which part names no stage
// of m; nil when the parts are exactly m's stages.
func (m *ServiceMeta) coveredBy(parts []PoolPartMove) error {
	named := map[string]bool{}
	for _, p := range parts {
		if _, ok := m.StageFor(p.Stage); !ok {
			return fmt.Errorf("%w: %q is no stage of the pair keyed by %q (stages %s)", errNotMovable, p.Stage, m.Hostname, strings.Join(m.StageHostnames(), ", "))
		}
		named[p.Stage] = true
	}
	for _, stage := range m.StageHostnames() {
		if !named[stage] {
			return fmt.Errorf("%w: the part of stage %q has no pool, and it would stay under the dev service's key", errNotMovable, stage)
		}
	}
	return nil
}

// partOnPool is m's part of p.Stage keyed by p.Pool: m on the pool, holding
// only that stage and that stage's records beside the pool's own; a copy, m
// untouched. pools are every part's pool, which the marker names.
func (m *ServiceMeta) partOnPool(dev string, p PoolPartMove, pools []string) *ServiceMeta {
	out := m.onPool(p.move(dev))
	stage, _ := m.StageFor(p.Stage)
	out.Stages = []topology.StageRef{stage}
	for h := range out.ServesHTTPByHost {
		if h != p.Stage {
			delete(out.ServesHTTPByHost, h)
		}
	}
	for h := range out.PublicAccess {
		if h != p.Stage && h != p.Pool {
			delete(out.PublicAccess, h)
		}
	}
	out.DevPool.MigratedFrom.Parts = slices.Clone(pools)
	out.dropEmptyMaps()
	return out
}

// RevertPoolPairs is MigratePairToPools's inverse, for a retire that aborts:
// inside one .services.lock the parts' pairs merge back into the pair keyed
// by dev, applied to what their files hold now — the stages in the parts'
// order, each stage's records from its part's file, the pair-wide facts from
// the first part's, the dev's own facts back from DevPool.MigratedFrom — and
// the parts' files are removed; nothing is restored from a snapshot. A split
// cut before the dev's file went left copies of it under some pools: those
// are removed and the dev's pair returned as it is. It refuses a part's pool
// keyed by a pair that is not this split's, or a part's pair missing while the
// dev's is gone, and changes nothing then.
func RevertPoolPairs(stateDir, dev string, parts []PoolPartMove) (*ServiceMeta, error) {
	if err := checkPartMoves(dev, parts); err != nil {
		return nil, fmt.Errorf("revert the split of %s: %w", dev, err)
	}
	out, err := rekeyServiceMetas(stateDir, append([]string{dev}, partPools(parts)...), func(read map[string]*ServiceMeta) ([]*ServiceMeta, error) {
		devMeta, partMetas, err := readSplit(read, dev, parts)
		if err != nil {
			return nil, err
		}
		if devMeta != nil {
			// A split cut before the dev's file went: the parts' files are
			// copies of it, and go.
			return []*ServiceMeta{devMeta}, nil
		}
		if _, err := finishedSplit(dev, parts, partMetas); err != nil {
			return nil, err
		}
		merged, err := mergeParts(partMetas)
		if err != nil {
			return nil, err
		}
		return []*ServiceMeta{merged}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("revert the split of %s from %s: %w", dev, strings.Join(partPools(parts), ", "), err)
	}
	return out[0], nil
}

// mergeParts is the pair keyed by the dev the parts' pairs were split from:
// the first part's re-keyed onto the dev, then every other part's stage and
// that stage's records.
func mergeParts(partMetas []*ServiceMeta) (*ServiceMeta, error) {
	out, err := partMetas[0].keyedOnDev()
	if err != nil {
		return nil, err
	}
	for _, m := range partMetas[1:] {
		for _, stage := range m.Stages {
			out.Stages = append(out.Stages, stage)
			if serves := m.ServesHTTPFor(stage.Hostname); serves != nil {
				out.SetServesHTTP(stage.Hostname, *serves)
			}
			if rec, ok := m.PublicAccess[stage.Hostname]; ok {
				out.SetPublicAccess(stage.Hostname, rec)
			}
		}
	}
	return out, nil
}
