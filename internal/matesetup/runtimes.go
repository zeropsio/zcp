// Package matesetup is what a new Mate's container does of its own setup once
// the person's press has created it (docs/spec-mate.md): it imports the
// tier's runtimes the press handed it in MATE_SETUP_RUNTIMES, with the Mate's
// own key, and reports how that goes in the status file the mate server reads
// (mate.Status). Nothing here waits on a browser, and nothing here holds up
// the mate server's start: the supervisor runs it beside the server.
package matesetup

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// EnvRuntimes is the service env the press sets on the Mate's zcp service:
// the tier's runtimes, as base64 of an import YAML. Absent when the tier has
// no runtimes.
const EnvRuntimes = "MATE_SETUP_RUNTIMES"

// API is the part of the platform client the boot import uses: the lag-free
// project reads and the import itself.
type API interface {
	ListServicesDirect(ctx context.Context, projectID string) ([]platform.ServiceStack, error)
	GetProjectProcessesDirect(ctx context.Context, projectID string) ([]platform.Process, error)
	GetProcess(ctx context.Context, processID string) (*platform.Process, error)
	ImportServices(ctx context.Context, projectID, yamlContent string) (*platform.ImportResult, error)
	// GetProject reads the project, its tags among them (the closed-off tag,
	// ops.ClosedOffTag).
	GetProject(ctx context.Context, projectID string) (*platform.Project, error)
}

// Importer imports the plan's missing services and tracks them to the end.
type Importer struct {
	API        API
	ProjectID  string
	StatusPath string
	// Poll is the time between two looks at the project while the import's
	// processes run; Timeout bounds the whole run.
	Poll    time.Duration
	Timeout time.Duration
	// Backoff is the wait before each retry of a failed import call; its
	// length is the number of retries.
	Backoff []time.Duration
	// Now is the clock; tests fix it.
	Now func() time.Time
	// IsolationPoll is the wait before the next look at a project not yet
	// closed off, by how long it has been waited for (default IsolationPoll).
	IsolationPoll func(waited time.Duration) time.Duration
}

// IsolationPoll looks every 10 s for the first half hour — the press closes
// the project off within about 90 s of the container's import — then every
// minute, for as long as it takes: a later "Finish setup" closes it.
func IsolationPoll(waited time.Duration) time.Duration {
	if waited < 30*time.Minute {
		return 10 * time.Second
	}
	return time.Minute
}

// Defaults for a production Importer.
const (
	DefaultPoll    = 3 * time.Second
	DefaultTimeout = 20 * time.Minute
)

// DefaultBackoff is three retries of a failed import call over a minute.
var DefaultBackoff = []time.Duration{5 * time.Second, 15 * time.Second, 45 * time.Second}

// heartbeat is the longest the status goes unwritten while the import runs,
// so a reader can tell a tracker that stopped from one with nothing new.
const heartbeat = 30 * time.Second

// sinceSlack widens "the processes this import started" to a clock that is
// not the platform's.
const sinceSlack = time.Minute

// MarkLaunch is the status a mate launch starts from. A boot import that
// settled (Settled) is left exactly as it ended: it never runs again. One a
// passing failure or the timeout stopped goes back to pending, to run again;
// so does a plan not run yet. No plan, and nothing settled, is none. A
// stand-up the file says is running was stopped by this restart (the server
// and every agent under it go down with the unit), so it is marked failed:
// nothing else ever would.
func MarkLaunch(path string, planSet bool, now time.Time) error {
	settled := Settled(path)
	if err := mate.UpdateStatus(path, func(s *mate.Status) {
		switch {
		case settled:
		case !planSet:
			s.Runtimes = mate.RuntimesStatus{State: mate.RuntimesNone, UpdatedAt: stamp(now)}
		case s.Runtimes.State != mate.RuntimesImporting:
			s.Runtimes.State, s.Runtimes.Error, s.Runtimes.EndedAt = mate.RuntimesPending, "", ""
			s.Runtimes.UpdatedAt = stamp(now)
		}
		if s.Standup.State == mate.StandupRunning {
			s.Standup.State = mate.StandupFailed
			s.Standup.EndedAt = stamp(now)
			s.Standup.UpdatedAt = stamp(now)
			s.Standup.Error = "the stand-up was stopped by a restart of the Mate's server"
		}
	}); err != nil {
		return fmt.Errorf("mark the launch in the status file: %w", err)
	}
	return nil
}

// settledPath is zcp's own record, beside the status file, that the boot
// import settled — done, or refused by the platform — and must never run
// again: a later launch with the plan still in the env would otherwise bring
// back a service the person deleted. Under the home, it survives restarts.
func settledPath(statusPath string) string { return statusPath + ".settled" }

// Settled reports a boot import that settled for good.
func Settled(statusPath string) bool {
	_, err := os.Stat(settledPath(statusPath))
	return err == nil
}

// settle records that the boot import settled; a failed write is logged.
func (im Importer) settle(why string) {
	if err := os.WriteFile(settledPath(im.StatusPath), []byte(stamp(im.Now())+" "+why+"\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] mate setup: record the settled import: %v\n", err)
	}
}

// DecodePlan reads MATE_SETUP_RUNTIMES: the hostnames it lists, in order, and
// its services as import entries.
func DecodePlan(encoded string) ([]string, []map[string]any, error) {
	raw, err := decodeBase64(strings.TrimSpace(encoded))
	if err != nil {
		return nil, nil, fmt.Errorf("%s is not base64: %w", EnvRuntimes, err)
	}
	var doc struct {
		Services []map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("%s is not an import YAML: %w", EnvRuntimes, err)
	}
	var hostnames []string
	for i, svc := range doc.Services {
		host, _ := svc["hostname"].(string)
		if host == "" {
			return nil, nil, fmt.Errorf("%s: service %d has no hostname", EnvRuntimes, i+1)
		}
		hostnames = append(hostnames, host)
	}
	if len(hostnames) == 0 {
		return nil, nil, fmt.Errorf("%s lists no services", EnvRuntimes)
	}
	return hostnames, doc.Services, nil
}

func decodeBase64(s string) ([]byte, error) {
	var firstErr error
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		raw, err := enc.DecodeString(s)
		if err == nil {
			return raw, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

// Run imports what the plan lists that the project does not hold yet, then
// follows the import's processes until every listed service is there and
// settled, writing the runtimes section as it goes. It decides what is
// missing by looking, so a restart repeats nothing: a service already there
// is never imported again, and an import a restart interrupted is followed
// from where the file left it.
//
// Nothing is imported into a project that is not closed off: runtimes run
// code, and with env isolation off they read the zcp service's variables,
// the Mate's key among them; closing the project off after they exist
// restarts them. The press closes it off once the container recipe's own
// project-env write (which resets it) has landed, reads it back, and tags
// the project ops.ClosedOffTag; a press whose tab closed first leaves that
// to "Finish setup", however much later — so the wait for the tag has no
// end of its own.
func (im Importer) Run(ctx context.Context, encoded string) {
	im = im.withDefaults()
	if Settled(im.StatusPath) {
		return
	}
	hostnames, entries, err := DecodePlan(encoded)
	if err != nil {
		im.finish(nil, err.Error(), true)
		return
	}
	prev, _ := mate.ReadStatus(im.StatusPath)

	live, err := im.listWithin(ctx)
	if err != nil {
		im.finish(nil, fmt.Sprintf("could not read the project's services: %v", err), false)
		return
	}
	missing := missingHosts(hostnames, live)
	if len(missing) > 0 {
		if !im.awaitClosedOff(ctx) {
			return
		}
		if live, err = im.listWithin(ctx); err != nil {
			im.finish(nil, fmt.Sprintf("could not read the project's services: %v", err), false)
			return
		}
		missing = missingHosts(hostnames, live)
	}

	ctx, cancel := context.WithTimeout(ctx, im.Timeout)
	defer cancel()

	since := parseStamp(prev.Runtimes.StartedAt)
	if len(missing) == 0 && since.IsZero() {
		// Every listed service was there at the first look: nothing to
		// import, nothing of an earlier launch's to follow.
		im.finishWith(hostnames, live, nil, nil, im.Now())
		return
	}

	if since.IsZero() {
		since = im.Now()
	}
	im.write(func(r *mate.RuntimesStatus) {
		r.State = mate.RuntimesImporting
		r.StartedAt = stamp(since)
		r.EndedAt, r.Error = "", ""
	})

	importErrs := map[string]string{}
	expected := map[string]bool{}
	if len(missing) > 0 {
		result, err := im.importMissing(ctx, entries, hostnames, missing)
		if err != nil {
			why := "the import failed"
			if refused(err) {
				why = "the import was refused"
			}
			im.finish(hostnames, fmt.Sprintf("%s: %v", why, oneLine(err.Error())), refused(err))
			return
		}
		for _, ss := range result.ServiceStacks {
			if ss.Error != nil && !nameTaken(ss.Error.Code) {
				importErrs[ss.Name] = oneLine(apiErrorText(ss.Error))
			}
			for _, p := range ss.Processes {
				expected[p.ID] = true
			}
		}
	}
	im.follow(ctx, hostnames, since, importErrs, expected)
}

func (im Importer) withDefaults() Importer {
	if im.Poll <= 0 {
		im.Poll = DefaultPoll
	}
	if im.Timeout <= 0 {
		im.Timeout = DefaultTimeout
	}
	if im.Backoff == nil {
		im.Backoff = DefaultBackoff
	}
	if im.Now == nil {
		im.Now = time.Now
	}
	if im.IsolationPoll == nil {
		im.IsolationPoll = IsolationPoll
	}
	return im
}

// listWithin is listUntil bounded by Timeout.
func (im Importer) listWithin(ctx context.Context) ([]platform.ServiceStack, error) {
	ctx, cancel := context.WithTimeout(ctx, im.Timeout)
	defer cancel()
	return im.listUntil(ctx)
}

// awaitClosedOff waits until the project reads closed off, with the runtimes
// section pending and saying why; false when the context ended first. A
// failed read is one more look, not an answer.
func (im Importer) awaitClosedOff(ctx context.Context) bool {
	start := time.Now()
	said := false
	for {
		if closed, err := ops.ReadProjectClosedOff(ctx, im.API, im.ProjectID); err == nil && closed {
			return true
		}
		if !said {
			im.write(func(r *mate.RuntimesStatus) {
				r.State, r.Error = mate.RuntimesPending, mate.RuntimesWaitingClosedOff
			})
			fmt.Fprintf(os.Stderr, "[zcp] mate setup: %s\n", mate.RuntimesWaitingClosedOff)
			said = true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(im.IsolationPoll(time.Since(start))):
		}
	}
}

// listUntil reads the project's services, retrying a failed read each poll
// until the context ends.
func (im Importer) listUntil(ctx context.Context) ([]platform.ServiceStack, error) {
	for {
		live, err := im.API.ListServicesDirect(ctx, im.ProjectID)
		if err == nil {
			return live, nil
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(im.Poll):
		}
	}
}

// importMissing sends the plan's entries for the missing services. A failed
// call is retried by Backoff, after looking again: a call that failed on the
// way back may still have created what it carried, and that is never
// imported twice.
func (im Importer) importMissing(ctx context.Context, entries []map[string]any, hostnames, missing []string) (*platform.ImportResult, error) {
	for attempt := 0; ; attempt++ {
		var subset []map[string]any
		for _, e := range entries {
			if host, _ := e["hostname"].(string); slices.Contains(missing, host) {
				subset = append(subset, e)
			}
		}
		body, err := yaml.Marshal(map[string]any{"services": subset})
		if err != nil {
			return nil, fmt.Errorf("render the import: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[zcp] mate setup: importing %s\n", strings.Join(missing, ", "))
		result, err := im.API.ImportServices(ctx, im.ProjectID, string(body))
		if err == nil {
			return result, nil
		}
		var pe *platform.PlatformError
		if errors.As(err, &pe) && nameTaken(pe.APICode) {
			// An earlier launch's import got there first: what it
			// created is followed like this one's.
			return &platform.ImportResult{}, nil
		}
		if attempt >= len(im.Backoff) || !retryable(err) {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "[zcp] mate setup: import failed, looking again in %s: %v\n", im.Backoff[attempt], err)
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(im.Backoff[attempt]):
		}
		live, listErr := im.API.ListServicesDirect(ctx, im.ProjectID)
		if listErr != nil {
			continue
		}
		if missing = missingHosts(hostnames, live); len(missing) == 0 {
			return &platform.ImportResult{}, nil
		}
	}
}

// retryable is an import call worth sending again: anything but the
// platform's own refusal of what it carried.
func retryable(err error) bool { return !refused(err) }

// refused is the platform refusing the import for what it carried: a 4xx it
// answered with its own error code. Nothing else settles the import for good:
// a 401 or 403 is a key that may be rotated or not granted yet, and an EOF, a
// body that does not decode, a cancelled request or any error that is not
// the platform's answer leaves the outcome unknown — followed by looking.
func refused(err error) bool {
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || platform.IsTransient(err) {
		return false
	}
	return pe.Code == platform.ErrAPIError && pe.APICode != ""
}

// follow looks at the project every Poll until the listed services settle
// or the time is up, writing each change.
func (im Importer) follow(ctx context.Context, hostnames []string, since time.Time, importErrs map[string]string, expected map[string]bool) {
	var last []mate.RuntimeService
	lastWrite := im.Now()
	for {
		live, liveErr := im.API.ListServicesDirect(ctx, im.ProjectID)
		procs, procErr := im.API.GetProjectProcessesDirect(ctx, im.ProjectID)
		if liveErr == nil && procErr == nil {
			procs = im.withExpected(ctx, procs, expected)
			services, settled := ServiceStates(hostnames, live, procs, importErrs, since.Add(-sinceSlack))
			if settled {
				im.finishWith(hostnames, live, procs, importErrs, since)
				return
			}
			if !slices.Equal(services, last) || im.Now().Sub(lastWrite) >= heartbeat {
				im.write(func(r *mate.RuntimesStatus) { r.Services = services })
				last, lastWrite = services, im.Now()
			}
		}
		select {
		case <-ctx.Done():
			im.write(func(r *mate.RuntimesStatus) {
				r.State = mate.RuntimesFailed
				r.EndedAt = stamp(im.Now())
				r.Error = fmt.Sprintf("the runtimes' import was still in flight after %s", im.Timeout)
			})
			return
		case <-time.After(im.Poll):
		}
	}
}

// withExpected adds the import's own processes the project list does not
// show, read by id, so a service is never settled before its process ran.
func (im Importer) withExpected(ctx context.Context, procs []platform.Process, expected map[string]bool) []platform.Process {
	seen := map[string]bool{}
	for _, p := range procs {
		seen[p.ID] = true
	}
	for id := range expected {
		if seen[id] {
			continue
		}
		if p, err := im.API.GetProcess(ctx, id); err == nil && p != nil {
			procs = append(procs, *p)
		}
	}
	return procs
}

// finishWith writes the end state from one last look.
func (im Importer) finishWith(hostnames []string, live []platform.ServiceStack, procs []platform.Process, importErrs map[string]string, since time.Time) {
	services, _ := ServiceStates(hostnames, live, procs, importErrs, since.Add(-sinceSlack))
	var failures []string
	for _, s := range services {
		if s.State == mate.ServiceFailed {
			failures = append(failures, s.Hostname+": "+s.Error)
		}
	}
	now := im.Now()
	im.write(func(r *mate.RuntimesStatus) {
		r.State = mate.RuntimesDone
		r.Error = ""
		if len(failures) > 0 {
			r.State = mate.RuntimesFailed
			r.Error = oneLine(strings.Join(failures, "; "))
		}
		if r.StartedAt == "" {
			r.StartedAt = stamp(now)
		}
		r.EndedAt = stamp(now)
		r.Services = services
	})
	im.settle(strings.Join(stateLine(services), ", "))
	fmt.Fprintf(os.Stderr, "[zcp] mate setup: runtimes %s\n", strings.Join(stateLine(services), ", "))
}

// finish ends the run failed with reason, before anything could be followed.
func (im Importer) finish(hostnames []string, reason string, settles bool) {
	if settles {
		im.settle("failed: " + oneLine(reason))
	}
	now := im.Now()
	im.write(func(r *mate.RuntimesStatus) {
		r.State = mate.RuntimesFailed
		r.Error = oneLine(reason)
		if r.StartedAt == "" {
			r.StartedAt = stamp(now)
		}
		r.EndedAt = stamp(now)
		if len(r.Services) == 0 {
			for _, h := range hostnames {
				r.Services = append(r.Services, mate.RuntimeService{Hostname: h, State: mate.ServiceFailed})
			}
		}
	})
	fmt.Fprintf(os.Stderr, "[zcp] mate setup: runtimes failed: %s\n", oneLine(reason))
}

// write changes the runtimes section; a failed write is logged, never fatal.
func (im Importer) write(change func(*mate.RuntimesStatus)) {
	if err := mate.UpdateRuntimes(im.StatusPath, change); err != nil {
		fmt.Fprintf(os.Stderr, "[zcp] mate setup: %v\n", err)
	}
}

// ServiceStates is each listed service as the project shows it: failed when
// the import refused it or a process for it since `since` failed, creating /
// building / deploying while one runs, running once none does. settled is
// every service there (or failed) with nothing of this import still running.
func ServiceStates(hostnames []string, live []platform.ServiceStack, procs []platform.Process, importErrs map[string]string, since time.Time) ([]mate.RuntimeService, bool) {
	byName := map[string]platform.ServiceStack{}
	for _, s := range live {
		byName[s.Name] = s
	}
	settled := true
	out := make([]mate.RuntimeService, 0, len(hostnames))
	for _, host := range hostnames {
		entry := mate.RuntimeService{Hostname: host}
		svc, there := byName[host]
		switch {
		case importErrs[host] != "":
			entry.State, entry.Error = mate.ServiceFailed, importErrs[host]
		case !there:
			entry.State = mate.ServiceCreating
			settled = false
		default:
			entry = serviceFromProcesses(entry, svc.ID, procs, since)
			if entry.State != mate.ServiceRunning && entry.State != mate.ServiceFailed {
				settled = false
			}
		}
		out = append(out, entry)
	}
	return out, settled
}

// serviceFromProcesses reads one present service's state from the processes
// that touched it since `since`: a live one wins; else the newest decides.
func serviceFromProcesses(entry mate.RuntimeService, serviceID string, procs []platform.Process, since time.Time) mate.RuntimeService {
	var newest *platform.Process
	for i := range procs {
		p := &procs[i]
		if !touches(p, serviceID) {
			continue
		}
		if created := parseStamp(p.Created); !created.IsZero() && created.Before(since) {
			continue
		}
		if ops.IsProcessLive(p.Status) {
			entry.State, entry.ProcessID = liveState(p), p.ID
			return entry
		}
		if newest == nil || p.Created > newest.Created {
			newest = p
		}
	}
	entry.State = mate.ServiceRunning
	if newest != nil && (newest.Status == platform.ProcessStatusFailed || newest.Status == platform.ProcessStatusCanceled) {
		entry.State, entry.ProcessID = mate.ServiceFailed, newest.ID
		entry.Error = fmt.Sprintf("%s ended %s", newest.ActionName, newest.Status)
		if newest.FailReason != nil && *newest.FailReason != "" {
			entry.Error += ": " + oneLine(*newest.FailReason)
		}
	}
	return entry
}

func touches(p *platform.Process, serviceID string) bool {
	for _, ref := range p.ServiceStacks {
		if ref.ID == serviceID {
			return true
		}
	}
	return false
}

// liveState names a running process's phase: a build process by its
// appVersion's phase, a deploy as deploying, anything else (stack.create, the
// import's own) as the service being created.
func liveState(p *platform.Process) string {
	switch p.ActionName {
	case "stack.deploy":
		return mate.ServiceDeploying
	case "stack.build":
	default:
		return mate.ServiceCreating
	}
	if p.AppVersion != nil && p.AppVersion.Status == platform.BuildStatusDeploying {
		return mate.ServiceDeploying
	}
	return mate.ServiceBuilding
}

func missingHosts(hostnames []string, live []platform.ServiceStack) []string {
	present := map[string]bool{}
	for _, s := range live {
		present[s.Name] = true
	}
	var missing []string
	for _, h := range hostnames {
		if !present[h] {
			missing = append(missing, h)
		}
	}
	return missing
}

// nameTaken is the platform answering that a service of that name is
// already there — an earlier launch's import of the same plan, not a failure.
func nameTaken(code string) bool {
	c := strings.ToLower(code)
	return strings.Contains(c, "nameunavailable") || strings.Contains(c, "alreadyexist") || strings.Contains(c, "nametaken")
}

func apiErrorText(e *platform.APIError) string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

func stateLine(services []mate.RuntimeService) []string {
	out := make([]string, 0, len(services))
	for _, s := range services {
		out = append(out, s.Hostname+" "+s.State)
	}
	return out
}

// errorLineMax bounds an error line in the status file.
const errorLineMax = 300

// oneLine folds s onto one line and bounds it.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > errorLineMax {
		s = s[:errorLineMax-1] + "…"
	}
	return s
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseStamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return time.Time{}
}

// Boot is the boot import as the mate launch runs it, beside the server: the
// plan, the Mate's key and its project from the container's environment
// (lookup reads the live env store first), then Run. No plan, nothing to do.
// A plan without the key or the project to act on is a failed import that
// says which is missing.
func Boot(ctx context.Context, statusPath string, lookup func(string) string) {
	encoded := lookup(EnvRuntimes)
	if strings.TrimSpace(encoded) == "" {
		return
	}
	im := Importer{StatusPath: statusPath, ProjectID: lookup("projectId")}.withDefaults()
	token := lookup("ZCP_API_KEY")
	switch {
	case token == "":
		im.finish(nil, "ZCP_API_KEY is not on this service, so the runtimes cannot be imported with the Mate's key", false)
		return
	case im.ProjectID == "":
		im.finish(nil, "projectId is not in this container's environment", false)
		return
	}
	client, err := platform.NewZeropsClient(token, mate.ResolveAPIHost(lookup("ZCP_API_HOST")))
	if err != nil {
		im.finish(nil, fmt.Sprintf("could not build the API client: %v", err), false)
		return
	}
	im.API = client
	im.Run(ctx, encoded)
}
