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
	return publishMateLeaseRecords(stateDir, statusPath, time.Time{})
}

// PublishMateLeasesAtBoot discovers held records without first publishing
// their previous state as current. A report newer than boot remains newer.
func PublishMateLeasesAtBoot(stateDir, statusPath string, at time.Time) error {
	return publishMateLeaseRecords(stateDir, statusPath, at)
}

func publishMateLeaseRecords(stateDir, statusPath string, bootAt time.Time) error {
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
					v.State, v.StateAt = old.State, old.StateAt
					break
				}
			}
			if !bootAt.IsZero() && bootAt.After(parseCopyTime(v.StateAt)) {
				v.State, _ = mate.NextCopyState(v.State, mate.CopyEvent{Kind: mate.CopyLinkLost})
				v.StateAt = bootAt.UTC().Format(time.RFC3339Nano)
			} else if v.State == "" {
				v.State, _ = mate.NextCopyState("", mate.CopyEvent{Kind: mate.CopyRecorded})
				v.StateAt = time.Now().UTC().Format(time.RFC3339Nano)
			}
			leases = append(leases, v)
		}
		for _, old := range previous {
			found := false
			for _, current := range leases {
				if old.Alias == current.Alias && old.AcquiredAt == current.AcquiredAt && old.Branch == current.Branch {
					found = true
					break
				}
			}
			if !found {
				_, _ = mate.NextCopyState(old.State, mate.CopyEvent{Kind: mate.CopyRelease})
			}
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

// MateCopyTarget identifies an entry and, for a held wait, the state it saw.
type MateCopyTarget struct {
	Hostname, Pool, Branch, AcquiredAt string
	State                              mate.CopyState
	StateAt                            string
}

// StoreMateCopyEvent orders the event under the status lock. A held wait may
// write only over the state from which it was armed; a late snapshot may not
// overwrite a newer event. Lease identity is checked under the record lock.
func StoreMateCopyEvent(stateDir, statusPath string, target MateCopyTarget, event mate.CopyEvent, at time.Time, expected string) error {
	if target.Pool != "" {
		unlock, err := lockLeaseRecord(stateDir, target.Hostname)
		if err != nil {
			return fmt.Errorf("lock copy event: %w", err)
		}
		defer unlock()
		rec, ok, err := readLeaseRecord(stateDir, target.Hostname)
		if err != nil {
			return fmt.Errorf("read copy event lease: %w", err)
		}
		if !ok || rec.AcquiredAt != target.AcquiredAt || rec.Branch != target.Branch {
			return nil
		}
	}
	if err := mate.UpdateStatus(statusPath, func(s *mate.Status) {
		if s.Copies == nil {
			return
		}
		apply := func(state *mate.CopyState, stateAt *string) {
			if expected != "" && *stateAt != expected {
				return
			}
			if !at.After(parseCopyTime(*stateAt)) {
				return
			}
			next, ok := mate.NextCopyState(*state, event)
			if !ok {
				fmt.Fprintf(os.Stderr, "[zcp] copy %s refused event %s in %s\n", target.Hostname, event.Kind, *state)
				return
			}
			*state, *stateAt = next, at.UTC().Format(time.RFC3339Nano)
		}
		if target.Pool != "" {
			for i := range s.Copies.Leases {
				v := &s.Copies.Leases[i]
				if v.Alias == target.Hostname && v.AcquiredAt == target.AcquiredAt && v.Branch == target.Branch {
					apply(&v.State, &v.StateAt)
					return
				}
			}
		} else {
			for i := range s.Copies.DevServices {
				v := &s.Copies.DevServices[i]
				if v.Hostname == target.Hostname {
					apply(&v.State, &v.StateAt)
					return
				}
			}
		}
	}); err != nil {
		return fmt.Errorf("store copy event: %w", err)
	}
	return nil
}

// MarkMateCopiesUnknown is the observer's boot event for every held copy.
func MarkMateCopiesUnknown(statusPath string, at time.Time) error {
	if err := mate.UpdateStatus(statusPath, func(s *mate.Status) {
		if s.Copies == nil {
			return
		}
		apply := func(state *mate.CopyState, when *string) {
			if !at.After(parseCopyTime(*when)) {
				return
			}
			next, ok := mate.NextCopyState(*state, mate.CopyEvent{Kind: mate.CopyLinkLost})
			if ok {
				*state, *when = next, at.UTC().Format(time.RFC3339Nano)
			}
		}
		for i := range s.Copies.Leases {
			v := &s.Copies.Leases[i]
			apply(&v.State, &v.StateAt)
		}
		for i := range s.Copies.DevServices {
			v := &s.Copies.DevServices[i]
			apply(&v.State, &v.StateAt)
		}
	}); err != nil {
		return fmt.Errorf("mark copy links lost: %w", err)
	}
	return nil
}

func publishMateLeaseEvent(d LeaseDeps, rec leaseRecord, event mate.CopyEvent) {
	if !runtime.DetectFrom(mate.LiveLookup(mate.LiveEnvStorePath)).MateEnabled {
		return
	}
	target := MateCopyTarget{Hostname: rec.alias(), Pool: rec.PoolHostname, Branch: rec.Branch, AcquiredAt: rec.AcquiredAt}
	if err := StoreMateCopyEvent(d.StateDir, mate.StatusFilePath(), target, event, d.now().UTC(), ""); err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] publish lease copy event: %v\n", err)
	}
}
