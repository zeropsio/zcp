package ops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// BoxAPI is a Zerops Box workspace's API, reached through zproxy
// (platform.BoxClient).
type BoxAPI interface {
	Ping(ctx context.Context) (platform.BoxPing, error)
	Workspace(ctx context.Context) (platform.BoxWorkspace, error)
	Bases(ctx context.Context) ([]platform.BoxBase, error)
	Operations(ctx context.Context, kind string) ([]platform.BoxOperation, error)
	PullBase(ctx context.Context, req platform.BoxPullBaseRequest, idempotencyKey string) (platform.BoxOperation, error)
	Operation(ctx context.Context, id string, wait time.Duration) (platform.BoxOperation, error)
	PutDomains(ctx context.Context, domains []platform.BoxDomain) (platform.BoxWorkspace, error)
}

var _ BoxAPI = (*platform.BoxClient)(nil)

// BoxFactory builds the API client of one Box from the minter of its zproxy
// accesses.
type BoxFactory func(mint platform.BoxMinter) BoxAPI

// NewBoxFactory builds Box API clients over hc.
func NewBoxFactory(hc *http.Client) BoxFactory {
	return func(mint platform.BoxMinter) BoxAPI { return platform.NewBoxClient(mint, hc) }
}

// BoxCallTimeout bounds one Box API call: above the 30 s an operation
// long-poll holds its answer.
const BoxCallTimeout = 60 * time.Second

// The base every Box setup pulls: the default sandbox image, as `zbox setup`
// pulls it when it names no other (the owner's decision: the default only).
const (
	BoxDefaultBaseImageRef = "https://storage.cdn.zerops.app/4ga3k-boxstorage/zbox/zbox-base.tar.gz"
	BoxDefaultBaseName     = "default"
)

// BoxSetupResult.Status: whether sandboxes can be created — READY (a READY
// base), PULLING (a base pull still running), FAILED (neither; Problems says
// why).
const (
	BoxSetupReady   = "READY"
	BoxSetupPulling = "PULLING"
	BoxSetupFailed  = "FAILED"
)

// BoxDomainView.State.
const (
	// BoxDomainAdded: this call put the 8081 subdomain in the domain list.
	BoxDomainAdded = "added"
	// BoxDomainPresent: the list already held it; nothing was written.
	BoxDomainPresent = "present"
	// BoxDomainNone: no sandbox domain is registered; Reason says why.
	BoxDomainNone = "none"
)

// BoxBaseView.Status beyond the platform's base statuses.
const (
	// boxBaseNone: no base, and no pull was started.
	boxBaseNone = "NONE"
	// boxBaseUnknown: the bases could not be read.
	boxBaseUnknown = "UNKNOWN"
)

// Default setup budgets: the ui-access mint answers
// serviceStackNoActiveContainer for a moment after a create (the zbox CLI
// retries it for 2 min), and a base pull takes 23 s on a fresh workspace and
// up to 15 min by the docs.
const (
	boxReadyWait  = 120 * time.Second
	boxReadyEvery = 5 * time.Second
	boxPullWait   = 180 * time.Second
	boxPollWait   = 30 * time.Second
)

// BoxTiming bounds a setup. A zero field takes its default.
type BoxTiming struct {
	// ReadyWait bounds the wait for the orchestrator to answer; ReadyEvery
	// is how often it is asked meanwhile.
	ReadyWait, ReadyEvery time.Duration
	// PullWait bounds the wait for a base pull; PollWait is one long-poll of
	// its operation.
	PullWait, PollWait time.Duration
}

func (t BoxTiming) withDefaults() BoxTiming {
	if t.ReadyWait <= 0 {
		t.ReadyWait = boxReadyWait
	}
	if t.ReadyEvery <= 0 {
		t.ReadyEvery = boxReadyEvery
	}
	if t.PullWait <= 0 {
		t.PullWait = boxPullWait
	}
	if t.PollWait <= 0 {
		t.PollWait = boxPollWait
	}
	return t
}

// BoxDeps are what the Box operations need.
type BoxDeps struct {
	Client    platform.Client
	ProjectID string
	Box       BoxFactory
	Timing    BoxTiming
	// NewKey makes a pull's Idempotency-Key; nil makes a random one.
	NewKey func() string
}

func (d BoxDeps) newKey() string {
	if d.NewKey != nil {
		return d.NewKey()
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "zcp-box-setup-" + hex.EncodeToString(b)
}

// api builds the Box's client, minting its accesses with zcp's own Zerops
// token (path A: zcp never reads ZBOX_WORKSPACE_API_KEY).
func (d BoxDeps) api(serviceID string) BoxAPI {
	return d.Box(func(ctx context.Context) (platform.BoxAccess, error) {
		return d.Client.MintBoxAccess(ctx, serviceID)
	})
}

// BoxSetupResult is the boxSetup block: what a Box's setup found and did,
// every failure with its fix, and what to do next. It never carries an access
// token or the workspace key.
type BoxSetupResult struct {
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
	// Orchestrator says whether the workspace API answered, and after how
	// long, or why not.
	Orchestrator string         `json:"orchestrator"`
	Base         *BoxBaseView   `json:"base,omitempty"`
	Domain       *BoxDomainView `json:"domain,omitempty"`
	Problems     []BoxProblem   `json:"problems,omitempty"`
	Next         string         `json:"next"`
}

func (r *BoxSetupResult) problem(what, fix string) {
	r.Problems = append(r.Problems, BoxProblem{Problem: what, Fix: fix})
}

// BoxBaseView is a base as the agent sees it.
type BoxBaseView struct {
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
	Digest string `json:"digest,omitempty"`
	// Operation is the pull still running (or the one that failed).
	Operation string `json:"operation,omitempty"`
	// Pulled marks a pull this call started.
	Pulled bool `json:"pulled,omitempty"`
}

// BoxDomainView is the Box's 8081 Zerops subdomain as a sandbox domain.
type BoxDomainView struct {
	Domain string `json:"domain,omitempty"`
	Form   string `json:"form,omitempty"`
	State  string `json:"state"`
	// Reason says why no domain is registered; Recovery is the calls that
	// register it, in order.
	Reason   string   `json:"reason,omitempty"`
	Recovery []string `json:"recovery,omitempty"`
}

// BoxProblem is one failure, named with its fix.
type BoxProblem struct {
	Problem string `json:"problem"`
	Fix     string `json:"fix"`
}

// BoxStatusResult is what a read of a Box's setup found. Reading changes
// nothing.
type BoxStatusResult struct {
	Hostname      string               `json:"hostname"`
	ServiceStatus string               `json:"serviceStatus"`
	Orchestrator  string               `json:"orchestrator"`
	Bases         []BoxBaseView        `json:"bases"`
	Pull          *BoxBaseView         `json:"pull,omitempty"`
	Domains       []platform.BoxDomain `json:"domains"`
	Subdomain     BoxSubdomainView     `json:"subdomain"`
	Problems      []BoxProblem         `json:"problems,omitempty"`
	Next          string               `json:"next"`
}

func (r *BoxStatusResult) problem(what, fix string) {
	r.Problems = append(r.Problems, BoxProblem{Problem: what, Fix: fix})
}

// BoxSubdomainView is the Box's subdomain state: whether access is on (read
// from the service, never from the presence of zeropsSubdomain, which the
// platform writes while the service is still being created), the 8081 host,
// and whether it is a sandbox domain.
type BoxSubdomainView struct {
	Access      bool   `json:"access"`
	IngressHost string `json:"ingressHost,omitempty"`
	Registered  bool   `json:"registered"`
}

// boxSetupCall is the recovery call that re-runs a Box's setup.
func boxSetupCall(host string) string { return "zerops_box action=setup serviceHostname=" + host }

func boxEnableCall(host string) string {
	return "zerops_subdomain action=enable serviceHostname=" + host
}

// BoxSetup finishes the setup of the Box hostname so a first sandbox can be
// created (docs/spec-workflows.md §8 PS-8): it waits for the workspace API to
// answer, makes sure a READY base exists — pulling the default image only
// when none is READY and no pull is running — and, when the Box's subdomain
// access is on, makes its 8081 Zerops subdomain a sandbox domain, keeping
// every domain already there. It never turns subdomain access on. Every step
// reads before it writes, so a re-run on a Box already set up changes
// nothing. A setup that fails is named in the result with its fix; the error
// is only for a hostname that is no Box.
func BoxSetup(ctx context.Context, d BoxDeps, hostname string) (BoxSetupResult, error) {
	svc, err := resolveBox(ctx, d, hostname)
	if err != nil {
		return BoxSetupResult{}, err
	}
	return boxSetupFor(ctx, d, svc), nil
}

// BoxSetupImported is BoxSetup for a service an import just created, read by
// its id (a direct read: the service search lags an import). nil when the
// service is no Box.
func BoxSetupImported(ctx context.Context, d BoxDeps, hostname, serviceID string) *BoxSetupResult {
	svc, err := d.Client.GetService(ctx, serviceID)
	if err != nil {
		r := BoxSetupResult{Hostname: hostname, Status: BoxSetupFailed, Orchestrator: "not asked"}
		r.problem("could not read the Box after its import: "+boxErrText(err), "Once zerops_discover lists "+hostname+", "+boxSetupCall(hostname))
		r.Next = boxFailedNext(hostname)
		return &r
	}
	if !topology.IsSandboxType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return nil
	}
	r := boxSetupFor(ctx, d, svc)
	return &r
}

// resolveBox finds the service hostname through the direct service list (the
// search lags a just-imported service and a just-changed subdomain access)
// and refuses any service that is no Box, naming what it is.
func resolveBox(ctx context.Context, d BoxDeps, hostname string) (*platform.ServiceStack, error) {
	services, err := d.Client.ListServicesDirect(ctx, d.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	svc, err := FindService(services, hostname)
	if err != nil {
		return nil, fmt.Errorf("find box: %w", err)
	}
	if typ := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName; !topology.IsSandboxType(typ) {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Service '%s' is %s, not a Zerops Box", svc.Name, typ),
			"zerops_box sets up a Zerops Box (ubuntu/zbox@1); pass the Box's hostname")
	}
	return svc, nil
}

func boxSetupFor(ctx context.Context, d BoxDeps, svc *platform.ServiceStack) BoxSetupResult {
	t := d.Timing.withDefaults()
	host := svc.Name
	r := BoxSetupResult{Hostname: host}
	if svc.Status == platform.ServiceStatusStopped {
		r.Status = BoxSetupFailed
		r.Orchestrator = "not asked: the Box is stopped"
		r.problem(boxStoppedProblem, boxStoppedFix(host))
		r.Next = boxFailedNext(host)
		return r
	}
	api := d.api(svc.ID)
	waited, err := waitBoxReady(ctx, api, t)
	if err != nil {
		r.Status = BoxSetupFailed
		r.Orchestrator = "not answering: " + boxErrText(err)
		what, fix := boxReachProblem(host, err)
		r.problem(what, fix)
		r.Next = boxFailedNext(host)
		return r
	}
	r.Orchestrator = "answering"
	if waited >= time.Second {
		r.Orchestrator = fmt.Sprintf("answering (after %s)", waited.Round(time.Second))
	}
	r.Base = ensureBoxBase(ctx, d, api, t, host, &r)
	r.Domain = ensureBoxDomain(ctx, d, api, svc, &r)
	switch r.Base.Status {
	case platform.BoxBaseStatusReady:
		r.Status = BoxSetupReady
		r.Next = boxReadyNext(host, r.Base.Digest)
	case platform.BoxBaseStatusPulling, platform.BoxBaseStatusFlattening:
		r.Status = BoxSetupPulling
		r.Next = boxPullingNext(host, r.Base.Operation)
	default:
		r.Status = BoxSetupFailed
		r.Next = boxFailedNext(host)
	}
	return r
}

// boxNotReadyError is an orchestrator that kept refusing within the budget.
type boxNotReadyError struct {
	wait time.Duration
	last error
}

func (e *boxNotReadyError) Error() string {
	return fmt.Sprintf("did not answer within %s (last: %s)", e.wait, boxErrText(e.last))
}

func (e *boxNotReadyError) Unwrap() error { return e.last }

// waitBoxReady pings the workspace API until it answers, a refusal says it
// never will, or the budget ends.
func waitBoxReady(ctx context.Context, api BoxAPI, t BoxTiming) (time.Duration, error) {
	start := time.Now()
	deadline := start.Add(t.ReadyWait)
	for {
		_, err := api.Ping(ctx)
		if err == nil {
			return time.Since(start), nil
		}
		if !boxReachRetryable(err) {
			return 0, err
		}
		if time.Now().Add(t.ReadyEvery).After(deadline) {
			return 0, &boxNotReadyError{wait: t.ReadyWait, last: err}
		}
		if err := boxSleep(ctx, t.ReadyEvery); err != nil {
			return 0, &boxNotReadyError{wait: time.Since(start).Round(time.Second), last: err}
		}
	}
}

// boxSleep waits d or until ctx ends.
func boxSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// Platform codes a Box's ui-access mint answers.
const (
	boxAPICodeNoActiveContainer = "serviceStackNoActiveContainer"
	boxAPICodeNotFound          = "serviceStackNotFound"
	boxAPICodeNoPermission      = "insufficientPermissions"
)

// boxReachRetryable reports a refusal the orchestrator outgrows: a container
// not active yet, the network, a timeout, a 5xx (zproxy answers 502 while the
// orchestrator starts).
func boxReachRetryable(err error) bool {
	var be *platform.BoxError
	if errors.As(err, &be) {
		return be.Status >= http.StatusInternalServerError
	}
	var pe *platform.PlatformError
	if errors.As(err, &pe) {
		return pe.APICode == boxAPICodeNoActiveContainer || platform.IsTransient(err)
	}
	return false
}

// boxTransient reports a failure worth sending the same request again for.
func boxTransient(err error) bool {
	var be *platform.BoxError
	if errors.As(err, &be) {
		return be.Status >= http.StatusInternalServerError
	}
	return platform.IsTransient(err)
}

const boxStoppedProblem = "the Box is stopped: its workspace API answers only while it runs"

func boxStoppedFix(host string) string {
	return fmt.Sprintf("zerops_manage action=start serviceHostname=%s, then %s", host, boxSetupCall(host))
}

// boxReachProblem names why the workspace API could not be reached, with
// its fix.
func boxReachProblem(host string, err error) (string, string) {
	setup := boxSetupCall(host)
	var notReady *boxNotReadyError
	var be *platform.BoxError
	var pe *platform.PlatformError
	switch {
	case errors.As(err, &notReady):
		return "the Box's orchestrator " + notReady.Error(),
			fmt.Sprintf("zerops_logs serviceHostname=%s shows the orchestrator's log (tag zerops-zbox-orchestrator); once it runs, %s", host, setup)
	case errors.As(err, &be) && be.Status == http.StatusUnauthorized:
		return "zproxy refused zcp's access to the Box (401) even with a freshly minted one",
			fmt.Sprintf("Call %s again; if it persists, ZBOX_WORKSPACE_API_KEY may have changed without a reload: reload the Box (zerops_manage action=reload serviceHostname=%s keeps running sandboxes), then %s", setup, host, setup)
	case errors.As(err, &pe) && (pe.Code == platform.ErrPermissionDenied || pe.APICode == boxAPICodeNoPermission):
		return "zcp's Zerops token may not open the Box's API: ui-access needs BasicUser or above on the project (" + boxErrText(err) + ")",
			"Ask the person to give this zcp's token BasicUser or above on the project, then " + setup
	case errors.As(err, &pe) && (pe.Code == platform.ErrServiceNotFound || pe.APICode == boxAPICodeNotFound):
		return "the Box is gone: the platform no longer knows the service (" + boxErrText(err) + ")",
			"zerops_discover lists the project's services"
	}
	return "could not reach the Box's workspace API: " + boxErrText(err), "Call " + setup + " again"
}

// boxErrText is an error's text with the platform's code when the text does
// not name it.
func boxErrText(err error) string {
	text := err.Error()
	var pe *platform.PlatformError
	if errors.As(err, &pe) && pe.APICode != "" && !strings.Contains(text, pe.APICode) {
		text += " (" + pe.APICode + ")"
	}
	return text
}

// ensureBoxBase makes sure the Box has a READY base: one that is READY
// already; else the pull that is running, polled; else a new pull of the
// default image, polled. A base on its way with no operation to follow is
// left alone. Read before write: a re-run never pulls twice. The running
// pull is found in operation-list, not base-list: base-list lists a pulled
// base only once it is READY (live, 2026-10-24: 30 reads during a 28 s pull
// listed no base, while operation-list showed the PULL_BASE RUNNING).
func ensureBoxBase(ctx context.Context, d BoxDeps, api BoxAPI, t BoxTiming, host string, r *BoxSetupResult) *BoxBaseView {
	setup := boxSetupCall(host)
	bases, err := api.Bases(ctx)
	if err != nil {
		r.problem("could not read the Box's bases: "+boxErrText(err), "Call "+setup+" again")
		return &BoxBaseView{Status: boxBaseUnknown}
	}
	if b := readyBoxBase(bases); b != nil {
		return &BoxBaseView{Name: b.Name, Status: platform.BoxBaseStatusReady, Digest: b.Digest}
	}
	pulls, err := api.Operations(ctx, platform.BoxOperationKindPullBase)
	if err != nil {
		r.problem("could not check for a base pull in progress, so none was started: "+boxErrText(err), "Call "+setup+" again")
		return &BoxBaseView{Status: boxBaseUnknown}
	}
	if op := runningBoxPull(pulls); op != nil {
		return pollBoxPull(ctx, api, t, host, op.ID, false, r)
	}
	if b := pullingBoxBase(bases); b != nil {
		return &BoxBaseView{Name: b.Name, Status: b.Status}
	}
	key := d.newKey()
	req := platform.BoxPullBaseRequest{ImageRef: BoxDefaultBaseImageRef, Name: BoxDefaultBaseName}
	op, err := api.PullBase(ctx, req, key)
	if err != nil && boxTransient(err) {
		// The same key: a pull whose answer was lost runs once.
		op, err = api.PullBase(ctx, req, key)
	}
	if err != nil {
		what, fix := boxPullProblem(host, err)
		r.problem(what, fix)
		return &BoxBaseView{Name: BoxDefaultBaseName, Status: boxBaseNone}
	}
	return pollBoxPull(ctx, api, t, host, op.ID, true, r)
}

// readyBoxBase is a READY base, the default one first.
func readyBoxBase(bases []platform.BoxBase) *platform.BoxBase {
	var first *platform.BoxBase
	for i := range bases {
		if bases[i].Status != platform.BoxBaseStatusReady {
			continue
		}
		if bases[i].Name == BoxDefaultBaseName {
			return &bases[i]
		}
		if first == nil {
			first = &bases[i]
		}
	}
	return first
}

// pullingBoxBase is a base on its way to READY.
func pullingBoxBase(bases []platform.BoxBase) *platform.BoxBase {
	for i := range bases {
		if bases[i].Status == platform.BoxBaseStatusPulling || bases[i].Status == platform.BoxBaseStatusFlattening {
			return &bases[i]
		}
	}
	return nil
}

// runningBoxPull is the newest pull still pending or running.
func runningBoxPull(pulls []platform.BoxOperation) *platform.BoxOperation {
	for i := range pulls {
		if pulls[i].Status == platform.BoxOperationPending || pulls[i].Status == platform.BoxOperationRunning {
			return &pulls[i]
		}
	}
	return nil
}

// pollBoxPull long-polls the pull id until it finishes, fails, or the budget
// ends. A replayed Idempotency-Key hands back the first answer with a stale
// status, so only the operation read tells where a pull is.
func pollBoxPull(ctx context.Context, api BoxAPI, t BoxTiming, host, id string, pulled bool, r *BoxSetupResult) *BoxBaseView {
	pulling := &BoxBaseView{Name: BoxDefaultBaseName, Status: platform.BoxBaseStatusPulling, Operation: id, Pulled: pulled}
	deadline := time.Now().Add(t.PullWait)
	for {
		asked := time.Now()
		op, err := api.Operation(ctx, id, t.PollWait)
		switch {
		case err != nil && ctx.Err() != nil:
			return pulling
		case err != nil && !boxTransient(err):
			r.problem(fmt.Sprintf("could not follow the base pull (operation %s): %s", id, boxErrText(err)), "Call "+boxSetupCall(host)+" again")
			return &BoxBaseView{Name: BoxDefaultBaseName, Status: boxBaseUnknown, Operation: id, Pulled: pulled}
		case err == nil && op.Status == platform.BoxOperationFinished:
			name := op.BaseName()
			if name == "" {
				name = BoxDefaultBaseName
			}
			return &BoxBaseView{Name: name, Status: platform.BoxBaseStatusReady, Digest: op.BaseDigest(), Pulled: pulled}
		case err == nil && op.Status == platform.BoxOperationFailed:
			r.problem(fmt.Sprintf("the base pull failed (operation %s): %s", id, op.Error), boxSetupCall(host)+" pulls it again")
			return &BoxBaseView{Name: BoxDefaultBaseName, Status: platform.BoxBaseStatusFailed, Operation: id, Pulled: pulled}
		}
		if !time.Now().Before(deadline) {
			return pulling
		}
		if time.Since(asked) < t.PollWait/2 {
			// The answer came early (a transport failure, a server that
			// does not hold the poll): pause rather than spin.
			if boxSleep(ctx, min(t.ReadyEvery, time.Until(deadline))) != nil {
				return pulling
			}
		}
	}
}

// boxPullProblem names why a pull could not start, with its fix.
func boxPullProblem(host string, err error) (string, string) {
	setup := boxSetupCall(host)
	var be *platform.BoxError
	if errors.As(err, &be) && be.Code == platform.BoxErrorWorkspaceFull {
		return "the workspace has no room to pull the default base (409 workspaceFull" + boxFullDetail(be.Metadata) + ")",
			fmt.Sprintf("Free room in the workspace — remove or park sandboxes, or raise the Box's RAM or disk with zerops_scale serviceHostname=%s (a resize restarts the Box and stops its running sandboxes) — then %s", host, setup)
	}
	if errors.As(err, &be) && be.Status == http.StatusUnauthorized {
		return boxReachProblem(host, err)
	}
	return "the default base pull was refused: " + boxErrText(err), "Call " + setup + " again"
}

// boxFullDetail spells out a workspaceFull refusal's metadata.
func boxFullDetail(meta map[string]string) string {
	resource := meta["resource"]
	if resource == "" {
		return ""
	}
	size := func(v string) string {
		n, err := strconv.ParseInt(v, 10, 64)
		switch {
		case err != nil:
			return v
		case resource == "cpu":
			return strconv.FormatInt(n, 10) + " vCPU"
		}
		return boxBytes(n)
	}
	text := ": " + resource
	if v, ok := meta["requested"]; ok {
		text += " needs " + size(v)
	}
	if v, ok := meta["free"]; ok {
		text += ", " + size(v) + " free"
	}
	return text
}

// boxBytes renders n bytes in binary units.
func boxBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', -1, 64) + " " + string("KMGTPE"[exp]) + "iB"
}

// ensureBoxDomain makes the Box's 8081 Zerops subdomain a sandbox domain
// when its subdomain access is on: the domain list is read, and written —
// whole, every domain kept — only when the ingress host is missing. With
// access off nothing is registered and the result says how to turn it on.
func ensureBoxDomain(ctx context.Context, d BoxDeps, api BoxAPI, svc *platform.ServiceStack, r *BoxSetupResult) *BoxDomainView {
	host := svc.Name
	setup := boxSetupCall(host)
	if !svc.SubdomainAccess {
		r.problem("the Box's subdomain access is off, so no sandbox domain is registered and sandbox ports get no public URL",
			boxEnableCall(host)+", then "+setup)
		return &BoxDomainView{State: BoxDomainNone,
			Reason:   "subdomain access is off: sandbox ports get no public URL, and setup never turns it on",
			Recovery: []string{boxEnableCall(host), setup}}
	}
	ingress, err := boxIngressFor(ctx, d, svc.ID)
	if err != nil {
		r.problem("could not read the Box's zeropsSubdomain: "+boxErrText(err), "Call "+setup+" again")
		return &BoxDomainView{State: BoxDomainNone, Reason: "the Box's zeropsSubdomain could not be read", Recovery: []string{setup}}
	}
	if ingress == "" {
		r.problem("subdomain access is on but the service env zeropsSubdomain names no 8081 host yet", "Call "+setup+" again in a moment")
		return &BoxDomainView{State: BoxDomainNone, Reason: "zeropsSubdomain names no 8081 host yet", Recovery: []string{setup}}
	}
	ws, err := api.Workspace(ctx)
	if err != nil {
		r.problem("could not read the workspace's domains: "+boxErrText(err), "Call "+setup+" again")
		return &BoxDomainView{Domain: ingress, State: BoxDomainNone, Reason: "the workspace's domains could not be read", Recovery: []string{setup}}
	}
	for _, dom := range ws.Domains {
		if strings.EqualFold(dom.Domain, ingress) {
			return &BoxDomainView{Domain: dom.Domain, Form: dom.Form, State: BoxDomainPresent}
		}
	}
	merged := append(slices.Clone(ws.Domains), platform.BoxDomain{Domain: ingress, Form: platform.BoxDomainFormDash})
	if _, err := api.PutDomains(ctx, merged); err != nil {
		r.problem(fmt.Sprintf("could not add the sandbox domain %s: %s", ingress, boxErrText(err)), "Call "+setup+" again")
		return &BoxDomainView{Domain: ingress, State: BoxDomainNone, Reason: "the domain list could not be written", Recovery: []string{setup}}
	}
	return &BoxDomainView{Domain: ingress, Form: platform.BoxDomainFormDash, State: BoxDomainAdded}
}

// boxIngressFor reads the Box's 8081 host from its zeropsSubdomain.
func boxIngressFor(ctx context.Context, d BoxDeps, serviceID string) (string, error) {
	envs, err := FetchServiceEnv(ctx, d.Client, serviceID)
	if err != nil {
		return "", fmt.Errorf("read box env: %w", err)
	}
	for _, e := range envs {
		if e.Key == "zeropsSubdomain" {
			return boxIngressHost(e.Content), nil
		}
	}
	return "", nil
}

// boxIngressHost is the sandbox ingress host in a Box's zeropsSubdomain: the
// platform writes one URL per published port, the API's (8080) first and the
// ingress's (8081) second. The host naming port 8081 is taken wherever its
// line is; with none, line 2.
func boxIngressHost(value string) string {
	var hosts []string
	for line := range strings.SplitSeq(strings.TrimSpace(value), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if u, err := url.Parse(line); err == nil && u.Host != "" {
			line = u.Host
		}
		line = strings.TrimSuffix(line, "/")
		if strings.Contains(line, "-8081.") {
			return line
		}
		hosts = append(hosts, line)
	}
	if len(hosts) >= 2 {
		return hosts[1]
	}
	return ""
}

func boxReadyNext(host, digest string) string {
	return fmt.Sprintf("Create sandboxes through the workspace API: an app in the project reaches it at http://%s:8080/api/rest/ with ${%s_ZBOX_WORKSPACE_API_KEY} wired by reference as its bearer (zcp never reads that key); "+
		"POST /api/rest/sandbox takes baseDigest %s and a networkMode (PROJECT_INTERNET reaches every project service, INTERNET and AIRGAPPED do not). "+
		"zerops_box action=status serviceHostname=%s re-reads this setup.", host, host, digest, host)
}

func boxPullingNext(host, operation string) string {
	if operation == "" {
		return fmt.Sprintf("A base is still on its way and sandboxes need a READY one: %s checks again.", boxSetupCall(host))
	}
	return fmt.Sprintf("The default base is still pulling (operation %s) and sandboxes need a READY base: %s waits for it again (a pull may take up to 15 minutes).", operation, boxSetupCall(host))
}

func boxFailedNext(host string) string {
	return fmt.Sprintf("The Box is not usable yet: fix the problems above, then %s.", boxSetupCall(host))
}

// BoxStatus reads the Box hostname's setup: the orchestrator, its bases and
// any pull running, its sandbox domains and subdomain state, and the calls
// that finish what is missing. It never mutates.
func BoxStatus(ctx context.Context, d BoxDeps, hostname string) (BoxStatusResult, error) {
	svc, err := resolveBox(ctx, d, hostname)
	if err != nil {
		return BoxStatusResult{}, err
	}
	host := svc.Name
	r := BoxStatusResult{Hostname: host, ServiceStatus: svc.Status, Bases: []BoxBaseView{}, Domains: []platform.BoxDomain{}}
	r.Subdomain.Access = svc.SubdomainAccess
	if ingress, err := boxIngressFor(ctx, d, svc.ID); err == nil {
		r.Subdomain.IngressHost = ingress
	} else {
		r.problem("could not read the Box's zeropsSubdomain: "+boxErrText(err), "Call zerops_box action=status serviceHostname="+host+" again")
	}
	if svc.Status == platform.ServiceStatusStopped {
		r.Orchestrator = "not asked: the Box is stopped"
		r.problem(boxStoppedProblem, boxStoppedFix(host))
		r.Next = "Start the Box: " + boxStoppedFix(host) + "."
		return r, nil
	}
	api := d.api(svc.ID)
	if _, err := api.Ping(ctx); err != nil {
		r.Orchestrator = "not answering: " + boxErrText(err)
		what, fix := boxReachProblem(host, err)
		r.problem(what, fix)
		r.Next = "The workspace API did not answer: " + fix + "."
		return r, nil
	}
	r.Orchestrator = "answering"
	var ready *platform.BoxBase
	if bases, err := api.Bases(ctx); err == nil {
		for i, b := range bases {
			r.Bases = append(r.Bases, BoxBaseView{Name: b.Name, Status: b.Status, Digest: b.Digest})
			if b.Status == platform.BoxBaseStatusReady && (ready == nil || b.Name == BoxDefaultBaseName) {
				ready = &bases[i]
			}
		}
	} else {
		r.problem("could not read the Box's bases: "+boxErrText(err), "Call zerops_box action=status serviceHostname="+host+" again")
	}
	if pulls, err := api.Operations(ctx, platform.BoxOperationKindPullBase); err == nil {
		if op := runningBoxPull(pulls); op != nil {
			r.Pull = &BoxBaseView{Status: platform.BoxBaseStatusPulling, Operation: op.ID}
		}
	} else {
		r.problem("could not read the Box's base pulls: "+boxErrText(err), "Call zerops_box action=status serviceHostname="+host+" again")
	}
	if ws, err := api.Workspace(ctx); err == nil {
		r.Domains = append(r.Domains, ws.Domains...)
		for _, dom := range ws.Domains {
			if r.Subdomain.IngressHost != "" && strings.EqualFold(dom.Domain, r.Subdomain.IngressHost) {
				r.Subdomain.Registered = true
			}
		}
	} else {
		r.problem("could not read the workspace's domains: "+boxErrText(err), "Call zerops_box action=status serviceHostname="+host+" again")
	}
	r.Next = boxStatusNext(host, r, ready)
	return r, nil
}

// boxStatusNext names the calls that finish a Box's setup, or — when nothing
// is missing — how sandboxes are created.
func boxStatusNext(host string, r BoxStatusResult, ready *platform.BoxBase) string {
	var steps []string
	switch {
	case !r.Subdomain.Access:
		steps = append(steps, boxEnableCall(host), boxSetupCall(host))
	case ready == nil || (r.Subdomain.IngressHost != "" && !r.Subdomain.Registered):
		steps = append(steps, boxSetupCall(host))
	}
	if len(steps) == 0 {
		return boxReadyNext(host, ready.Digest)
	}
	var missing []string
	switch {
	case ready == nil && r.Pull != nil:
		missing = append(missing, "the default base is still pulling (operation "+r.Pull.Operation+")")
	case ready == nil:
		missing = append(missing, "no base is READY")
	}
	switch {
	case !r.Subdomain.Access:
		missing = append(missing, "subdomain access is off, so sandbox ports get no public URL")
	case r.Subdomain.IngressHost != "" && !r.Subdomain.Registered:
		missing = append(missing, "the 8081 subdomain "+r.Subdomain.IngressHost+" is no sandbox domain yet")
	}
	return "The setup is not finished (" + strings.Join(missing, "; ") + "): " + strings.Join(steps, ", then ") + "."
}
