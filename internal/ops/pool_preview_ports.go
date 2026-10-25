package ops

import (
	"fmt"
	"strconv"

	"github.com/zeropsio/zcp/internal/platform"
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
// mutates (spec-workflows §8 PS-17).

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

// poolRaisePreviewPortsError refuses raising the maximum of pool (as the
// direct read has it) to `to` when it is a dev pool zcp leases from and the
// containers the raise allows past its preview block would answer their
// preview on ports the pool does not open; nil for a lowering, the same
// maximum again, a pool zcp leases nothing from, ports zcp cannot read as one
// block, or a raise the block covers.
func poolRaisePreviewPortsError(stateDir string, pool *platform.ServiceStack, to int) error {
	if eff := effectiveAutoscaling(pool); eff != nil && eff.HorizontalMaxCount > 0 && to <= int(eff.HorizontalMaxCount) {
		return nil
	}
	first, last, ok := poolPreviewBlock(pool.Ports)
	opens := last - first + 1
	if !ok || to <= opens || !poolLeasedByZcp(stateDir, pool) {
		return nil
	}
	base := first - 1
	containers := make([]string, 0, to-opens)
	ports := make([]string, 0, to-opens)
	for n := opens + 1; n <= to; n++ {
		containers = append(containers, strconv.Itoa(n))
		ports = append(ports, strconv.Itoa(base+n))
	}
	block := strconv.Itoa(first)
	if last > first {
		block += "-" + strconv.Itoa(last)
	}
	who, portNoun, whose := "container "+containers[0], "port "+ports[0], "its preview"
	if len(ports) > 1 {
		who, portNoun, whose = "containers "+joinAnd(containers), "ports "+joinAnd(ports), "their previews"
	}
	msg := fmt.Sprintf("Pool '%s' opens preview ports %s, one for each of its first %d containers: a lease answers its preview on previewPortBase + its container's stable number (1 … maxContainers), and a per-port subdomain routes only to a port the pool declares with httpSupport: true. "+
		"At maxContainers %d, %s would answer on %s, which the pool does not open, so %s would answer 502",
		pool.Name, block, opens, to, who, portNoun, whose)
	suggestion := fmt.Sprintf("Raising a pool's maxContainers is an infrastructure change — the pool may then run more containers, each at the pool's minimum RAM and CPU or more — so ask the person before you raise it. "+
		"To raise it: declare %s in run.ports of the pool's setup in zerops.yaml, each `- port: <n>` with `httpSupport: true`; commit it; deploy the pool from that commit "+
		"(zerops_deploy sourceService=\"<a lease or dev service holding the commit>\" targetService=%q sha=\"<commit>\"); then raise it again (zerops_scale serviceHostname=%q maxContainers=%d). "+
		"Without a raise, releasing a lease you no longer need frees a container (zerops_lease action=status lists them; ask the person before releasing one this conversation did not take).",
		portNoun, pool.Name, pool.Name, to)
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing, msg, suggestion)
}
