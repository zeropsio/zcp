package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// MatePreviewRequest identifies the copy whose Draft is being opened.
type MatePreviewRequest struct {
	Lease, AcquiredAt, Service, Branch string
}

// MatePreviewAnswer is the on-demand answer to the Mate, not an observation
// stored in its status file.
type MatePreviewAnswer struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Reason string `json:"reason,omitempty"`
}

const previewDraftEnded = "draft ended"

func previewFailure(reason string) MatePreviewAnswer {
	return MatePreviewAnswer{Status: "failed", Reason: reason}
}

// EnsureMatePreview starts the copy's preview server if needed and answers
// only after the supervisor reports it running at a browser address.
func EnsureMatePreview(ctx context.Context, d ops.LeaseDeps, httpClient ops.HTTPDoer, req MatePreviewRequest) MatePreviewAnswer {
	if req.Branch == "" || (req.Lease == "") == (req.Service == "") || (req.Lease != "" && req.AcquiredAt == "") || (req.Service != "" && req.AcquiredAt != "") {
		return previewFailure("invalid draft identity")
	}
	host := req.Service
	if req.Lease != "" {
		host = req.Lease
	}
	root := supervisorRoot(d.StateDir, host)
	if root == "" {
		root = ops.DevRepoRoot
	}
	lease, reason := previewCopy(ctx, d, req, host, root)
	if reason != "" {
		return previewFailure(reason)
	}
	ds, err := ops.ReadDevServers(ctx, d.SSH, host, root)
	reportPreviewCopy(ctx, d, req, lease, host, ds)
	var noSupervisor *ops.NoSupervisorError
	noSupervisorForLease := req.Lease != "" && ds != nil && errors.As(err, &noSupervisor)
	if ds == nil || ds.DeclarationErr != nil || (err != nil && !noSupervisorForLease) {
		return previewFailure("could not read the draft's preview server")
	}
	ds.Service = lease.Pool
	server, ok := devsup.PreviewServer(ds.Servers())
	if !ok {
		return previewFailure("no preview server is declared for this draft")
	}
	state := ds.State.Servers[server.Name]
	address := ""
	started := false
	result := &ops.DevServerResult{Hostname: host, Name: server.Name, Pool: lease.Pool, Port: server.Port,
		PreviewPort: state.PreviewPort, Running: state.Status == devsup.StatusRunning, Read: ds}
	if !result.Running {
		if _, reason = previewCopy(ctx, d, req, host, root); reason != "" {
			return previewFailure(reason)
		}
		started = true
		if state.Status == devsup.StatusStarting && ds.Supervisor.Supports(devsup.CapabilityWait) {
			started = false
			result, reason, err = awaitPreviewServer(ctx, d, req, ds, server, lease.Pool, root)
		} else {
			result, err = ops.ExecuteDevServer(ctx, d.SSH, d.Client, d.ProjectID, ops.DevServerParams{
				Action: "start", Hostname: host, Name: server.Name, Root: root, StateDir: d.StateDir,
				WaitSeconds: int(devsup.ListenWait / time.Second),
			})
			if err == nil && !result.Running && result.Reason == string(devsup.StatusStarting) && result.Read != nil && result.Read.Supervisor.Supports(devsup.CapabilityWait) {
				if _, reason = previewCopy(ctx, d, req, host, root); reason != "" {
					return previewFailure(reason)
				}
				result, reason, err = awaitPreviewServer(ctx, d, req, result.Read, server, lease.Pool, root)
			}
		}
		if result != nil {
			reportPreviewCopy(ctx, d, req, lease, host, result.Read)
		}
		if reason != "" {
			return previewFailure(reason)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return previewFailure("preview server did not start in time")
		}
		if _, reason = previewCopy(ctx, d, req, host, root); reason != "" {
			return previewFailure(reason)
		}
		if err != nil {
			return previewFailure("could not start the draft's preview server")
		}
		if !result.Running {
			if result.Reason == string(devsup.StatusStarting) {
				return previewFailure("preview server did not start in time")
			}
			if result.Reason != "" {
				return previewFailure("preview server: " + strings.Join(strings.Fields(strings.ReplaceAll(result.Reason, "_", " ")), " "))
			}
			return previewFailure("preview server did not start")
		}
		if result.Reason == ops.ReasonLoopbackOnly {
			return previewFailure("preview server listens only on loopback")
		}
	}
	current, ok := devsup.PreviewServer(result.Read.Servers())
	if result.Read.DeclarationErr != nil || !ok || current.Name != server.Name || current.Port != server.Port {
		return previewFailure("the draft's preview server changed while opening")
	}
	if result.Read.LoopbackOnly[current.Port] {
		return previewFailure("preview server listens only on loopback")
	}
	if started {
		access := ensureDevServerPublicAccess(ctx, d.Client, httpClient, d.ProjectID, d.StateDir, host, result)
		result.PreviewURL = access.SubdomainURL
		address = access.SubdomainURL
	}
	if address == "" {
		for _, observed := range ops.DevServerStates(ctx, d.Client, d.ProjectID, result) {
			if observed.Name == server.Name && observed.URL != "" {
				address = observed.URL
				break
			}
		}
	}
	if address == "" {
		return previewFailure("the draft's preview address is unavailable")
	}
	if _, reason = previewCopy(ctx, d, req, host, root); reason != "" {
		return previewFailure(reason)
	}
	return MatePreviewAnswer{Status: "running", URL: address}
}

func reportPreviewCopy(ctx context.Context, d ops.LeaseDeps, req MatePreviewRequest, lease ops.LeaseTargetInfo, host string, read *ops.DevServers) {
	if !runtime.DetectFrom(mate.LiveLookup(mate.LiveEnvStorePath)).MateEnabled {
		return
	}
	target := ops.MateCopyTarget{Hostname: host}
	if req.Lease != "" {
		target.Pool, target.Branch, target.AcquiredAt = lease.Pool, req.Branch, req.AcquiredAt
	}
	if err := ops.ReportMateCopy(ctx, d, mate.StatusFilePath(), target, read); err != nil {
		logMateCopyError("report preview copy", err)
	}
}

// awaitPreviewServer uses the supervisor's existing start receipt, then reads
// the listener and address facts that can have changed while it waited.
func awaitPreviewServer(ctx context.Context, d ops.LeaseDeps, req MatePreviewRequest, ds *ops.DevServers, server devsup.Server, pool, root string) (*ops.DevServerResult, string, error) {
	if err := ops.WaitDevServer(ctx, d.SSH, ds, server.Name, int(devsup.ListenWait/time.Second)); err != nil {
		return nil, "", fmt.Errorf("wait for preview server: %w", err)
	}
	if _, reason := previewCopy(ctx, d, req, ds.Hostname, root); reason != "" {
		return nil, reason, nil
	}
	ds, err := ops.ReadDevServers(ctx, d.SSH, ds.Hostname, root)
	if err != nil {
		return nil, "", fmt.Errorf("read waited preview server: %w", err)
	}
	ds.Service = pool
	state := ds.State.Servers[server.Name]
	reason := ""
	if state.Status != devsup.StatusRunning {
		reason = string(state.Status)
	}
	return &ops.DevServerResult{Hostname: ds.Hostname, Name: server.Name, Pool: pool, Port: server.Port,
		PreviewPort: state.PreviewPort, Running: state.Status == devsup.StatusRunning, Reason: reason, Read: ds}, "", nil
}

func previewLeaseMatches(lease ops.LeaseTargetInfo, req MatePreviewRequest) bool {
	return lease.AcquiredAt == req.AcquiredAt && lease.Branch == req.Branch
}

// previewCopy checks the identity at each boundary where the call could
// reach a copy or hand its address back after a wait.
func previewCopy(ctx context.Context, d ops.LeaseDeps, req MatePreviewRequest, host, root string) (ops.LeaseTargetInfo, string) {
	if req.Lease != "" {
		lease, ok, err := ops.LeaseTarget(d.StateDir, host)
		if err != nil {
			return lease, "could not read the draft's lease"
		}
		if !ok || !previewLeaseMatches(lease, req) {
			return lease, previewDraftEnded
		}
		lease, ok, err = ops.LeaseTargetChecked(ctx, d, host, "opened")
		if ops.IsLeaseLost(err) {
			return lease, previewDraftEnded
		}
		if err != nil {
			return lease, "could not verify the draft's lease"
		}
		if !ok || !previewLeaseMatches(lease, req) {
			return lease, previewDraftEnded
		}
		if lease.Parked {
			return lease, "the draft's container is stopped"
		}
		return lease, ""
	}
	meta, err := workflow.FindServiceMeta(d.StateDir, host)
	if err != nil {
		return ops.LeaseTargetInfo{}, "could not read the draft's service"
	}
	if meta == nil || !meta.HasStage() || meta.DevPool != nil {
		return ops.LeaseTargetInfo{}, previewDraftEnded
	}
	_, branch, _, err := ops.ReadEditHead(ctx, d.SSH, host, root)
	if err != nil {
		return ops.LeaseTargetInfo{}, "could not read the draft's branch"
	}
	if branch != req.Branch {
		return ops.LeaseTargetInfo{}, previewDraftEnded
	}
	return ops.LeaseTargetInfo{}, ""
}

// KeepMateCopies keeps the Mate-wide projection beside the supervised server.
// File replacement is the event; there is no periodic liveness check.
func KeepMateCopies(ctx context.Context, env func() func(string) string, statusPath string, log func(string)) {
	rt := runtime.DetectFrom(env())
	if !rt.InContainer || !rt.MateEnabled {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stateDir := runtime.ContainerStateDir
	servicesDir := filepath.Join(stateDir, "services")
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		log("copy observer cannot open service metadata")
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log("copy observer cannot watch service metadata")
		return
	}
	defer watcher.Close()
	if err := watcher.Add(servicesDir); err != nil {
		log("copy observer cannot watch service metadata")
		return
	}
	changed, err := hq.WatchReplacedFile(ctx, statusPath)
	if err != nil {
		log("copy observer cannot watch Mate status")
		return
	}
	if err := ops.PublishMateLeases(stateDir, statusPath); err != nil {
		log("copy observer cannot publish leases")
	}
	if err := reconcileMateDevServices(stateDir, statusPath); err != nil {
		log("copy observer cannot publish development services")
	}
	if err := ops.MarkMateCopiesUnknown(statusPath, time.Now().UTC()); err != nil {
		log("copy observer cannot mark its lost links")
	}
	o := &mateCopyObserver{stateDir: stateDir, statusPath: statusPath, log: log,
		deps: liveDevGitDeps(env, log), ssh: platform.NewSystemSSHDeployer(),
		done: make(chan mateCopyWaited, 64), scanDone: make(chan struct{}, 1),
		active: map[mateCopyWaitKey]mateCopyActive{}, seen: map[mateCopyWaitKey]string{}}
	o.bootLeases(ctx)
	o.scan(ctx)
	o.observe(ctx)
	o.run(ctx, watcher, changed)
}

type mateCopyWaitKey struct{ host, acquired, branch string }
type mateCopyWaited struct {
	key    mateCopyWaitKey
	target ops.MateCopyTarget
}
type mateCopyActive struct {
	at     string
	cancel context.CancelFunc
}

// Only run's goroutine changes active and the scan flags.
type mateCopyObserver struct {
	stateDir, statusPath string
	log                  func(string)
	deps                 func(context.Context) (devGitDeps, bool)
	ssh                  ops.SSHDeployer
	done                 chan mateCopyWaited
	scanDone             chan struct{}
	active               map[mateCopyWaitKey]mateCopyActive
	seen                 map[mateCopyWaitKey]string
	scanning, scanAgain  bool
}

func (o *mateCopyObserver) run(ctx context.Context, watcher *fsnotify.Watcher, changed <-chan struct{}) {
	defer func() {
		for _, active := range o.active {
			active.cancel()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-changed:
			if !ok {
				o.log("copy observer lost its status watch")
				return
			}
			o.observe(ctx)
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Ext(event.Name) == ".json" && event.Op&(fsnotify.Create|fsnotify.Rename|fsnotify.Remove|fsnotify.Write) != 0 {
				o.refresh(ctx)
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				o.log("copy observer lost its service metadata watch")
				return
			}
			o.log("copy observer service metadata watch lost events")
		case result := <-o.done:
			o.finishWait(ctx, result)
		case <-o.scanDone:
			o.scanning = false
			if o.scanAgain {
				o.scanAgain = false
				o.scan(ctx)
			}
		}
	}
}

func (o *mateCopyObserver) finishWait(ctx context.Context, result mateCopyWaited) {
	delete(o.active, result.key)
	if result.target.State != "" {
		if d, ok := o.deps(ctx); ok {
			o.startWait(ctx, result.key, result.target, d)
		}
	}
	o.observe(ctx)
}

func (o *mateCopyObserver) refresh(ctx context.Context) {
	if err := reconcileMateDevServices(o.stateDir, o.statusPath); err != nil {
		o.log("copy observer cannot publish development services")
		return
	}
	o.scan(ctx)
}

func (o *mateCopyObserver) scan(ctx context.Context) {
	if o.scanning {
		o.scanAgain = true
		return
	}
	o.scanning = true
	d, _ := o.deps(ctx)
	go func() {
		lookMateDevServices(ctx, o.stateDir, o.statusPath, o.ssh, d.client, d.rt.ProjectID, o.log)
		select {
		case o.scanDone <- struct{}{}:
		case <-ctx.Done():
		}
	}()
}

// bootLeases reads each held lease once after publishing unknown.
func (o *mateCopyObserver) bootLeases(ctx context.Context) {
	status, err := mate.ReadStatus(o.statusPath)
	if err != nil || status.Copies == nil {
		return
	}
	d, ok := o.deps(ctx)
	if !ok {
		return
	}
	for _, lease := range status.Copies.Leases {
		target := ops.MateCopyTarget{Hostname: lease.Alias, Pool: lease.Pool, Branch: lease.Branch, AcquiredAt: lease.AcquiredAt, State: lease.State, StateAt: lease.StateAt}
		go func() {
			ld := LeaseDepsFor(d.client, d.rt.ProjectID, o.stateDir, ops.NewSwarmFactory(&http.Client{Timeout: ops.LeaseCallTimeout}), nil, d.ssh, d.rt)
			info, exists, checkErr := ops.LeaseTargetChecked(ctx, ld, target.Hostname, "observed")
			if checkErr != nil || !exists || info.AcquiredAt != target.AcquiredAt || info.Branch != target.Branch {
				return
			}
			if info.Parked {
				if err := ops.StoreMateCopyEvent(o.stateDir, o.statusPath, target, mate.CopyEvent{Kind: mate.CopyPark}, time.Now().UTC(), ""); err != nil {
					o.log("copy observer cannot store parked lease")
				}
				return
			}
			read, _ := ops.ReadDevServers(ctx, d.ssh, target.Hostname, supervisorRoot(o.stateDir, target.Hostname))
			if err := ops.ReportMateCopy(ctx, ld, o.statusPath, target, read); err != nil {
				o.log("copy observer cannot read lease state")
			}
		}()
	}
}

func (o *mateCopyObserver) observe(ctx context.Context) {
	status, err := mate.ReadStatus(o.statusPath)
	if err != nil || status.Copies == nil {
		return
	}
	var d devGitDeps
	var haveDeps, checked bool
	present := map[mateCopyWaitKey]bool{}
	start := func(target ops.MateCopyTarget) {
		key := mateCopyWaitKey{target.Hostname, target.AcquiredAt, target.Branch}
		present[key] = true
		active, running := o.active[key]
		if target.State == mate.CopyUnknown || target.State == mate.CopyParked || target.State == mate.CopyGone || target.State == "" {
			if running {
				active.cancel()
			}
			return
		}
		if running {
			if active.at != target.StateAt {
				active.cancel()
			}
			return
		}
		if o.seen[key] == target.StateAt {
			return
		}
		if !checked {
			d, haveDeps = o.deps(ctx)
			checked = true
		}
		if haveDeps {
			o.startWait(ctx, key, target, d)
		}
	}
	for _, v := range status.Copies.Leases {
		start(ops.MateCopyTarget{Hostname: v.Alias, Pool: v.Pool, Branch: v.Branch, AcquiredAt: v.AcquiredAt, State: v.State, StateAt: v.StateAt})
	}
	for _, v := range status.Copies.DevServices {
		start(ops.MateCopyTarget{Hostname: v.Hostname, State: v.State, StateAt: v.StateAt})
	}
	for key, active := range o.active {
		if !present[key] {
			active.cancel()
		}
	}
	for key := range o.seen {
		if !present[key] {
			delete(o.seen, key)
		}
	}
}

func (o *mateCopyObserver) startWait(ctx context.Context, key mateCopyWaitKey, target ops.MateCopyTarget, d devGitDeps) {
	waitCtx, cancel := context.WithCancel(ctx)
	o.active[key] = mateCopyActive{target.StateAt, cancel}
	o.seen[key] = target.StateAt
	go func() {
		defer cancel()
		ld := LeaseDepsFor(d.client, d.rt.ProjectID, o.stateDir, ops.NewSwarmFactory(&http.Client{Timeout: ops.LeaseCallTimeout}), nil, d.ssh, d.rt)
		root := supervisorRoot(o.stateDir, target.Hostname)
		next, err := ops.AwaitMateServer(waitCtx, ld, o.statusPath, root, target)
		result := mateCopyWaited{key: key}
		if err != nil && waitCtx.Err() == nil {
			o.log("copy observer could not wait for a dev server")
			if next != "" {
				target.State = next
				result.target = target
			}
		}
		select {
		case o.done <- result:
		case <-ctx.Done():
		}
	}()
}

// reconcileMateDevServices replaces the set without losing observations of
// copies that remain in it. Service metadata is read under the status lock.
func reconcileMateDevServices(stateDir, statusPath string) error {
	return mate.UpdateStatusChecked(statusPath, func(s *mate.Status) error {
		metas, err := workflow.ListServiceMetas(stateDir)
		if err != nil {
			return fmt.Errorf("read dev services: %w", err)
		}
		hosts := map[string]bool{}
		for _, meta := range metas {
			if meta.HasStage() && meta.DevPool == nil {
				hosts[meta.Hostname] = true
				for _, host := range meta.CodeVolume.DevServiceHostnames() {
					hosts[host] = true
				}
			}
		}
		if s.Copies == nil {
			s.Copies = &mate.CopyStatus{}
		}
		old := map[string]mate.DevCopyStatus{}
		for _, dev := range s.Copies.DevServices {
			old[dev.Hostname] = dev
		}
		names := make([]string, 0, len(hosts))
		for host := range hosts {
			if host != "" {
				names = append(names, host)
			}
		}
		sort.Strings(names)
		devs := make([]mate.DevCopyStatus, 0, len(names))
		for _, host := range names {
			dev := old[host]
			dev.Hostname = host
			if dev.State == "" {
				dev.State, _ = mate.NextCopyState("", mate.CopyEvent{Kind: mate.CopyRecorded})
				dev.StateAt = time.Now().UTC().Format(time.RFC3339Nano)
			}
			devs = append(devs, dev)
		}
		s.Copies.DevServices = devs
		return nil
	})
}

func lookMateDevServices(ctx context.Context, stateDir, statusPath string, ssh ops.SSHDeployer, client platform.Client, projectID string, log func(string)) {
	status, err := mate.ReadStatus(statusPath)
	if err != nil || status.Copies == nil {
		return
	}
	for _, dev := range status.Copies.DevServices {
		lookCtx, cancel := context.WithTimeout(ctx, devGitSSHWithin)
		root := supervisorRoot(stateDir, dev.Hostname)
		if root == "" {
			root = ops.DevRepoRoot
		}
		_, branch, _, branchErr := ops.ReadEditHead(lookCtx, ssh, dev.Hostname, root)
		branchAt := time.Now().UTC()
		read, serverErr := ops.ReadDevServers(lookCtx, ssh, dev.Hostname, root)
		var servers *[]topology.DevServerState
		var serversAt time.Time
		if serverErr == nil {
			states := ops.DevServerStates(lookCtx, client, projectID, &ops.DevServerResult{Hostname: dev.Hostname, Read: read})
			servers = &states
			serversAt = read.ReadAt
		}
		cancel()
		var branchRead *string
		if branchErr == nil {
			branchRead = &branch
		}
		if branchErr != nil || serverErr != nil {
			log("copy observer could not read every fact for " + dev.Hostname)
		}
		ld := ops.LeaseDeps{StateDir: stateDir, SSH: ssh, Client: client, ProjectID: projectID}
		if err := ops.ReportMateCopy(ctx, ld, statusPath, ops.MateCopyTarget{Hostname: dev.Hostname}, read); err != nil {
			log("copy observer could not report " + dev.Hostname)
		}
		if err := storeClassicLook(statusPath, dev.Hostname, branchRead, branchAt, servers, serversAt); err != nil {
			log("copy observer could not store the observation for " + dev.Hostname)
		}
	}
}

func storeClassicServers(stateDir, statusPath, host string, servers []topology.DevServerState, at time.Time) error {
	meta, err := workflow.FindServiceMeta(stateDir, host)
	if err != nil {
		return fmt.Errorf("find dev service: %w", err)
	}
	if meta == nil || !meta.HasStage() || meta.DevPool != nil {
		return nil
	}
	if err := reconcileMateDevServices(stateDir, statusPath); err != nil {
		return fmt.Errorf("reconcile development services: %w", err)
	}
	return storeClassicLook(statusPath, host, nil, time.Time{}, &servers, at)
}

func storeClassicLook(statusPath, host string, branch *string, branchAt time.Time, servers *[]topology.DevServerState, serversAt time.Time) error {
	return mate.UpdateStatus(statusPath, func(s *mate.Status) {
		if s.Copies == nil {
			return
		}
		for i := range s.Copies.DevServices {
			dev := &s.Copies.DevServices[i]
			if dev.Hostname != host {
				continue
			}
			oldBranchAt, _ := time.Parse(time.RFC3339Nano, dev.BranchReadAt)
			oldServersAt, _ := time.Parse(time.RFC3339Nano, dev.DevServersReadAt)
			if branch != nil && branchAt.After(oldBranchAt) {
				dev.Branch, dev.BranchReadAt = branch, branchAt.UTC().Format(time.RFC3339Nano)
			}
			if servers != nil && serversAt.After(oldServersAt) {
				dev.DevServers, dev.DevServersReadAt = servers, serversAt.UTC().Format(time.RFC3339Nano)
			}
			return
		}
	})
}

func logMateCopyError(action string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] %s failed\n", action)
	}
}
