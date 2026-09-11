package observer

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// SinkBundle is a Bundle (bundle.go) backed by the farm bucket's
// runs/<runId>/ keys (docs/spec-eval-farm.md §1.1), so a later slice can
// observe a run straight from the bucket without pulling it to a local
// directory first (§7.2). Bundle's ReadFile/ListFiles carry no context
// parameter (matching DirBundle), so the context for SinkBundle's network
// calls is fixed at construction instead.
type SinkBundle struct {
	ctx    context.Context //nolint:containedctx // Bundle (S1) has no ctx parameter; this is the one place a network-backed Bundle can carry one
	store  ObjectStore
	runID  string
	prefix string // "runs/<runId>/"
}

// NewSinkBundle returns a Bundle over the farm bucket's runs/<runId>/ keys.
// runID is checked against the FM-47 grammar before any key is built or any
// store call happens (§7.6 FM-47).
func NewSinkBundle(ctx context.Context, store ObjectStore, runID string) (*SinkBundle, error) {
	if !farm.ValidRunID(runID) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidRunID, runID)
	}
	return &SinkBundle{ctx: ctx, store: store, runID: runID, prefix: "runs/" + runID + "/"}, nil
}

// ReadFile returns the bytes at runs/<runId>/<rel>.
func (b *SinkBundle) ReadFile(rel string) ([]byte, error) {
	key := b.prefix + rel
	data, err := b.store.Get(b.ctx, key)
	if err != nil {
		return nil, fmt.Errorf("sinkbundle: read %s: %w", rel, err)
	}
	return data, nil
}

// ListFiles returns every key under runs/<runId>/<prefix>, relative to the
// bundle root (i.e. with the leading runs/<runId>/ stripped back off).
func (b *SinkBundle) ListFiles(prefix string) ([]string, error) {
	fullPrefix := b.prefix + prefix
	keys, err := b.store.List(b.ctx, fullPrefix)
	if err != nil {
		return nil, fmt.Errorf("sinkbundle: list %s: %w", prefix, err)
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, strings.TrimPrefix(key, b.prefix))
	}
	return out, nil
}
