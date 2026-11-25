package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// ActiveVersionReader reads the platform's name of a stage's active version.
type ActiveVersionReader func(context.Context, string, string) (string, error)

const (
	whereVersionEvent = "version"
	whereNoAnswer     = "no answer in 3 s"
)

type whereEvent struct {
	kind, host, value string
	envelope          workflow.StateEnvelope
	ids               map[string]string
	changes           []workflow.HQRepoChanges
	read              ops.DevServerRead
	dirty             bool
	err               error
}

// whereGatherer owns this call's coordinator. Its maps and pending count are
// changed only by the receiving goroutine; readers deliver independent events.
type whereGatherer struct {
	deps        ops.LeaseDeps
	rt          runtime.Info
	alias       string
	now         time.Time
	active      ActiveVersionReader
	events      chan whereEvent
	pending     int
	env         workflow.StateEnvelope
	havePreview bool
	versions    map[string]whereEvent
	branches    map[string]whereEvent
	servers     map[string]ops.DevServerRead
	work        map[string]ops.DevServerRead
	changes     []workflow.HQRepoChanges
}

// WhereAnswer reads the facts for a turn without changing a checkout or a
// supervisor. The caller supplies the shared deadline and the read time.
func WhereAnswer(ctx context.Context, deps ops.LeaseDeps, rt runtime.Info, alias string, now time.Time, active ActiveVersionReader) (string, error) {
	if deps.Client == nil || deps.ProjectID == "" {
		return "", fmt.Errorf("where: no project key or project")
	}
	g := &whereGatherer{
		deps: deps, rt: rt, alias: alias, now: now, active: active,
		events: make(chan whereEvent, 256), pending: 2,
		versions: map[string]whereEvent{}, branches: map[string]whereEvent{},
		servers: map[string]ops.DevServerRead{}, work: map[string]ops.DevServerRead{},
	}
	g.start(ctx)
	for g.pending > 0 {
		select {
		case event := <-g.events:
			if err := g.process(ctx, event); err != nil {
				return "", err
			}
		case <-ctx.Done():
			return g.atDeadline(ctx)
		}
	}
	answer := g.render()
	return answer, nil
}

func (g *whereGatherer) start(ctx context.Context) {
	preview := workflow.WithEnvelopePreview(func(env workflow.StateEnvelope, services []platform.ServiceStack) {
		ids := make(map[string]string, len(services))
		for _, service := range services {
			ids[service.Name] = service.ID
		}
		g.send(ctx, whereEvent{kind: "preview", envelope: env, ids: ids})
	})
	deps := g.deps
	options := leaseEnvelopeOptions(deps.Client, deps.ProjectID, deps.StateDir, g.rt, 3*time.Second,
		workflow.WithLeaseReader(leaseReaderWith(deps.Client, deps.ProjectID, deps.StateDir, deps.Swarm, deps.Host, 3*time.Second)), preview)
	go func() {
		env, err := workflow.ComputeEnvelope(ctx, deps.Client, deps.StateDir, deps.ProjectID, g.rt, g.now, options...)
		g.send(ctx, whereEvent{kind: "envelope", envelope: env, err: err})
	}()
	go func() {
		changes, _, _, _, _ := statusHQChanges(g.rt, deps.StateDir, hqAddress(g.rt), g.now, false)
		g.send(ctx, whereEvent{kind: "changes", changes: changes})
	}()
}

func (g *whereGatherer) send(ctx context.Context, event whereEvent) {
	select {
	case g.events <- event:
		return
	default:
	}
	select {
	case g.events <- event:
	case <-ctx.Done():
	}
}

func (g *whereGatherer) atDeadline(ctx context.Context) (string, error) {
	// Keep every receipt already delivered before the deadline.
	for len(g.events) > 0 {
		if err := g.process(ctx, <-g.events); err != nil {
			return "", err
		}
	}
	if !g.havePreview {
		return "", fmt.Errorf("where: project not read: %w", ctx.Err())
	}
	return g.render(), nil
}

func (g *whereGatherer) render() string {
	return renderWhereAnswer(g.env, g.versions, g.branches, g.servers, g.work, g.changes, g.rt, g.alias, g.now)
}

func (g *whereGatherer) process(ctx context.Context, event whereEvent) error {
	switch event.kind {
	case "preview":
		g.havePreview, g.env = true, event.envelope
		for _, service := range g.env.Services {
			if len(service.PairStageHostnames()) == 0 {
				continue
			}
			for _, host := range service.PairStageHostnames() {
				g.startVersion(ctx, host, event.ids[host])
			}
			if !service.DevIsPool {
				g.startDev(ctx, service.Hostname)
			}
		}
	case "envelope":
		g.pending--
		if event.err != nil {
			return fmt.Errorf("where: platform or local project state could not be read")
		}
		g.env = event.envelope
		for _, lease := range g.env.LeaseList() {
			if ctx.Err() == nil && lease.State == ops.LeaseStateHeld && !lease.Unverified {
				g.startStatus(ctx, lease.Alias, true)
			}
		}
	case "changes":
		g.pending--
		g.changes = event.changes
	case whereVersionEvent:
		g.pending--
		g.versions[event.host] = event
	case "branch":
		g.pending--
		g.branches[event.host] = event
	case "servers":
		g.pending--
		g.servers[event.host] = event.read
	case "work":
		g.pending--
		g.work[event.host] = event.read
	}
	return nil
}

func (g *whereGatherer) startVersion(ctx context.Context, host, id string) {
	if _, exists := g.versions[host]; exists {
		return
	}
	g.versions[host] = whereEvent{value: whereNoAnswer}
	if id == "" || g.active == nil {
		g.versions[host] = whereEvent{value: "stage service not read"}
		return
	}
	if ctx.Err() != nil {
		return
	}
	g.pending++
	go func() {
		name, err := g.active(ctx, g.deps.ProjectID, id)
		g.send(ctx, whereEvent{kind: whereVersionEvent, host: host, value: name, err: err})
	}()
}

func (g *whereGatherer) startDev(ctx context.Context, host string) {
	if ctx.Err() != nil {
		return
	}
	g.startStatus(ctx, host, false)
	if g.deps.SSH == nil {
		g.branches[host] = whereEvent{err: fmt.Errorf("no SSH reader")}
		return
	}
	g.pending++
	root := supervisorRoot(g.deps.StateDir, host)
	if root == "" {
		root = ops.DevRepoRoot
	}
	go func() {
		_, branch, dirty, ok, err := git.HeadStatus(ctx, git.SSHRunner{Executor: g.deps.SSH, Hostname: host}, root)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if !ok && err == nil {
			err = fmt.Errorf("checkout did not answer")
		}
		g.send(ctx, whereEvent{kind: "branch", host: host, value: branch, dirty: dirty, err: err})
	}()
}

func (g *whereGatherer) startStatus(ctx context.Context, host string, lease bool) {
	if ctx.Err() != nil {
		return
	}
	if g.deps.SSH == nil {
		g.servers[host] = ops.DevServerRead{ServersUnread: "no SSH reader"}
		g.work[host] = ops.DevServerRead{WorkUnread: "no SSH reader"}
		return
	}
	deps := g.deps
	g.pending++
	if lease {
		g.pending++
	}
	go func() {
		if ctx.Err() != nil {
			return
		}
		serverRead, checkoutRead := ops.ReadDevServerStatus(ctx, deps.SSH, deps.Client, deps.ProjectID, deps.StateDir, host, supervisorRoot(deps.StateDir, host), lease)
		go g.waitRead(ctx, "servers", host, serverRead)
		if checkoutRead != nil {
			g.waitRead(ctx, "work", host, checkoutRead)
		}
	}()
}

func (g *whereGatherer) waitRead(ctx context.Context, kind, host string, receipt <-chan ops.DevServerRead) {
	select {
	case read := <-receipt:
		g.send(ctx, whereEvent{kind: kind, host: host, read: read})
	case <-ctx.Done():
	}
}

func renderWhereAnswer(env workflow.StateEnvelope, versions, branches map[string]whereEvent, servers, work map[string]ops.DevServerRead, changes []workflow.HQRepoChanges, rt runtime.Info, alias string, now time.Time) string {
	serverStates := make(map[string][]topology.DevServerState, len(servers))
	for host, read := range servers {
		serverStates[host] = read.Servers
	}
	env = env.WithParts(serverStates)
	facts := workflow.WhereFacts{ReadAt: now, Lease: alias}
	repos := map[string]string{}
	for _, service := range env.Services {
		if len(service.WireStages) == 0 {
			continue
		}
		repos[service.Hostname] = service.HQRepo
		for _, stage := range service.WireStages {
			got, ok := versions[stage.Hostname]
			item := workflow.WhereStage{Hostname: stage.Hostname, Part: stage.Part}
			switch {
			case !ok:
				item.Unread = whereNoAnswer
			case got.err != nil:
				item.Unread = "platform version search failed"
				if errors.Is(got.err, context.DeadlineExceeded) || errors.Is(got.err, context.Canceled) {
					item.Unread = whereNoAnswer
				}
			case got.kind != whereVersionEvent:
				item.Unread = got.value
			default:
				item.Version = got.value
			}
			facts.Stages = append(facts.Stages, item)
		}
		if service.DevIsPool {
			continue
		}
		workingCopy := workflow.WhereCopy{Name: service.Hostname, Part: service.WireStages[0].Part, Branch: "not read", HasHQ: service.HQRepo != ""}
		if got, ok := branches[service.Hostname]; ok {
			if got.err != nil {
				workingCopy.WorkUnread, workingCopy.HasHQ = "checkout did not answer", false
				if errors.Is(got.err, context.DeadlineExceeded) || errors.Is(got.err, context.Canceled) {
					workingCopy.WorkUnread = whereNoAnswer
				}
			} else {
				workingCopy.Branch, workingCopy.Dirty = got.value, got.dirty
			}
		} else {
			workingCopy.WorkUnread, workingCopy.HasHQ = whereNoAnswer, false
		}
		workingCopy.DevServers, workingCopy.ServersUnread = whereServers(servers, service.Hostname)
		facts.Copies = append(facts.Copies, workingCopy)
	}
	for _, lease := range env.LeaseList() {
		if lease.State == "unknown-holder" {
			continue
		}
		_, repo, _ := hq.RepoOfURL(hqAddress(rt), lease.Repo)
		workingCopy := workflow.WhereCopy{Name: lease.Alias, Part: lease.Part, Branch: lease.Branch, Lease: true, State: lease.State, Unverified: lease.Unverified, HasHQ: repo != ""}
		repos[lease.Alias] = repo
		if lease.State == ops.LeaseStateHeld && !lease.Unverified {
			if got, ok := work[lease.Alias]; ok {
				workingCopy.Unsaved, workingCopy.WorkUnread = got.Unsaved, whereReason(got.WorkUnread)
			} else {
				workingCopy.WorkUnread = whereNoAnswer
			}
			workingCopy.DevServers, workingCopy.ServersUnread = whereServers(servers, lease.Alias)
		} else {
			workingCopy.ServersUnread = "lease " + lease.State
			if lease.Unverified {
				workingCopy.WorkUnread, workingCopy.ServersUnread = "lease unverified", "lease unverified"
			}
		}
		facts.Copies = append(facts.Copies, workingCopy)
	}
	for _, repo := range changes {
		for _, change := range repo.Changes {
			held := false
			for i := range facts.Copies {
				workingCopy := &facts.Copies[i]
				if repos[workingCopy.Name] == repo.Repo && workingCopy.Branch == change.Branch && change.Branch != "" {
					workingCopy.Change = &change
					held = true
				}
			}
			if !held && change.Open && change.Branch != "" {
				facts.Changes = append(facts.Changes, change)
			}
		}
	}
	return workflow.RenderWhere(facts)
}

func whereServers(reads map[string]ops.DevServerRead, host string) ([]topology.DevServerState, string) {
	read, ok := reads[host]
	if !ok {
		return nil, whereNoAnswer
	}
	return read.Servers, whereReason(string(read.ServersUnread))
}

func whereReason(reason string) string {
	first, _, _ := strings.Cut(reason, "\n")
	return first
}
