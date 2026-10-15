package ops

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
)

// A dev service that runs several heavy dev servers can outgrow its RAM
// ceiling and wedge: no SSH, restart answering 500 (zeropsio/apps, two Analog
// servers of ~2.5 GiB each plus a stage deploy on a 6 GiB service). Starting
// one more server is refused when the headroom left would not hold another
// server like the largest one already running.
const (
	// devServerMarginFloorKB is the margin when no server runs yet or all are
	// small: the platform's default free-RAM buffer is 2 GiB (AUTOSCALING.md);
	// half of it still leaves the autoscaler room to react.
	devServerMarginFloorKB = 1024 * 1024
	gbKB                   = 1024 * 1024
)

// memoryProbeCmd prints one line: cgroup v2 memory.max, memory.current and
// memory.stat's inactive_file, in bytes ("max" when unlimited).
const memoryProbeCmd = `m=/sys/fs/cgroup; echo "@@zcp-memory $(cat $m/memory.max 2>/dev/null) $(cat $m/memory.current 2>/dev/null) $(awk '/^inactive_file /{print $2}' $m/memory.stat 2>/dev/null)"`

// containerMemory is the dev container's memory in KiB. UsedKB leaves out
// reclaimable page cache. MaxKB is 0 when the cgroup is unlimited.
type containerMemory struct {
	MaxKB, UsedKB int64
}

// readContainerMemory returns false when the container's cgroup files are not
// readable; the start then goes ahead as it always did.
func readContainerMemory(ctx context.Context, ssh SSHDeployer, hostname string) (containerMemory, bool) {
	out, err := ssh.ExecSSH(ctx, hostname, memoryProbeCmd)
	if err != nil {
		return containerMemory{}, false
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "@@zcp-memory ")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 2 {
			return containerMemory{}, false
		}
		current, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			return containerMemory{}, false
		}
		var inactive int64
		if len(f) > 2 {
			inactive, _ = strconv.ParseInt(f[2], 10, 64)
		}
		var maxB int64
		if f[0] != "max" {
			if maxB, err = strconv.ParseInt(f[0], 10, 64); err != nil {
				return containerMemory{}, false
			}
		}
		return containerMemory{MaxKB: maxB / 1024, UsedKB: (current - inactive) / 1024}, true
	}
	return containerMemory{}, false
}

// serviceMaxRAMKB is the service's autoscaling ceiling (maxRam, GiB) in KiB, 0
// when unknown. The cgroup's memory.max is only what is granted now; the
// autoscaler grows it up to this.
func serviceMaxRAMKB(ctx context.Context, client platform.Client, projectID, hostname string) int64 {
	if client == nil {
		return 0
	}
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return 0
	}
	svc, err := FindService(services, hostname)
	if err != nil || svc.CustomAutoscaling == nil {
		return 0
	}
	return int64(svc.CustomAutoscaling.MaxRAM * gbKB)
}

// memoryRefusal is the one sentence for a start that would leave less headroom
// than the largest running server holds; "" when the start fits.
func memoryRefusal(hostname, name string, mem containerMemory, maxRAMKB int64, st devsup.State) string {
	ceiling := mem.MaxKB
	if maxRAMKB > 0 {
		ceiling = maxRAMKB
	}
	if ceiling <= 0 {
		return ""
	}
	margin := int64(devServerMarginFloorKB)
	var running []string
	for n, s := range st.Servers {
		if s.Status == devsup.StatusStopped || s.Status == devsup.StatusCrashed || s.RssKB == 0 {
			continue
		}
		margin = max(margin, s.RssKB)
		running = append(running, fmt.Sprintf("%s %s", n, gib(s.RssKB)))
	}
	headroom := ceiling - mem.UsedKB
	if headroom >= margin {
		return ""
	}
	slices.Sort(running)
	return fmt.Sprintf("Not starting %s: %s uses %s of its %s RAM ceiling, leaving %s, less than the %s one more server like the largest running one needs (running: %s) — stop another dev server on %s, or raise the service's max RAM with zerops_scale.",
		serverLabel(hostname, name), hostname, gib(mem.UsedKB), gib(ceiling), gib(max(headroom, 0)), gib(margin), strings.Join(running, ", "), hostname)
}

func gib(kb int64) string {
	return strconv.FormatFloat(float64(kb)/gbKB, 'f', 1, 64) + " GiB"
}

// refuseStartOnMemory is the guard of a start: nil, or the precondition error
// carrying memoryRefusal. A restart of a running server and an unreadable
// cgroup pass.
func refuseStartOnMemory(ctx context.Context, ssh SSHDeployer, client platform.Client, projectID string, p DevServerParams, name string, ds *DevServers) error {
	if s, ok := ds.State.Servers[name]; ok && s.Status != devsup.StatusStopped && s.Status != devsup.StatusCrashed {
		return nil
	}
	mem, ok := readContainerMemory(ctx, ssh, p.Hostname)
	if !ok {
		return nil
	}
	msg := memoryRefusal(p.Hostname, name, mem, serviceMaxRAMKB(ctx, client, projectID, p.Hostname), ds.State)
	if msg == "" {
		return nil
	}
	return platform.NewPlatformError(platform.ErrPreflightFailed, msg,
		"Stop a dev server you are not working on (zerops_dev_server action=stop name=<server>), or raise the service's max RAM (zerops_scale).")
}
