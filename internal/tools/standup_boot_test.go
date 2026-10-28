package tools

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestStandup_BootImportDecisions(t *testing.T) {
	t.Parallel()
	for _, decision := range []struct {
		name   string
		cancel bool
	}{
		{name: "A stand-up whose boot-import wait expires while its import is still running does not adopt, deploy or ask for another import."},
		{name: "Cancelling a boot-import wait does not begin preparation mutations.", cancel: true},
	} {
		t.Run(decision.name, func(t *testing.T) {
			t.Parallel()
			for _, missing := range []bool{false, true} {
				name := "every half is visible"
				if missing {
					name = "a half is still missing"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					f := newStandupFixture(t)
					boot := mate.RuntimesStatus{State: mate.RuntimesImporting, Services: []mate.RuntimeService{
						{Hostname: "nextstorestage", State: mate.ServiceCreating, ProcessID: "boot-import-1"},
					}}
					if err := mate.UpdateStatus(f.statusPath, func(s *mate.Status) { s.Runtimes = boot }); err != nil {
						t.Fatal(err)
					}
					present := f.services
					if missing {
						present = withoutService(present, "nextstorestage")
					}
					f.importing = &importingClient{Mock: f.mock, stages: [][]platform.ServiceStack{present}}
					d := f.deps(t)
					if !decision.cancel {
						d.bootWait = 10 * time.Millisecond
					}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					waited := false
					progress := newStandupProgress(func(message string, _, _ float64) {
						if strings.HasPrefix(message, "waiting for the container's import") {
							waited = true
							if decision.cancel {
								cancel()
							}
						}
					})
					progress.status = f.status
					// This is the full consumer invoked by the MCP handler, including
					// preparation, deployment, reporting and persisted stand-up status.
					result := d.run(ctx, progress)
					text := getTextContent(t, result)
					metas, err := workflow.ListServiceMetas(f.stateDir)
					if err != nil {
						t.Fatal(err)
					}
					if len(metas) != 0 || len(f.mounter.mounted) != 0 || len(f.ssh.pushes()) != 0 || f.mock.CallCount("ImportServices") != 0 {
						t.Errorf("ASSERTION: an unresolved boot import must leave preparation untouched: adopted=%d mounted=%d deployed=%d imported=%d", len(metas), len(f.mounter.mounted), len(f.ssh.pushes()), f.mock.CallCount("ImportServices"))
					}
					for _, forbidden := range []string{"zerops_import", "route=", "zerops_deploy_batch"} {
						if strings.Contains(text, forbidden) {
							t.Errorf("ASSERTION: an unresolved boot import must not suggest another mutation (%s): %s", forbidden, text)
						}
					}
					want := "still importing"
					if decision.cancel {
						want = "context canceled"
					}
					if (decision.cancel && !waited) || !result.IsError || !strings.Contains(text, want) || !strings.Contains(text, "zerops_standup") {
						t.Errorf("ASSERTION: the stopped observer must report %q and only retry stand-up; waited=%v result=%s", want, waited, text)
					}
					if f.importing.reads != 0 {
						t.Errorf("ASSERTION: preparation read the project %d times before boot ownership ended", f.importing.reads)
					}
					status, err := mate.ReadStatus(f.statusPath)
					if err != nil || !reflect.DeepEqual(status.Runtimes, boot) {
						t.Fatalf("boot import identity/state changed: %+v, %v", status.Runtimes, err)
					}
					// The same accepted import completes later. A fresh MCP call may
					// now prepare and deploy each dev half once, without importing.
					if err := mate.UpdateStatus(f.statusPath, func(s *mate.Status) {
						s.Runtimes.State = mate.RuntimesDone
						s.Runtimes.Services[0].State = mate.ServiceRunning
					}); err != nil {
						t.Fatal(err)
					}
					f.importing.stages = [][]platform.ServiceStack{f.services}
					next, body := f.run(t)
					if next.IsError || body.StandUp != standupDevelopment {
						t.Fatalf("completed import did not allow continuation: %s", getTextContent(t, next))
					}
					metas, err = workflow.ListServiceMetas(f.stateDir)
					if err != nil || len(metas) != 2 || len(f.mounter.mounted) != 2 || !reflect.DeepEqual(sorted(f.ssh.pushes()), []string{"medusadev → svc-medusadev (medusadev)", "nextstoredev → svc-nextstoredev (nextstoredev)"}) || f.mock.CallCount("ImportServices") != 0 {
						t.Errorf("continuation: adopted=%d mounted=%d deployed=%v imported=%d err=%v", len(metas), len(f.mounter.mounted), f.ssh.pushes(), f.mock.CallCount("ImportServices"), err)
					}
				})
			}
		})
	}
}
