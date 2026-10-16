package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

type recordingUnits struct {
	removed []string
	err     error
}

func (u *recordingUnits) RemoveUnits(_ context.Context, prefix string) error {
	u.removed = append(u.removed, prefix)
	return u.err
}

// TestRetireDevServerKeeperAtBoot_RemovesKeeperUnits_NeverFailsInit: every
// container start retires the units an older zcp's dev-server keeper left —
// not only a container where an agent session starts `zcp serve` — and a
// failed removal is reported without failing the start; outside a container
// there are no such units to touch.
func TestRetireDevServerKeeperAtBoot_RemovesKeeperUnits_NeverFailsInit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		rt          runtime.Info
		removeErr   error
		wantRemoved []string
		wantStderr  string
	}{
		{name: "container", rt: runtime.Info{InContainer: true}, wantRemoved: []string{"devservers-"}},
		{name: "container, removal fails", rt: runtime.Info{InContainer: true}, removeErr: errors.New("zsc: boom"), wantRemoved: []string{"devservers-"}, wantStderr: "zsc: boom"},
		{name: "local", rt: runtime.Info{InContainer: false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			units := &recordingUnits{err: tt.removeErr}
			var stderr bytes.Buffer
			retireDevServerKeeperAtBoot(context.Background(), tt.rt, units, &stderr)
			if !slices.Equal(units.removed, tt.wantRemoved) {
				t.Errorf("units removed = %q, want %q", units.removed, tt.wantRemoved)
			}
			if tt.wantStderr == "" && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want nothing on a clean run", stderr.String())
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want the failure %q reported", stderr.String(), tt.wantStderr)
			}
		})
	}
}
