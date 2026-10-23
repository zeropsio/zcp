// Tests for: ops/lease_checkout.go abandonLease — a checkout that fails hands
// the container back, and the refusal's message and suggestion say the same,
// true thing about where the container went: released, deleted (it could not
// be reset), gone already, or still reserved (docs/spec-workflows.md §8 DP-6,
// DP-15).
package ops

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestAbandonLease_MessageAndSuggestionSayWhereTheContainerWent(t *testing.T) {
	t.Parallel()
	busy := &platform.SwarmError{Status: http.StatusConflict, Code: "containerAction", Message: "container is busy", Reason: "container-busy", Retryable: true}
	tests := []struct {
		name       string
		releaseErr error
		deleteErr  error
		wantKept   bool     // the record survives: the container stays reserved
		wantMsg    []string // in the message
		wantSug    []string // in the suggestion
		bannedBoth []string // in neither
	}{
		{
			name:    "released",
			wantMsg: []string{"released with a reset"}, wantSug: []string{"acquire again"},
			bannedBoth: []string{"stays reserved", "deleted", "force=true"},
		},
		{
			name:       "could not be reset: deleted",
			releaseErr: noPrimarySnapshot(),
			wantMsg:    []string{"could not be reset", "deleted"}, wantSug: []string{"acquire again", "minContainers"},
			bannedBoth: []string{"stays reserved", "released with a reset", "force=true"},
		},
		{
			name:       "the orchestrator no longer held it",
			releaseErr: &platform.SwarmError{Status: http.StatusConflict, Code: "containerAction", Message: "unknown lease", Reason: "unknown-lease"},
			wantMsg:    []string{"no longer held"}, wantSug: []string{"acquire again"},
			bannedBoth: []string{"stays reserved", "released with a reset", "deleted", "force=true"},
		},
		{
			name:       "the release is refused: still reserved",
			releaseErr: busy, wantKept: true,
			wantMsg: []string{"stays reserved"}, wantSug: []string{"zerops_lease action=release lease=devpool-3 force=true"},
			bannedBoth: []string{"was released", "released with a reset", "deleted", "dropped"},
		},
		{
			name:       "could not be reset and the delete is refused: still reserved",
			releaseErr: noPrimarySnapshot(), deleteErr: busy, wantKept: true,
			wantMsg: []string{"stays reserved", "could not be reset"}, wantSug: []string{"zerops_lease action=release lease=devpool-3 force=true"},
			bannedBoth: []string{"was released", "released with a reset", "dropped"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.acquired = acquiredAs(3)
			fx.swarm.releaseErr, fx.swarm.deleteErr = tt.releaseErr, tt.deleteErr
			out := "fatal: repository 'https://github.com/acme/app/' not found"
			fx.deps.SSH = &leaseSSH{fn: func(string, string) ([]byte, error) {
				return []byte(out), sshFailure(out, exitError(t, 128))
			}}

			_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")

			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want the checkout's refusal", err)
			}
			for _, want := range tt.wantMsg {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q lacks %q", pe.Message, want)
				}
			}
			for _, want := range tt.wantSug {
				if !strings.Contains(pe.Suggestion, want) {
					t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
				}
			}
			for _, banned := range tt.bannedBoth {
				if strings.Contains(pe.Message, banned) || strings.Contains(pe.Suggestion, banned) {
					t.Errorf("message %q / suggestion %q say %q, which is not what happened", pe.Message, pe.Suggestion, banned)
				}
			}
			if strings.Contains(pe.Message+pe.Suggestion, testLease) {
				t.Errorf("the refusal leaks the lease")
			}
			_, recErr := os.Stat(filepath.Join(fx.stateDir, "leases", "devpool-3.json"))
			if tt.wantKept != (recErr == nil) {
				t.Errorf("record kept = %v, want %v", recErr == nil, tt.wantKept)
			}
		})
	}
}
