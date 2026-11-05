package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// heldBootDeadline lets the test decide when the import observation expires.
func heldBootDeadline(d *standupDeps) func() {
	var expire func()
	d.bootContext = func(ctx context.Context, _ time.Duration, cause error) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancelCause(ctx)
		expire = func() { cancel(cause) }
		return ctx, func() { cancel(context.Canceled) }
	}
	return func() { expire() }
}

func standupBody(t *testing.T, result *mcp.CallToolResult) standupResponse {
	t.Helper()
	var body standupResponse
	if !result.IsError {
		if err := json.Unmarshal([]byte(getTextContent(t, result)), &body); err != nil {
			t.Fatalf("parse stand-up result: %v", err)
		}
	}
	return body
}

// runStandupAfterImport holds the consumer at two successive observations of
// the in-flight import. The status is finished only after both observations,
// so a project read before the import finishes is an observable violation.
func runStandupAfterImport(t *testing.T, f *standupFixture, finish func() error) (*mcp.CallToolResult, standupResponse) {
	t.Helper()
	d := f.deps(t)
	heldBootDeadline(&d)
	wake := make(chan struct{})
	d.bootWatch = func(context.Context, string) (<-chan struct{}, error) { return wake, nil }
	read := make(chan struct{}, 1)
	f.importing.onFirstRead = func() { read <- struct{}{} }
	waiting, resume := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	progress := newStandupProgress(func(message string, _, _ float64) {
		if strings.HasPrefix(message, "waiting for the container's import") {
			select {
			case waiting <- struct{}{}:
			case <-ctx.Done():
				return
			}
			select {
			case <-resume:
			case <-ctx.Done():
			}
		}
	})
	progress.status = f.status
	result := make(chan *mcp.CallToolResult, 1)
	go func() { defer close(done); result <- d.run(ctx, progress) }()
	for i := range 2 {
		select {
		case <-waiting:
		case got := <-result:
			t.Fatalf("stand-up returned before waiting for the import: %s", getTextContent(t, got))
		}
		select {
		case <-read:
			t.Error("the stand-up read the project before the import finished")
		default:
		}
		if i == 0 {
			resume <- struct{}{}
			select {
			case wake <- struct{}{}:
			case got := <-result:
				t.Fatalf("stand-up stopped while the import was still running: %s", getTextContent(t, got))
			}
		}
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	resume <- struct{}{}
	select {
	case wake <- struct{}{}:
	case got := <-result:
		t.Fatalf("stand-up stopped before observing the finished import: %s", getTextContent(t, got))
	}
	got := <-result
	select {
	case <-read:
	default:
		t.Error("the stand-up never read the project after the import finished")
	}
	return got, standupBody(t, got)
}

// A status that cannot start importing must return without entering the wait.
func runStandupWithoutImportWait(t *testing.T, f *standupFixture) (*mcp.CallToolResult, standupResponse) {
	t.Helper()
	d := f.deps(t)
	heldBootDeadline(&d)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	waiting := make(chan struct{}, 1)
	progress := newStandupProgress(func(message string, _, _ float64) {
		if strings.HasPrefix(message, "waiting for the container's import") {
			waiting <- struct{}{}
		}
	})
	progress.status = f.status
	result := make(chan *mcp.CallToolResult, 1)
	go func() { defer close(done); result <- d.run(ctx, progress) }()
	var got *mcp.CallToolResult
	select {
	case got = <-result:
	case <-waiting:
		cancel()
		got = <-result
		t.Error("the stand-up waited for an import that was not in flight")
	}
	return got, standupBody(t, got)
}

// TestStandup_BootImportFollowsTheStatusFile: the wait for the container's
// import reads its progress when the importing process puts the status file
// in place, and at no other time: with no time left to a re-read, each change
// reaches the model and the import's end ends the wait.
func TestStandup_BootImportFollowsTheStatusFile(t *testing.T) {
	t.Parallel()
	t.Run("Boot-import progress is read at the status file's change; no 5 s re-read.", func(t *testing.T) {
		t.Parallel()
		f := newStandupFixture(t)
		if err := mate.UpdateStatus(f.statusPath, func(s *mate.Status) {
			s.Runtimes = mate.RuntimesStatus{State: mate.RuntimesImporting, Services: []mate.RuntimeService{
				{Hostname: "medusastage", State: mate.ServiceCreating},
				{Hostname: "nextstorestage", State: mate.ServiceCreating},
			}}
		}); err != nil {
			t.Fatal(err)
		}
		d := f.deps(t)
		heldBootDeadline(&d)
		said := make(chan string, 16)
		progress := newStandupProgress(func(message string, _, _ float64) { said <- message })
		ended := make(chan bootImportResult, 1)
		go func() { ended <- d.awaitBootImport(t.Context(), progress) }()

		if got, want := <-said, "waiting for the container's import of medusastage, nextstorestage"; got != want {
			t.Fatalf("first look said %q, want %q", got, want)
		}
		if err := mate.UpdateStatus(f.statusPath, func(s *mate.Status) { s.Runtimes.Services[0].State = mate.ServiceRunning }); err != nil {
			t.Fatal(err)
		}
		if got, want := <-said, "waiting for the container's import of nextstorestage"; got != want {
			t.Fatalf("the look at the file's change said %q, want %q", got, want)
		}
		if err := mate.UpdateStatus(f.statusPath, func(s *mate.Status) {
			s.Runtimes.State = mate.RuntimesDone
			s.Runtimes.Services[1].State = mate.ServiceRunning
		}); err != nil {
			t.Fatal(err)
		}
		if res := <-ended; res.stopped != nil || len(res.failed) != 0 {
			t.Fatalf("the import's end ended the wait with %+v", res)
		}
	})
}

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
					expire := heldBootDeadline(&d)
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					waited := false
					progress := newStandupProgress(func(message string, _, _ float64) {
						if strings.HasPrefix(message, "waiting for the container's import") {
							waited = true
							if decision.cancel {
								cancel()
							} else {
								expire()
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
