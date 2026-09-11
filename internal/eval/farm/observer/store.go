package observer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// ObjectStore is the bucket seam Store needs — satisfied by *farm.SinkClient
// (Get/Put/Head/List) and, in tests, by an in-memory fake. Kept as a small
// interface here (rather than importing the farm package's own SinkClient
// type into every call site) so tests never need a real HTTP server.
type ObjectStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, body []byte) error
	Head(ctx context.Context, key string) (exists bool, size int64, err error)
	List(ctx context.Context, prefix string) ([]string, error)
}

var (
	// ErrInvalidRunID is returned when a run id fails the FM-47 grammar
	// (docs/spec-eval-farm.md §7.6).
	ErrInvalidRunID = errors.New("observer: invalid run id")
	// ErrKeyOutsidePrefix is returned when a key falls outside
	// runs/<runId>/observer/ (docs/spec-eval-farm.md §7.6 FM-47).
	ErrKeyOutsidePrefix = errors.New("observer: key outside runs/<runId>/observer/ prefix")
	// ErrObservationExists is returned by Store.PutObservation when the
	// target key already has an object (docs/spec-eval-farm.md §7.5: "the
	// store never overwrites an existing observation key").
	ErrObservationExists = errors.New("observer: observation already exists")
)

// Store reads and writes observations in the farm bucket, restricted to
// runs/<runId>/observer/ (docs/spec-eval-farm.md §7.5, §7.6 FM-47).
type Store struct {
	client ObjectStore
}

// NewStore returns a Store backed by client (a *farm.SinkClient in
// production).
func NewStore(client ObjectStore) *Store {
	return &Store{client: client}
}

// observerPrefix returns "runs/<runId>/observer/", after checking runID
// against the FM-47 grammar. No key is built and no network call happens
// when runID is invalid.
func observerPrefix(runID string) (string, error) {
	if !farm.ValidRunID(runID) {
		return "", fmt.Errorf("%w: %q", ErrInvalidRunID, runID)
	}
	return "runs/" + runID + "/observer/", nil
}

// validateObserverKey enforces §7.6 FM-47: a write may only target
// "runs/<runId>/observer/<obsId>.json" for a runID matching the FM-47
// grammar — checked before key is ever built into a network call. Refuses
// key when it: names a different run than runID, escapes the observer/
// directory (an embedded "/" or ".." past the prefix), or is otherwise not
// a direct child of the prefix.
func validateObserverKey(runID, key string) error {
	prefix, err := observerPrefix(runID)
	if err != nil {
		return err
	}
	rest, ok := strings.CutPrefix(key, prefix)
	if !ok {
		return fmt.Errorf("%w: %q is not under %q", ErrKeyOutsidePrefix, key, prefix)
	}
	if rest == "" || strings.Contains(rest, "/") || strings.Contains(rest, "..") {
		return fmt.Errorf("%w: %q", ErrKeyOutsidePrefix, key)
	}
	return nil
}

// PutObservation stores obs at runs/<runId>/observer/<obs.ObsID>.json,
// refusing to overwrite an existing observation (Head before Put, §7.5) and
// refusing any key outside the observer prefix before any network call
// (§7.6 FM-47).
func (s *Store) PutObservation(ctx context.Context, runID string, obs Observation) error {
	if obs.ObsID == "" {
		return fmt.Errorf("%w: empty observation id", ErrKeyOutsidePrefix)
	}
	key := "runs/" + runID + "/observer/" + obs.ObsID + ".json"
	if err := validateObserverKey(runID, key); err != nil {
		return err
	}

	exists, _, err := s.client.Head(ctx, key)
	if err != nil {
		return fmt.Errorf("observer: head %s: %w", key, err)
	}
	if exists {
		return fmt.Errorf("%w: %s", ErrObservationExists, key)
	}

	body, err := json.Marshal(obs)
	if err != nil {
		return fmt.Errorf("observer: marshal observation: %w", err)
	}
	if err := s.client.Put(ctx, key, body); err != nil {
		return fmt.Errorf("observer: put %s: %w", key, err)
	}
	return nil
}

// ListObservations returns runID's obsIds, oldest first — the obsId's
// leading UTC timestamp sorts lexicographically in chronological order
// (§7.5).
func (s *Store) ListObservations(ctx context.Context, runID string) ([]string, error) {
	prefix, err := observerPrefix(runID)
	if err != nil {
		return nil, err
	}
	keys, err := s.client.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("observer: list %s: %w", prefix, err)
	}
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		rel := strings.TrimPrefix(key, prefix)
		if rel == "" || strings.Contains(rel, "/") || !strings.HasSuffix(rel, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(rel, ".json"))
	}
	sort.Strings(ids)
	return ids, nil
}

// GetObservation reads and parses one runID/obsID observation.
func (s *Store) GetObservation(ctx context.Context, runID, obsID string) (Observation, error) {
	prefix, err := observerPrefix(runID)
	if err != nil {
		return Observation{}, err
	}
	if obsID == "" {
		return Observation{}, fmt.Errorf("%w: empty observation id", ErrKeyOutsidePrefix)
	}
	key := prefix + obsID + ".json"
	body, err := s.client.Get(ctx, key)
	if err != nil {
		return Observation{}, fmt.Errorf("observer: get %s: %w", key, err)
	}
	var obs Observation
	if err := json.Unmarshal(body, &obs); err != nil {
		return Observation{}, fmt.Errorf("observer: parse %s: %w", key, err)
	}
	return obs, nil
}

// CurrentObservation returns runID's newest obsId (§7.5) and whether any
// observation exists yet.
func (s *Store) CurrentObservation(ctx context.Context, runID string) (obsID string, found bool, err error) {
	ids, err := s.ListObservations(ctx, runID)
	if err != nil {
		return "", false, err
	}
	if len(ids) == 0 {
		return "", false, nil
	}
	return ids[len(ids)-1], true, nil
}
