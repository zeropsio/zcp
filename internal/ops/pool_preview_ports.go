package ops

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// A lease of a dev pool answers its preview on previewPortBase + its
// container's stable number, 1 … maxContainers, and a per-port subdomain
// routes only to a port the pool declares with httpSupport: true
// (docs/spec-workflows.md §8 DP-19, §13 PM-11). The ports are the run.ports
// of the pool's deployed setup and previewPortBase lives in the repository's
// declaration; zcp writes neither. So a raise of the pool's maxContainers is
// where its limit outgrows its preview ports — Kara's run, 2026-10-25: hqpool
// declared 3001-3003, a helper raised it from 3 to 4, and container 4's
// preview on 3004 answered 502 — and Scale checks it there, before it
// mutates (spec-workflows §8 PS-17). A pool can be past its ports already —
// raised before that check, or outside zcp — and a scale that does not raise
// it, or a lease status of it, says so instead (PS-17a). Both read the one
// shortfall (readPoolPreviewShortfall), so the refusal and the notice never
// disagree.

// poolRaiseNote is what a full pool's offer to raise its maximum adds: a
// raise is the person's call, and each container it adds needs its preview
// port first.
const poolRaiseNote = "; a raise is an infrastructure change, so ask the person first, and each container it adds needs its preview port declared in the pool's run.ports first"

// poolPreviewBlock is the run of consecutive ports the pool declares as HTTP
// (isHTTPScheme: the scheme is set at deploy from the port's httpSupport,
// unlike the routing flag, which follows the subdomain) that its leases'
// previews answer on: the longest such run. ok is false when the pool
// declares no HTTP port, or two runs of that length — zcp cannot tell which
// is the preview block.
//
// It cannot tell an HTTP port right next to the block that is no preview
// port from a preview port: a dev server's own 3000 declared beside previews
// 3001-3003 reads as one block 3000-3003 of four, so a raise to 4 passes
// unrefused and unnoticed, and the ports named past the block are one lower
// than the ones the leases answer on. previewPortBase, which would tell them
// apart, lives in the repository's .zcp/dev-servers.json, which neither a
// scale nor a lease status reads.
func poolPreviewBlock(ports []platform.Port) (first, last int, ok bool) {
	declared := map[int]bool{}
	for _, p := range ports {
		if isHTTPScheme(p) {
			declared[p.Port] = true
		}
	}
	best, ties := 0, 0
	for p := range declared {
		if declared[p-1] {
			continue
		}
		end := p
		for declared[end+1] {
			end++
		}
		switch n := end - p + 1; {
		case n > best:
			best, ties, first, last = n, 1, p, end
		case n == best:
			ties++
		}
	}
	return first, last, best > 0 && ties == 1
}

// poolLeasedByZcp reports whether zcp holds a lease record of the pool — the
// one sign that the pool is a part's dev pool, whose HTTP ports are its
// leases' previews: zcp leases no pool an app uses as infrastructure.
func poolLeasedByZcp(stateDir string, pool *platform.ServiceStack) bool {
	records, _ := readLeaseRecords(stateDir)
	for _, r := range records {
		if r.PoolHostname == pool.Name && r.PoolServiceID == pool.ID {
			return true
		}
	}
	return false
}

// poolMaxAfter is the maxContainers a scale leaves the pool at: the one it
// sets, else the one the pool runs with (current, the direct read); 0 when
// neither is known.
func poolMaxAfter(current *platform.ServiceStack, params ScaleParams) int {
	if params.MaxContainers != nil {
		return *params.MaxContainers
	}
	if eff := effectiveAutoscaling(current); eff != nil {
		return int(eff.HorizontalMaxCount)
	}
	return 0
}

// poolRaises reports whether params raise the pool's maxContainers above the
// one it runs with (current, the direct read); a maximum it runs with that is
// not known counts as raised. The same maximum again and a lowering are no
// raise.
func poolRaises(current *platform.ServiceStack, params ScaleParams) bool {
	if params.MaxContainers == nil {
		return false
	}
	eff := effectiveAutoscaling(current)
	return eff == nil || eff.HorizontalMaxCount <= 0 || *params.MaxContainers > int(eff.HorizontalMaxCount)
}

// poolPreviewShortfall is how the preview ports of a dev pool fall short of a
// maxContainers: the block it opens and each container past it with the
// preview port it would answer on, which the pool does not open.
type poolPreviewShortfall struct {
	pool          string
	maxContainers int
	first, last   int      // the preview block (poolPreviewBlock)
	containers    []string // the containers past it, by stable number
	ports         []string // their preview ports: the block's first port − 1 + the number
}

// readPoolPreviewShortfall is the one read of a pool's preview ports against
// maxContainers that both the raise refusal and the notice take, from the
// pool as the direct read has it. ok is false when the block covers
// maxContainers, and when zcp cannot read the ports as one block (no HTTP
// port, or two longest runs of one length): zcp names a count only when it
// knows it. The caller decides the pool is a dev pool (poolLeasedByZcp).
func readPoolPreviewShortfall(pool *platform.ServiceStack, maxContainers int) (poolPreviewShortfall, bool) {
	first, last, ok := poolPreviewBlock(pool.Ports)
	opens := last - first + 1
	if !ok || maxContainers <= opens {
		return poolPreviewShortfall{}, false
	}
	s := poolPreviewShortfall{pool: pool.Name, maxContainers: maxContainers, first: first, last: last,
		containers: make([]string, 0, maxContainers-opens), ports: make([]string, 0, maxContainers-opens)}
	for n := opens + 1; n <= maxContainers; n++ {
		s.containers = append(s.containers, strconv.Itoa(n))
		s.ports = append(s.ports, strconv.Itoa(first-1+n))
	}
	return s, true
}

// opens is how many preview ports the block holds: one per container.
func (s poolPreviewShortfall) opens() int { return s.last - s.first + 1 }

// block is the preview block as a port or a range of ports.
func (s poolPreviewShortfall) block() string {
	if s.last > s.first {
		return strconv.Itoa(s.first) + "-" + strconv.Itoa(s.last)
	}
	return strconv.Itoa(s.first)
}

// raiseError refuses the raise to s.maxContainers before it is sent (PS-17):
// it names the containers past the block, their ports and that their previews
// would answer 502, that a raise is the person's call, and the run.ports
// change and pool deploy that come first.
func (s poolPreviewShortfall) raiseError() error {
	who, portNoun, whose := "container "+s.containers[0], "port "+s.ports[0], "its preview"
	if len(s.ports) > 1 {
		who, portNoun, whose = "containers "+joinAnd(s.containers), "ports "+joinAnd(s.ports), "their previews"
	}
	msg := fmt.Sprintf("Pool '%s' opens preview ports %s, one for each of its first %d containers: a lease answers its preview on previewPortBase + its container's stable number (1 … maxContainers), and a per-port subdomain routes only to a port the pool declares with httpSupport: true. "+
		"At maxContainers %d, %s would answer on %s, which the pool does not open, so %s would answer 502",
		s.pool, s.block(), s.opens(), s.maxContainers, who, portNoun, whose)
	suggestion := fmt.Sprintf("Raising a pool's maxContainers is an infrastructure change — the pool may then run more containers, each at the pool's minimum RAM and CPU or more — so ask the person before you raise it. "+
		"To raise it: declare %s in run.ports of the pool's setup in zerops.yaml, each `- port: <n>` with `httpSupport: true`; commit it; deploy the pool from that commit "+
		"(zerops_deploy sourceService=\"<a lease or dev service holding the commit>\" targetService=%q sha=\"<commit>\"); then raise it again (zerops_scale serviceHostname=%q maxContainers=%d). "+
		"Without a raise, releasing a lease you no longer need frees a container (zerops_lease action=status lists them; ask the person before releasing one this conversation did not take).",
		portNoun, s.pool, s.pool, s.maxContainers)
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing, msg, suggestion)
}

// notice says that the pool, at s.maxContainers, allows more containers than
// it opens preview ports for (PS-17a): how many containers against how many
// ports, which containers have no preview port, and the two fixes — declare
// the ports and deploy the pool, or keep maxContainers at the port count.
func (s poolPreviewShortfall) notice() string {
	opens := s.opens()
	has := fmt.Sprintf("only %d preview ports (%s), one for each of its first %d containers", opens, s.block(), opens)
	if opens == 1 {
		has = fmt.Sprintf("only 1 preview port (%s), for its first container", s.block())
	}
	missing := fmt.Sprintf("Container %s has no preview port: the pool does not open %s, so a lease landing there has no preview (its preview address answers 502)",
		s.containers[0], s.ports[0])
	declare := fmt.Sprintf("declare port %s in run.ports of the pool's setup in zerops.yaml as `- port: %s` with `httpSupport: true`", s.ports[0], s.ports[0])
	if len(s.ports) > 1 {
		missing = fmt.Sprintf("Containers %s have no preview port: the pool does not open %s, so a lease landing in one of them has no preview (its preview address answers 502)",
			joinAnd(s.containers), joinOr(s.ports))
		declare = fmt.Sprintf("declare ports %s in run.ports of the pool's setup in zerops.yaml, each as `- port: <n>` with `httpSupport: true`", joinAnd(s.ports))
	}
	return fmt.Sprintf("Pool '%s': maxContainers %d, but %s. %s. "+
		"To fix it, %s, commit it and deploy the pool from that commit (zerops_deploy sourceService=\"<a lease or dev service holding the commit>\" targetService=%q sha=\"<commit>\"); "+
		"or keep maxContainers at %d or below (zerops_scale serviceHostname=%q maxContainers=%d).",
		s.pool, s.maxContainers, has, missing, declare, s.pool, opens, s.pool, opens)
}

// joinOr joins names as "a", "a or b", "a, b or c".
func joinOr(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// LeasePoolNotice is what a lease status says of one pool as a whole.
type LeasePoolNotice struct {
	Pool string `json:"pool"`
	Note string `json:"note"`
}

// LeasePoolPreviewNotices says, of each pool zcp holds a lease record of —
// only pool, when one is named — whose maxContainers runs past the preview
// ports it opens, which containers have no preview and the fixes
// (poolPreviewShortfall.notice, PS-17a). Each pool is read by the id its records
// keep — a direct read, never the search that trails a scale — and one that
// cannot be read, is gone or being deleted (a record of a pool since
// re-created keeps the old id), or reads as no single block of ports says
// nothing. zerops_lease status asks it; the envelope's
// lease reader does not, so no other tool pays the read.
func LeasePoolPreviewNotices(ctx context.Context, d LeaseDeps, pool string) []LeasePoolNotice {
	if d.Client == nil {
		return nil
	}
	records, _ := readLeaseRecords(d.StateDir)
	ids := map[string]map[string]bool{}
	for _, r := range records {
		if (pool != "" && r.PoolHostname != pool) || r.PoolServiceID == "" {
			continue
		}
		if ids[r.PoolHostname] == nil {
			ids[r.PoolHostname] = map[string]bool{}
		}
		ids[r.PoolHostname][r.PoolServiceID] = true
	}
	var out []LeasePoolNotice
	for _, name := range slices.Sorted(maps.Keys(ids)) {
		for _, id := range slices.Sorted(maps.Keys(ids[name])) {
			svc, err := d.Client.GetService(ctx, id)
			if err != nil || svc == nil || svc.Name != name || !topology.IsPoolType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) ||
				platform.ServiceDeleted(svc.Status) || platform.ServiceDeleting(svc.Status) {
				continue
			}
			if short, ok := readPoolPreviewShortfall(svc, poolMaxAfter(svc, ScaleParams{})); ok {
				out = append(out, LeasePoolNotice{Pool: name, Note: short.notice()})
			}
			break
		}
	}
	return out
}
