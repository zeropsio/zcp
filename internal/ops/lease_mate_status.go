package ops

import (
	"fmt"
	"os"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// publishMateLeases publishes after a record mutation, without changing the
// outcome of an already completed reservation or release if the file fails.
func publishMateLeases(stateDir, action string) {
	if !runtime.DetectFrom(mate.LiveLookup(mate.LiveEnvStorePath)).MateEnabled {
		return
	}
	if err := PublishMateLeases(stateDir, mate.StatusFilePath()); err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] publish Mate leases after %s: %v\n", action, err)
	}
}

// PublishMateLeases replaces the record projection while holding the status
// lock. Reading inside that lock orders concurrent writers across aliases.
func PublishMateLeases(stateDir, statusPath string) error {
	if err := mate.UpdateStatusChecked(statusPath, func(s *mate.Status) error {
		records, err := readLeaseRecords(stateDir)
		if err != nil {
			return fmt.Errorf("read lease records: %w", err)
		}
		if s.Copies == nil {
			s.Copies = &mate.CopyStatus{}
		}
		previous := s.Copies.Leases
		leases := make([]mate.LeaseCopyStatus, 0, len(records))
		for _, rec := range records {
			v := mate.LeaseCopyStatus{Alias: rec.alias(), Pool: rec.PoolHostname, ContainerHostname: rec.ContainerHostname,
				Branch: rec.Branch, Repo: topology.RepoIdentity(rec.Repo), AcquiredAt: rec.AcquiredAt}
			for _, old := range previous {
				if old.Alias == v.Alias && old.AcquiredAt == v.AcquiredAt && old.Branch == v.Branch {
					v.DevServers, v.DevServersReadAt = old.DevServers, old.DevServersReadAt
					break
				}
			}
			leases = append(leases, v)
		}
		s.Copies.Leases = leases
		return nil
	}); err != nil {
		return fmt.Errorf("publish lease records: %w", err)
	}
	return nil
}

// StoreMateLeaseServers keeps a supervisor read only while the record is the
// same lease. It holds the record lock in the same order as its writers.
func StoreMateLeaseServers(stateDir, statusPath string, target LeaseTargetInfo, servers []topology.DevServerState, readAt time.Time) error {
	unlock, err := lockLeaseRecord(stateDir, target.Alias)
	if err != nil {
		return fmt.Errorf("lock lease observation: %w", err)
	}
	defer unlock()
	rec, ok, err := readLeaseRecord(stateDir, target.Alias)
	if err != nil {
		return fmt.Errorf("read lease observation: %w", err)
	}
	if !ok || rec.AcquiredAt != target.AcquiredAt || rec.Branch != target.Branch || rec.ContainerHostname != target.ContainerHostname {
		return nil
	}
	if err := mate.UpdateStatus(statusPath, func(s *mate.Status) {
		if s.Copies == nil {
			return
		}
		for i := range s.Copies.Leases {
			v := &s.Copies.Leases[i]
			if v.Alias == target.Alias && v.AcquiredAt == target.AcquiredAt && v.Branch == target.Branch {
				if v.DevServersReadAt == "" || readAt.After(parseCopyTime(v.DevServersReadAt)) {
					v.DevServers, v.DevServersReadAt = &servers, readAt.UTC().Format(time.RFC3339Nano)
				}
				return
			}
		}
	}); err != nil {
		return fmt.Errorf("store lease servers: %w", err)
	}
	return nil
}

func parseCopyTime(raw string) time.Time {
	at, _ := time.Parse(time.RFC3339Nano, raw)
	return at
}
