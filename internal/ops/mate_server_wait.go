package ops

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/topology"
)

// MateServerWait identifies the observation that caused a bounded wait.
type MateServerWait struct {
	Hostname, Pool, Branch, AcquiredAt, Name string
	ReadAt                                   time.Time
}

// AwaitMateServer waits for one start and records the answer only if the
// observation and (for a lease) the reservation have not since changed.
func AwaitMateServer(ctx context.Context, d LeaseDeps, statusPath, root string, target MateServerWait) (time.Time, error) {
	var rec leaseRecord
	if target.Pool != "" {
		lease, ok, err := LeaseTargetChecked(ctx, d, target.Hostname, "observed")
		if err != nil {
			return time.Time{}, fmt.Errorf("check lease for server wait: %w", err)
		}
		if !ok || lease.Parked || lease.AcquiredAt != target.AcquiredAt || lease.Branch != target.Branch {
			return time.Time{}, nil
		}
		rec, ok, err = readLeaseRecord(d.StateDir, target.Hostname)
		if err != nil {
			return time.Time{}, fmt.Errorf("read lease for server wait: %w", err)
		}
		if !ok || rec.AcquiredAt != target.AcquiredAt || rec.Branch != target.Branch {
			return time.Time{}, nil
		}
	}
	ds, err := readDevServers(ctx, d.SSH, target.Hostname, root, d.now)
	if err != nil {
		return time.Time{}, fmt.Errorf("read server for wait: %w", err)
	}
	if !ds.Supervisor.Supports(devsup.CapabilityWait) {
		return time.Time{}, nil
	}
	state, err := supervisorWait(ctx, d.SSH, target.Hostname, target.Name, int(devsup.ListenWait/time.Second))
	if err != nil {
		return time.Time{}, fmt.Errorf("wait for server: %w", err)
	}
	ds.State, ds.ReadAt = state, d.now().UTC()
	ds.Service = target.Pool
	servers := DevServerStates(ctx, d.Client, d.ProjectID, &DevServerResult{Hostname: target.Hostname, Pool: target.Pool, Read: ds})
	var observed *topology.DevServerState
	for i := range servers {
		if servers[i].Name == target.Name {
			observed = &servers[i]
			break
		}
	}
	if observed == nil {
		return ds.ReadAt, nil
	}
	if target.Pool != "" {
		unlock, err := lockLeaseRecord(d.StateDir, target.Hostname)
		if err != nil {
			return time.Time{}, fmt.Errorf("lock lease after server wait: %w", err)
		}
		defer unlock()
		current, found, err := readLeaseRecord(d.StateDir, target.Hostname)
		if err != nil {
			return time.Time{}, fmt.Errorf("read lease after server wait: %w", err)
		}
		if !found || current.Lease != rec.Lease || current.ContainerID != rec.ContainerID {
			return ds.ReadAt, nil
		}
	}
	if err := mate.UpdateStatus(statusPath, func(s *mate.Status) {
		if s.Copies == nil {
			return
		}
		if target.Pool != "" {
			for i := range s.Copies.Leases {
				v := &s.Copies.Leases[i]
				if v.Alias == target.Hostname && v.AcquiredAt == target.AcquiredAt && v.Branch == target.Branch {
					mergeWaitedServer(v.DevServers, &v.DevServersReadAt, target, *observed)
					return
				}
			}
			return
		}
		for i := range s.Copies.DevServices {
			v := &s.Copies.DevServices[i]
			if v.Hostname == target.Hostname {
				mergeWaitedServer(v.DevServers, &v.DevServersReadAt, target, *observed)
				return
			}
		}
	}); err != nil {
		return time.Time{}, fmt.Errorf("store waited server: %w", err)
	}
	return ds.ReadAt, nil
}

func mergeWaitedServer(servers *[]topology.DevServerState, at *string, target MateServerWait, observed topology.DevServerState) {
	if servers == nil {
		return
	}
	for i := range *servers {
		old := &(*servers)[i]
		if old.Name == target.Name && old.ReadAt.Equal(target.ReadAt) {
			*old = observed
			if observed.ReadAt.After(parseCopyTime(*at)) {
				*at = observed.ReadAt.UTC().Format(time.RFC3339Nano)
			}
			return
		}
	}
}
