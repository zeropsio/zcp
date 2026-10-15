package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// partsRepos answers the parts-signal read per hostname and records who was read.
type partsRepos struct {
	mu   sync.Mutex
	out  map[string]string
	read []string
}

func (p *partsRepos) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	if !strings.Contains(command, "@@yaml") {
		return nil, nil
	}
	p.mu.Lock()
	p.read = append(p.read, host)
	p.mu.Unlock()
	return []byte(p.out[host]), nil
}

func (p *partsRepos) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// TestAttachPartsSignal: in a container, every dev half's repo is read for the
// multi-part signal — never a stage, a managed service or anything in local
// mode — and only a multi-part repo marks its dev half.
func TestAttachPartsSignal(t *testing.T) {
	t.Parallel()
	threeParts := "@@yaml\nzerops:\n  - setup: dev\n  - setup: web\n  - setup: api\n@@declaration\n@@markers\n@@packages\n"
	onePart := "@@yaml\nzerops:\n  - setup: dev\n  - setup: prod\n@@declaration\n@@markers\n@@packages\n"
	services := func() []workflow.ServiceSnapshot {
		return []workflow.ServiceSnapshot{
			{Hostname: "monodev", Mode: topology.ModeStandard, RuntimeClass: topology.RuntimeDynamic, SetupName: "dev"},
			{Hostname: "webstage", Mode: topology.ModeStage, RuntimeClass: topology.RuntimeDynamic},
			{Hostname: "appdev", Mode: topology.ModeDev, RuntimeClass: topology.RuntimeDynamic, SetupName: "dev"},
			{Hostname: "db", RuntimeClass: topology.RuntimeManaged},
		}
	}
	repos := &partsRepos{out: map[string]string{"monodev": threeParts, "appdev": onePart, "webstage": threeParts}}
	got := services()
	attachPartsSignal(context.Background(), got, repos, runtime.Info{InContainer: true})
	want := map[string]bool{"monodev": true, "webstage": false, "appdev": false, "db": false}
	for _, s := range got {
		if s.MultiPart != want[s.Hostname] {
			t.Errorf("%s: MultiPart = %v, want %v", s.Hostname, s.MultiPart, want[s.Hostname])
		}
	}
	if len(repos.read) != 2 {
		t.Errorf("read %v, want the two dev halves only", repos.read)
	}

	local := &partsRepos{out: repos.out}
	got = services()
	attachPartsSignal(context.Background(), got, local, runtime.Info{})
	if len(local.read) != 0 || got[0].MultiPart {
		t.Errorf("local mode read %v", local.read)
	}
}

// TestMultiPartRouteNote: the route menu names the monorepo playbook only for
// an existing runtime service whose repo is multi-part.
func TestMultiPartRouteNote(t *testing.T) {
	t.Parallel()
	stack := func(name, typ string) platform.ServiceStack {
		return platform.ServiceStack{Name: name, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typ}}
	}
	existing := []platform.ServiceStack{stack("appdev", "nodejs@22"), stack("db", "postgresql@17")}
	multi := "@@yaml\nzerops:\n  - setup: dev\n  - setup: web\n  - setup: api\n@@declaration\n@@markers\n@@packages\n"
	single := "@@yaml\nzerops:\n  - setup: dev\n@@declaration\n@@markers\n@@packages\n"
	rt := runtime.Info{InContainer: true}

	got := multiPartRouteNote(context.Background(), existing, &partsRepos{out: map[string]string{"appdev": multi}}, rt)
	if !strings.Contains(got, "`appdev`") || !strings.Contains(got, `uri="zerops://playbooks/monorepo"`) {
		t.Errorf("multi-part note = %q", got)
	}
	repos := &partsRepos{out: map[string]string{"appdev": single, "db": multi}}
	if got := multiPartRouteNote(context.Background(), existing, repos, rt); got != "" {
		t.Errorf("single-part note = %q, want none", got)
	}
	if len(repos.read) != 1 {
		t.Errorf("read %v, want appdev only", repos.read)
	}
}
