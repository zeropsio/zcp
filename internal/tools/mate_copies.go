package tools

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// KeepMateCopies keeps the Mate-wide projection beside the supervised server.
// File replacement is the event; there is no periodic liveness check.
func KeepMateCopies(ctx context.Context, env func() func(string) string, statusPath string, log func(string)) {
	rt := runtime.DetectFrom(env())
	if !rt.InContainer || !rt.MateEnabled {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stateDir := filepath.Join(topology.CodeMountRoot, ".zcp", "state")
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
	o := &mateCopyObserver{stateDir: stateDir, statusPath: statusPath, log: log,
		deps: liveDevGitDeps(env, log), ssh: platform.NewSystemSSHDeployer(),
		done: make(chan mateCopyWaited, 64), scanDone: make(chan struct{}, 1),
		seen: map[mateCopyWaitKey]time.Time{}, active: map[mateCopyWaitKey]bool{}}
	o.refresh(ctx)
	o.observe(ctx)
	o.run(ctx, watcher, changed)
}

type mateCopyWaitKey struct{ host, acquired, branch, name string }

type mateCopyWaited struct {
	key mateCopyWaitKey
	at  time.Time
}

// mateCopyObserver owns its watches, in-flight waits and coalesced scan. All
// maps and scan flags are changed only by run's goroutine.
type mateCopyObserver struct {
	stateDir, statusPath string
	log                  func(string)
	deps                 func(context.Context) (devGitDeps, bool)
	ssh                  ops.SSHDeployer
	done                 chan mateCopyWaited
	scanDone             chan struct{}
	seen                 map[mateCopyWaitKey]time.Time
	active               map[mateCopyWaitKey]bool
	scanning, scanAgain  bool
}

func (o *mateCopyObserver) run(ctx context.Context, watcher *fsnotify.Watcher, changed <-chan struct{}) {
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
			o.refresh(ctx)
		case result := <-o.done:
			delete(o.active, result.key)
			if !result.at.IsZero() {
				o.seen[result.key] = result.at
			}
			o.observe(ctx)
		case <-o.scanDone:
			o.scanning = false
			if o.scanAgain {
				o.scanAgain = false
				o.scan(ctx)
			}
		}
	}
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

func (o *mateCopyObserver) observe(ctx context.Context) {
	status, err := mate.ReadStatus(o.statusPath)
	if err != nil || status.Copies == nil {
		return
	}
	var d devGitDeps
	var haveDeps, checked bool
	present := map[mateCopyWaitKey]bool{}
	start := func(target ops.MateServerWait) {
		key := mateCopyWaitKey{target.Hostname, target.AcquiredAt, target.Branch, target.Name}
		present[key] = true
		if o.active[key] || o.seen[key].Equal(target.ReadAt) {
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
	for _, lease := range status.Copies.Leases {
		if lease.DevServers != nil {
			for _, server := range *lease.DevServers {
				if server.Status == "starting" {
					start(ops.MateServerWait{Hostname: lease.Alias, Pool: lease.Pool, Branch: lease.Branch, AcquiredAt: lease.AcquiredAt, Name: server.Name, ReadAt: server.ReadAt})
				}
			}
		}
	}
	for _, dev := range status.Copies.DevServices {
		if dev.DevServers != nil {
			for _, server := range *dev.DevServers {
				if server.Status == "starting" {
					start(ops.MateServerWait{Hostname: dev.Hostname, Name: server.Name, ReadAt: server.ReadAt})
				}
			}
		}
	}
	for key := range o.seen {
		if !present[key] {
			delete(o.seen, key)
		}
	}
}

func (o *mateCopyObserver) startWait(ctx context.Context, key mateCopyWaitKey, target ops.MateServerWait, d devGitDeps) {
	o.active[key], o.seen[key] = true, target.ReadAt
	go func() {
		ld := LeaseDepsFor(d.client, d.rt.ProjectID, o.stateDir, ops.NewSwarmFactory(&http.Client{Timeout: ops.LeaseCallTimeout}), nil, d.ssh, d.rt)
		root := supervisorRoot(o.stateDir, target.Hostname)
		at, err := ops.AwaitMateServer(ctx, ld, o.statusPath, root, target)
		if err != nil {
			o.log("copy observer could not wait for a dev server")
		}
		select {
		case o.done <- mateCopyWaited{key, at}:
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
