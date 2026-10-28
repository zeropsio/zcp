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
// declaration; zcp writes neither. So a pool whose maxContainers outgrows its
// preview ports has containers with no preview — Kara's run, 2026-10-25:
// hqpool declared 3001-3003, was raised from 3 to 4, and container 4's
// preview on 3004 answered 502 with nothing saying why. The scale is the
// person's call and still runs; its result, and a lease status of the pool,
// say so (spec-workflows §8 PS-17).

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

// poolPreviewPortsNotice says that pool (as the direct read has it), at
// maxContainers maxContainers, allows more containers than it opens preview
// ports for: how many containers against how many ports, which containers
// have no preview port, and the two fixes — declare the ports and deploy the
// pool, or keep maxContainers at the port count. "" when the ports cover
// maxContainers, and when zcp cannot read the ports as one block (no HTTP
// port, or two longest runs of one length): zcp names a count only when it
// knows it. The caller decides the pool is a dev pool (poolLeasedByZcp).
func poolPreviewPortsNotice(pool *platform.ServiceStack, maxContainers int) string {
	first, last, ok := poolPreviewBlock(pool.Ports)
	opens := last - first + 1
	if !ok || maxContainers <= opens {
		return ""
	}
	base := first - 1
	containers := make([]string, 0, maxContainers-opens)
	ports := make([]string, 0, maxContainers-opens)
	for n := opens + 1; n <= maxContainers; n++ {
		containers = append(containers, strconv.Itoa(n))
		ports = append(ports, strconv.Itoa(base+n))
	}
	block := strconv.Itoa(first)
	if last > first {
		block += "-" + strconv.Itoa(last)
	}
	has := fmt.Sprintf("only %d preview ports (%s), one for each of its first %d containers", opens, block, opens)
	if opens == 1 {
		has = fmt.Sprintf("only 1 preview port (%s), for its first container", block)
	}
	missing := fmt.Sprintf("Container %s has no preview port: the pool does not open %s, so a lease landing there has no preview (its preview address answers 502)",
		containers[0], ports[0])
	declare := fmt.Sprintf("declare port %s in run.ports of the pool's setup in zerops.yaml as `- port: %s` with `httpSupport: true`", ports[0], ports[0])
	if len(ports) > 1 {
		missing = fmt.Sprintf("Containers %s have no preview port: the pool does not open %s, so a lease landing in one of them has no preview (its preview address answers 502)",
			joinAnd(containers), joinOr(ports))
		declare = fmt.Sprintf("declare ports %s in run.ports of the pool's setup in zerops.yaml, each as `- port: <n>` with `httpSupport: true`", joinAnd(ports))
	}
	return fmt.Sprintf("Pool '%s': maxContainers %d, but %s. %s. "+
		"To fix it, %s, commit it and deploy the pool from that commit (zerops_deploy sourceService=\"<a lease or dev service holding the commit>\" targetService=%q sha=\"<commit>\"); "+
		"or keep maxContainers at %d or below (zerops_scale serviceHostname=%q maxContainers=%d).",
		pool.Name, maxContainers, has, missing, declare, pool.Name, opens, pool.Name, opens)
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
// (poolPreviewPortsNotice, PS-17). Each pool is read by the id its records
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
			if note := poolPreviewPortsNotice(svc, poolMaxAfter(svc, ScaleParams{})); note != "" {
				out = append(out, LeasePoolNotice{Pool: name, Note: note})
			}
			break
		}
	}
	return out
}
