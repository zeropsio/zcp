package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// Decision: A mount read that cannot answer is not reported as an absent mount.
func TestMountStatus_AMountReadThatCannotAnswerIsNotReportedAsAnAbsentMount(t *testing.T) {
	t.Parallel()
	for _, hostname := range []string{"app", "apppool-3", ""} {
		t.Run(hostname, func(t *testing.T) {
			t.Parallel()
			unreadable := errors.New("mount observation did not answer")
			m := newMockMounter()
			m.checkErr = unreadable
			got, err := MountStatus(context.Background(), platform.NewMock().WithServices(testServices()), "proj-1", m, hostname)
			if !errors.Is(err, unreadable) || got != nil {
				t.Fatalf("ASSERTION: unreadable mount must return its observation error, never absent mount facts: got %+v, err %v", got, err)
			}
		})
	}
}

func TestLeaseHost_MountReadCannotAnswer_NamesUnknownRatherThanAbsence(t *testing.T) {
	t.Parallel()
	m := newMockMounter()
	m.checkErr = errors.New("mount observation did not answer")
	got := (LeaseHost{Mounter: m}).gap(context.Background(), "apppool-3")
	if !strings.Contains(got, "mount status could not be read") || strings.Contains(got, "it is not mounted") {
		t.Fatalf("ASSERTION: unreadable lease mount is unknown, never asserted absent: %q", got)
	}
}

type unreadableUnitMounter struct {
	*mockMounter
	unitErr error
}

func (m *unreadableUnitMounter) HasUnit(context.Context, string) (bool, error) {
	return false, m.unitErr
}

func TestMount_UnitReadCannotAnswer_NoAbsenceOrMountEffect(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"status", "mount", "unmount"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			m := &unreadableUnitMounter{mockMounter: newMockMounter(), unitErr: errors.New("unit observation did not answer")}
			client := platform.NewMock().WithServices(testServices())
			var err error
			switch action {
			case "status":
				_, err = MountStatus(context.Background(), client, "proj-1", m, "app")
			case "mount":
				_, err = MountService(context.Background(), client, "proj-1", m, "app")
			case "unmount":
				_, err = UnmountService(context.Background(), client, "proj-1", m, "app")
			}
			if err == nil || m.states["/var/www/app"] == platform.MountStateActive {
				t.Fatalf("ASSERTION: unreadable unit must not imply absence or permit mount effects: state %v, err %v", m.states, err)
			}
		})
	}
}
