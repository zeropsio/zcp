package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
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
	head, repo, root  string
	ref               *topology.BranchRef
	change            *hq.Change
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
	deps         ops.LeaseDeps
	rt           runtime.Info
	alias        string
	now          time.Time
	active       ActiveVersionReader
	events       chan whereEvent
	pending      int
	env          workflow.StateEnvelope
	havePreview  bool
	haveEnvelope bool
	versions     map[string]whereEvent
	branches     map[string]whereEvent
	servers      map[string]ops.DevServerRead
	work         map[string]ops.DevServerRead
	changes      []workflow.HQRepoChanges
	refs         map[string]whereEvent
	hqReads      map[string]whereEvent
}

// WhereAnswer reads the facts for a turn without changing a checkout or a
// supervisor. The caller supplies the shared deadline and the read time.
func WhereAnswer(ctx context.Context, deps ops.LeaseDeps, rt runtime.Info, alias string, now time.Time, active ActiveVersionReader) (string, error) {
	g := newWhereGatherer(deps, rt, alias, now, active)
	if err := g.gather(ctx); err != nil {
		return "", err
	}
	return g.render(), nil
}

func newWhereGatherer(deps ops.LeaseDeps, rt runtime.Info, alias string, now time.Time, active ActiveVersionReader) *whereGatherer {
	if deps.Client == nil || deps.ProjectID == "" {
		return nil
	}
	return &whereGatherer{
		deps: deps, rt: rt, alias: alias, now: now, active: active,
		events: make(chan whereEvent, 256), pending: 2,
		versions: map[string]whereEvent{}, branches: map[string]whereEvent{},
		servers: map[string]ops.DevServerRead{}, work: map[string]ops.DevServerRead{},
		refs: map[string]whereEvent{}, hqReads: map[string]whereEvent{},
	}
}

func (g *whereGatherer) gather(ctx context.Context) error {
	if g == nil {
		return fmt.Errorf("where: no project key or project")
	}
	g.start(ctx)
	return g.receive(ctx)
}

// gatherEdit uses the caller's already-read project and leases, then reads
// the checkout and change facts that the edit step needs.
func (g *whereGatherer) gatherEdit(ctx context.Context, env workflow.StateEnvelope) error {
	if g == nil {
		return fmt.Errorf("where: no project key or project")
	}
	g.pending = 0
	g.havePreview, g.haveEnvelope, g.env = true, true, env
	g.changes = env.HQChanges
	for _, service := range env.Services {
		if len(service.PairStageHostnames()) > 0 && !service.DevIsPool {
			g.startDev(ctx, service.Hostname, service.HQRepo)
		}
	}
	g.startLeases(ctx)
	return g.receive(ctx)
}

func (g *whereGatherer) receive(ctx context.Context) error {
	for g.pending > 0 {
		select {
		case event := <-g.events:
			if err := g.process(ctx, event); err != nil {
				return err
			}
		case <-ctx.Done():
			return g.atDeadline(ctx)
		}
	}
	return nil
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

func (g *whereGatherer) atDeadline(ctx context.Context) error {
	// Keep every receipt already delivered before the deadline.
	for len(g.events) > 0 {
		if err := g.process(ctx, <-g.events); err != nil {
			return err
		}
	}
	if !g.havePreview {
		return fmt.Errorf("where: project not read: %w", ctx.Err())
	}
	return nil
}

func (g *whereGatherer) render() string {
	return renderWhereAnswer(g.env, g.versions, g.branches, g.servers, g.work, g.changes, g.rt, g.alias, g.now, g.refs, g.hqReads)
}

func (g *whereGatherer) facts() workflow.WhereFacts {
	return whereFacts(g.env, g.versions, g.branches, g.servers, g.work, g.changes, g.rt, g.alias, g.now, g.refs, g.hqReads)
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
				g.startDev(ctx, service.Hostname, service.HQRepo)
			}
		}
	case "envelope":
		g.pending--
		if event.err != nil {
			return fmt.Errorf("where: platform or local project state could not be read")
		}
		g.haveEnvelope, g.env = true, event.envelope
		g.startLeases(ctx)
	case "changes":
		g.pending--
		g.changes = event.changes
	case whereVersionEvent:
		g.pending--
		g.versions[event.host] = event
	case "branch":
		g.pending--
		g.branches[event.host] = event
		if event.err == nil && g.rt.MateEnabled && event.repo != "" && event.value != "" && event.value != hqBase && event.head != "" {
			g.startEditReads(ctx, event)
		}
	case "ref":
		g.pending--
		g.refs[event.host] = event
	case "hq":
		g.pending--
		g.hqReads[event.host] = event
	case "servers":
		g.pending--
		g.servers[event.host] = event.read
	case "work":
		g.pending--
		g.work[event.host] = event.read
	}
	return nil
}

func (g *whereGatherer) startLeases(ctx context.Context) {
	for _, lease := range g.env.LeaseList() {
		if ctx.Err() == nil && lease.State == ops.LeaseStateHeld && !lease.Unverified {
			g.startStatus(ctx, lease.Alias, true)
			_, repo, _ := hq.RepoOfURL(hqAddress(g.rt), lease.Repo)
			if g.rt.MateEnabled && repo != "" {
				g.startHead(ctx, lease.Alias, ops.LeaseWorkDir, repo)
			}
		}
	}
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

func (g *whereGatherer) startDev(ctx context.Context, host, repo string) {
	if ctx.Err() != nil {
		return
	}
	g.startStatus(ctx, host, false)
	root := supervisorRoot(g.deps.StateDir, host)
	if root == "" {
		root = ops.DevRepoRoot
	}
	g.startHead(ctx, host, root, repo)
}

// startHead reads the actual root checkout, including its branch and commit.
func (g *whereGatherer) startHead(ctx context.Context, host, root, repo string) {
	if g.deps.SSH == nil {
		g.branches[host] = whereEvent{err: fmt.Errorf("no SSH reader")}
		return
	}
	g.pending++
	go func() {
		var head, branch string
		var dirty bool
		var err error
		if g.rt.MateEnabled && repo != "" {
			head, branch, dirty, err = ops.ReadEditHead(ctx, g.deps.SSH, host, root)
		} else {
			var ok bool
			head, branch, dirty, ok, err = git.HeadStatus(ctx, git.SSHRunner{Executor: g.deps.SSH, Hostname: host}, root)
			if !ok && err == nil {
				err = errors.New("checkout did not answer")
			}
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		g.send(ctx, whereEvent{kind: "branch", host: host, value: branch, head: head, repo: repo, root: root, dirty: dirty, err: err})
	}()
}

// startEditReads asks both owners once, in parallel under where's deadline.
func (g *whereGatherer) startEditReads(ctx context.Context, workcopy whereEvent) {
	if ctx.Err() != nil {
		return
	}
	g.pending += 2
	go func() {
		ref, err := ops.ReadBranchRef(ctx, g.deps.SSH, workcopy.host, workcopy.root, workcopy.value, workcopy.head)
		g.send(ctx, whereEvent{kind: "ref", host: workcopy.host, ref: &ref, err: err})
	}()
	go func() {
		client, ok := openHQ(http.DefaultClient, g.rt)
		if !ok {
			g.send(ctx, whereEvent{kind: "hq", host: workcopy.host, err: errors.New("HQ was not read")})
			return
		}
		change, err := client.OpenChangeOnBranch(ctx, workcopy.repo, workcopy.value)
		g.send(ctx, whereEvent{kind: "hq", host: workcopy.host, change: change, err: err})
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

func renderWhereAnswer(env workflow.StateEnvelope, versions, branches map[string]whereEvent, servers, work map[string]ops.DevServerRead, changes []workflow.HQRepoChanges, rt runtime.Info, alias string, now time.Time, refs, hqReads map[string]whereEvent) string {
	return workflow.RenderWhere(whereFacts(env, versions, branches, servers, work, changes, rt, alias, now, refs, hqReads))
}

func whereFacts(env workflow.StateEnvelope, versions, branches map[string]whereEvent, servers, work map[string]ops.DevServerRead, changes []workflow.HQRepoChanges, rt runtime.Info, alias string, now time.Time, refs, hqReads map[string]whereEvent) workflow.WhereFacts {
	serverStates := make(map[string][]topology.DevServerState, len(servers))
	for host, read := range servers {
		serverStates[host] = read.Servers
	}
	// Match the part named by the ordinary status envelope. Fresh server
	// reads below still describe each server, but do not rename a paired part.
	withServers := env.WithParts(serverStates)
	env = env.WithParts(nil)
	if env.Leases != nil {
		for i := range *env.Leases {
			if (*env.Leases)[i].Part == "" {
				(*env.Leases)[i].Part = (*withServers.Leases)[i].Part
			}
		}
	}
	facts := workflow.WhereFacts{ReadAt: now, Lease: alias, EditSteps: rt.MateEnabled}
	repos := map[string]string{}
	classicDevs := map[string]string{}
	unreadClassic := map[string]string{}
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
		if service.HQRepo != "" && (classicDevs[service.HQRepo] == "" || service.Hostname < classicDevs[service.HQRepo]) {
			classicDevs[service.HQRepo] = service.Hostname
		}
		workingCopy := workflow.WhereCopy{Name: service.Hostname, Part: service.WireStages[0].Part, Branch: "not read", HasHQ: service.HQRepo != ""}
		if got, ok := branches[service.Hostname]; ok {
			workingCopy.Path = editCheckoutPath(service.Hostname, got.root)
			if got.err != nil {
				workingCopy.WorkUnread = "checkout did not answer"
				if errors.Is(got.err, context.DeadlineExceeded) || errors.Is(got.err, context.Canceled) {
					workingCopy.WorkUnread = whereNoAnswer
				}
			} else {
				workingCopy.Branch, workingCopy.Dirty = got.value, got.dirty
			}
		} else {
			workingCopy.WorkUnread = whereNoAnswer
		}
		if workingCopy.WorkUnread != "" && service.HQRepo != "" && (unreadClassic[service.HQRepo] == "" || service.Hostname < unreadClassic[service.HQRepo]) {
			unreadClassic[service.HQRepo] = service.Hostname
		}
		workingCopy.DevServers, workingCopy.ServersUnread = whereServers(servers, service.Hostname)
		facts.Copies = append(facts.Copies, workingCopy)
	}
	for _, lease := range env.LeaseList() {
		if lease.State == "unknown-holder" {
			continue
		}
		_, repo, _ := hq.RepoOfURL(hqAddress(rt), lease.Repo)
		workingCopy := workflow.WhereCopy{Name: lease.Alias, Pool: lease.Pool, Path: editCheckoutPath(lease.Alias, ops.LeaseWorkDir), Part: lease.Part, Branch: lease.Branch, Lease: true, State: lease.State, Unverified: lease.Unverified, HasHQ: repo != ""}
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
	if rt.MateEnabled {
		planWhereEdits(&facts, repos, branches, refs, hqReads, now)
	}
	classicCopies := map[string]*workflow.WhereCopy{}
	for i := range facts.Copies {
		workcopy := &facts.Copies[i]
		repo := repos[workcopy.Name]
		if !workcopy.Lease && classicDevs[repo] == workcopy.Name {
			classicCopies[repo] = workcopy
		}
	}
	for _, repo := range changes {
		for _, change := range repo.Changes {
			held := false
			for i := range facts.Copies {
				workingCopy := &facts.Copies[i]
				if repos[workingCopy.Name] == repo.Repo && workingCopy.Branch == change.Branch && change.Branch != "" {
					if !rt.MateEnabled || repos[workingCopy.Name] == "" {
						workingCopy.Change = &change
					}
					held = true
				}
			}
			if !held && change.Open && change.Branch != "" {
				item := workflow.WhereChange{HQChangeLine: change, Step: workflow.MissingEditCopyStep(change.Branch, classicCopies[repo.Repo])}
				if host := unreadClassic[repo.Repo]; host != "" {
					item.Unread = "checkout " + host + " branch was not read"
					item.Step = workflow.EditStep{Action: workflow.EditUnknown, Reason: item.Unread}
				}
				facts.Changes = append(facts.Changes, item)
			}
		}
	}
	return facts
}

// editCheckoutPath translates a remote checkout root to its service mount.
func editCheckoutPath(host, root string) string {
	if root != ops.DevRepoRoot && !strings.HasPrefix(root, ops.DevRepoRoot+"/") {
		return ""
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(root, ops.DevRepoRoot), "/")
	return path.Join(ops.DevRepoRoot, host, rel)
}

// planWhereEdits keeps each copy's own reads separate; no last-heard HQ
// state can establish the change or its absence for this call.
func planWhereEdits(facts *workflow.WhereFacts, repos map[string]string, branches, refs, hqReads map[string]whereEvent, now time.Time) {
	inputs := make(map[string]workflow.EditCopy, len(facts.Copies))
	for i := range facts.Copies {
		workcopy := &facts.Copies[i]
		if !workcopy.HasHQ {
			continue
		}
		input := workflow.EditCopy{Name: workcopy.Name}
		branch, read := branches[workcopy.Name]
		if workcopy.Branch == hqBase && read && branch.err == nil && branch.value == hqBase {
			continue
		}
		switch {
		case workcopy.Unverified:
			input.Unread = "lease was not verified"
		case workcopy.Lease && workcopy.State != ops.LeaseStateHeld:
			input.Unread = "lease checkout was not read"
		case !read || branch.err != nil:
			input.Unread = "checkout head and branch were not read"
		case branch.value == "":
			input.Unread = "checkout is not on a branch"
		case branch.head == "":
			input.Unread = "checkout has no commit"
		case workcopy.Lease && branch.value != workcopy.Branch:
			input.Unread = "checkout branch differs from the lease"
		case workcopy.WorkUnread != "":
			input.Unread = "working copy was not read"
		default:
			input.Dirty = branch.dirty
			ref, found := refs[workcopy.Name]
			if found && ref.err == nil {
				input.Ref = ref.ref
			} else {
				input.Unread = "branch remote ref or checkout history was not read"
			}
		}
		workcopy.BranchRef = input.Ref
		inputs[workcopy.Name] = input
	}
	for i := range facts.Copies {
		workcopy := &facts.Copies[i]
		input, planned := inputs[workcopy.Name]
		if !planned {
			continue
		}
		answer, answered := hqReads[workcopy.Name]
		fresh := answered && answer.err == nil
		var change *workflow.EditChange
		if fresh && answer.change != nil {
			current := answer.change
			change = editChange(current, repos[workcopy.Name])
			told := workflow.HQChangeTold{Ready: current.Ready, Mergeability: current.Mergeability, Behind: current.Behind}
			if current.PullRequest != nil {
				told.Checks = current.PullRequest.Checks
			}
			reviewBuildAsTold(&told, current.ReviewBuild, current.ChecksGivenUp != nil)
			workcopy.Change = &workflow.HQChangeLine{Number: current.Number, Branch: current.Branch, Labels: changeLabels(&told, now), Open: true}
		} else if !fresh {
			workcopy.HasHQ = false // The last-heard state cannot establish absence.
		}
		group := []workflow.EditCopy{input}
		if workcopy.Lease {
			for _, other := range facts.Copies {
				if !other.Lease || other.Name == workcopy.Name || repos[other.Name] != repos[workcopy.Name] || other.Branch != workcopy.Branch {
					continue
				}
				otherInput, exists := inputs[other.Name]
				if !exists {
					otherInput = workflow.EditCopy{Name: other.Name, Unread: "checkout was not read"}
				}
				group = append(group, otherInput)
			}
		}
		step := workflow.PlanEdit(group, change, fresh, true)
		workcopy.Step = &step
	}
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
