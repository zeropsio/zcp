package tools

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A variant-B pair (docs/spec-mate.md §2.8a.4a) keeps its repository on a code
// volume every dev service of the pair mounts. The pool migration moves it
// like a dev serving several parts — one pool per part — reading the census
// on the volume's checkout, and its retire deletes every dev service, then the
// volume last; CodeVolume is cleared only once the volume reads gone
// (docs/spec-workflows.md §13 PM-13). These tests walk appdev, editing the
// repository on vol at /var/www/ws, and apidev, running the api part from it,
// onto webpool and apipool.

const variantBMount = "/var/www/ws"

// variantBMeta is appdev serving web and api, apidev the api part's dev
// service, both mounting vol.
func variantBMeta() *workflow.ServiceMeta {
	m := poolMigrateMultiPartMeta()
	m.CodeVolume = &topology.CodeVolume{Hostname: "vol", MountPath: variantBMount,
		DevServices: []topology.DevServiceRef{{Hostname: "apidev", SetupName: "dev-api"}}}
	return m
}

// variantBCensus is the census of the volume's checkout, read through a dev
// service's mount: clean, on main, pushed.
func variantBCensus() string {
	return strings.ReplaceAll(poolMigrateCleanCensus(), " /var/www\n", " "+variantBMount+"\n")
}

func variantBVolumeService() platform.ServiceStack {
	return platform.ServiceStack{ID: "svc-vol", Name: "vol", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "local-storage:single@1"}}
}

// variantBFixture is the multi-part fixture on a variant-B pair: apidev holds
// an env of its own and a larger scale than appdev.
func variantBFixture(t *testing.T, web, api partProgress, mutate func(*poolMigrateFixture)) poolMigrateFixture {
	t.Helper()
	return multiPartFixture(t, web, api, func(f *poolMigrateFixture) {
		f.meta = variantBMeta()
		apidev := poolMigrateRuntime("apidev", "svc-api")
		apidev.CustomAutoscaling.MaxRAM = 16
		f.services = append(f.services, apidev, variantBVolumeService())
		f.env["svc-api"] = []platform.ServiceEnvVar{
			{ID: "a-port", Key: "API_PORT", Content: "8080", Type: platform.ServiceEnvUser},
			{ID: "a-secret", Key: "API_SECRET", Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true},
		}
		f.ssh.env["API_SECRET"] = "api-secret-value"
		f.ssh.output = variantBCensus()
		// The api part's pool holds apidev's env once it is past its env step.
		if len(f.env["svc-apipool"]) > len(poolMigratePoolOwnEnv()) {
			f.env["svc-apipool"] = append(poolMigratePoolOwnEnv(),
				platform.ServiceEnvVar{ID: "p-port", Key: "API_PORT", Content: "8080", Type: platform.ServiceEnvUser},
				platform.ServiceEnvVar{ID: "p-api-secret", Key: "API_SECRET", Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true},
			)
		}
		if mutate != nil {
			mutate(f)
		}
	})
}

// TestPoolMigrate_VariantB_CensusOnTheVolumesCheckout pins the read side of a
// variant-B pair: no refusal, the census read on the code volume's checkout
// through the dev's mount, one pool per part, and each part's stanza taking
// the scale of the dev service that runs the part.
func TestPoolMigrate_VariantB_CensusOnTheVolumesCheckout(t *testing.T) {
	t.Parallel()
	f := variantBFixture(t, partAbsent, partAbsent, nil)
	before := snapshotTree(t, f.stateDir)
	out, _ := f.run(t)
	if out["status"] != "step" || out["step"] != "provision" || len(poolMigrateReasons(out)) > 0 {
		t.Fatalf("variant-B pair = %v, want the first part's provision step and no refusal", out)
	}
	if census, _ := out["census"].(map[string]any); census == nil || census["clean"] != true {
		t.Fatalf("census = %v, want the volume checkout's census", out["census"])
	}
	cmds := f.ssh.recorded()
	if len(cmds) != 1 || !strings.HasPrefix(cmds[0], "appdev: cd '"+variantBMount+"'") {
		t.Fatalf("ssh = %q, want one census on appdev at %s", cmds, variantBMount)
	}
	parts := answerParts(t, out)
	if len(parts) != 2 || parts[0]["pool"] != "webpool" || parts[1]["pool"] != "apipool" {
		t.Fatalf("parts = %v, want webpool and apipool", parts)
	}
	if content := poolMigrateNextContent(t, out); !strings.Contains(content, "hostname: webpool") || !strings.Contains(content, "maxRam: 8") {
		t.Errorf("web part's stanza = %s, want webpool with appdev's scale", content)
	}
	if after := snapshotTree(t, f.stateDir); !mapsEqual(before, after) {
		t.Errorf("the read side wrote: before %v after %v", before, after)
	}

	f.input.Pool = "apipool"
	out, _ = f.run(t)
	if content := poolMigrateNextContent(t, out); !strings.Contains(content, "hostname: apipool") || !strings.Contains(content, "maxRam: 16") {
		t.Errorf("api part's stanza = %s, want apipool with apidev's scale", content)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestPoolMigrateEnv_VariantB_EachPartsPoolTakesItsOwnDevServicesEnv: the
// part of apistage runs on apidev, so its pool takes apidev's env — the
// secret read in apidev's container — never appdev's; the image step reads the
// commit on the volume's checkout.
func TestPoolMigrateEnv_VariantB_EachPartsPoolTakesItsOwnDevServicesEnv(t *testing.T) {
	t.Parallel()
	f := variantBFixture(t, partLeased, partProvisioned, nil)
	f.input = WorkflowInput{Workflow: "pool-migrate", Action: actionPoolEnv, Service: "appdev", Pool: "apipool"}
	out, mock := f.runEnv(t)
	if out["action"] != actionPoolEnv || out["pool"] != "apipool" {
		t.Fatalf("env step = %v", out)
	}
	rows, err := mock.GetServiceEnv(context.Background(), "svc-apipool")
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]platform.ServiceEnvVar{}
	for _, r := range rows {
		keys[r.Key] = r
	}
	if keys["API_PORT"].Content != "8080" || !keys["API_SECRET"].Sensitive {
		t.Errorf("apipool env = %v, want apidev's API_PORT and its secret API_SECRET", keys)
	}
	if _, copied := keys["STRIPE_SECRET"]; copied {
		t.Errorf("apipool took appdev's STRIPE_SECRET: %v", keys)
	}
	var secretRead, imageRead string
	for _, c := range f.ssh.recorded() {
		switch {
		case strings.Contains(c, poolMigrateEnvReadMarker):
			secretRead = c
		case strings.Contains(c, poolMigrateImageReadMarker):
			imageRead = c
		}
	}
	if !strings.HasPrefix(secretRead, "apidev: ") {
		t.Errorf("the secret was read as %q, want it read on apidev", secretRead)
	}
	if !strings.Contains(imageRead, "'"+variantBMount+"'") {
		t.Errorf("the image step read %q, want the commit on the volume's checkout", imageRead)
	}
	if strings.Contains(getTextOfJSON(t, out), "api-secret-value") {
		t.Fatal("the secret's value reached the answer")
	}
}

func getTextOfJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The variant-B retire's rig.

// codeVolumeSSH is the variant-B pair over SSH: every dev service mounts the
// volume at /var/www/ws and the volume's own container holds it at /data,
// both the one real repository the retire rig keeps; each bundle,
// fingerprint, census and config read is logged with the host it ran on.
type codeVolumeSSH struct {
	*retireSSH
}

func (s *codeVolumeSSH) rooted(command string) string {
	command = strings.ReplaceAll(command, "'"+variantBMount+"'", "'"+topology.CodeMountRoot+"'")
	return strings.ReplaceAll(command, "'"+ops.CodeVolumeDataRoot+"'", "'"+topology.CodeMountRoot+"'")
}

func (s *codeVolumeSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	switch {
	case strings.Contains(command, "zcp-code-volume-config 1"):
		s.events.add("config on " + hostname)
		out, err := s.local(ctx, s.rooted(command)).CombinedOutput()
		if err != nil {
			return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
		}
		return out, nil
	case strings.Contains(command, "'zcp-bundle 1'"):
		s.events.add("bundle on " + hostname)
	case strings.Contains(command, "'zcp-fingerprint 1'"):
		s.events.add("fingerprint on " + hostname)
	case strings.Contains(command, "zcp-census 1"):
		s.events.add("census on " + hostname)
	}
	return s.retireSSH.ExecSSH(ctx, hostname, s.rooted(command))
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *codeVolumeSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *codeVolumeSSH) StreamSSH(ctx context.Context, hostname, command string, stdout io.Writer) error {
	s.events.add("stream on " + hostname)
	return s.retireSSH.StreamSSH(ctx, hostname, s.rooted(command), stdout)
}

// codeVolumeClient is the platform with every retired service's state by id under
// the test's control. A service gone answers as the platform does: HTTP 400
// serviceStackNotFound.
type codeVolumeClient struct {
	*platform.Mock
	events   *retireEvents
	mu       sync.Mutex
	state    map[string]string
	after    map[string]string
	deleted  []string
	onDelete func(id string)
}

// volumeFailed is the volume's status after a refused delete: it still works.
const volumeFailed = "failed"

func (c *codeVolumeClient) GetService(ctx context.Context, id string) (*platform.ServiceStack, error) {
	c.mu.Lock()
	st := c.state[id]
	c.mu.Unlock()
	switch st {
	case devGone:
		pe := platform.NewPlatformError(platform.ErrAPIError, "Service stack not found.", "")
		pe.APICode = "serviceStackNotFound"
		return nil, pe
	case devDeleting, volumeFailed:
		svc, err := c.Mock.GetService(ctx, id)
		if err != nil {
			return nil, err
		}
		out := *svc
		out.Status = "DELETING"
		if st == volumeFailed {
			out.Status = "FAILED"
		}
		return &out, nil
	}
	return c.Mock.GetService(ctx, id)
}

func (c *codeVolumeClient) DeleteService(ctx context.Context, id string) (*platform.Process, error) {
	c.events.add("delete " + id)
	c.mu.Lock()
	c.deleted = append(c.deleted, id)
	if next, ok := c.after[id]; ok {
		c.state[id] = next
	}
	hook := c.onDelete
	c.mu.Unlock()
	if hook != nil {
		hook(id)
	}
	return c.Mock.DeleteService(ctx, id)
}

func (c *codeVolumeClient) deletes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.deleted)
}

func (c *codeVolumeClient) set(id, state string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state[id] = state
}

// codeVolumeRig is the variant-B pair ready to retire: both parts leased, the
// repository real — its zerops.yaml mounting vol in the dev setups alone —
// and every dev service mounted on this container.
type codeVolumeRig struct {
	r      *retireRig
	client *codeVolumeClient
	ssh    *codeVolumeSSH
}

// variantBConfig is the checkout's zerops.yaml: the dev setups mount the
// volume, the stage and pool setups do not.
const variantBConfig = `zerops:
  - setup: dev
    run:
      base: nodejs@22
      volume:
        hostname: vol
        mountPath: /var/www/ws
  - setup: dev-api
    run:
      base: nodejs@22
      volume:
        hostname: vol
        mountPath: /var/www/ws
  - setup: web
    run:
      base: nodejs@22
  - setup: api
    run:
      base: nodejs@22
`

func newCodeVolumeRig(t *testing.T, config string) *codeVolumeRig {
	t.Helper()
	r := newMultiPartRetireRig(t, partLeased)
	// The rig's fixture is rebuilt on the variant-B pair.
	f := variantBFixture(t, partLeased, partLeased, nil)
	for _, id := range []string{"svc-dev", "svc-api", "svc-vol"} {
		f.mock.WithProcess(&platform.Process{ID: "proc-delete-" + id, Status: platform.ProcessStatusFinished})
	}
	r.f = f
	r.ssh.poolMigrateSSH = f.ssh
	if err := os.WriteFile(filepath.Join(r.ssh.repo, "zerops.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	r.mounter.mounted = map[string]bool{"appdev": true, "apidev": true}
	client := &codeVolumeClient{
		Mock: f.mock, events: r.events,
		state: map[string]string{},
		after: map[string]string{"svc-dev": devGone, "svc-api": devGone, "svc-vol": devGone},
	}
	return &codeVolumeRig{r: r, client: client, ssh: &codeVolumeSSH{retireSSH: r.ssh}}
}

func (v *codeVolumeRig) deps() poolRetireDeps {
	d := v.r.multiPartDeps()
	d.client = v.client
	d.ssh = v.ssh
	return d
}

func variantBAck() *DestructiveAck {
	return &DestructiveAck{Operation: poolRetireOperation, AcknowledgedTargets: []string{"appdev", "apidev", "vol"}}
}

func variantBRetireInput(ack, force bool) WorkflowInput {
	in := retireInput(false, force)
	if ack {
		in.ConfirmDestructive = variantBAck()
	}
	return in
}

func (v *codeVolumeRig) retire(t *testing.T, input WorkflowInput) (map[string]any, bool) {
	t.Helper()
	result, typed, err := poolMigrateRetireRun(context.Background(), v.deps(), input)
	if err != nil || typed != nil {
		t.Fatalf("retire: %v, typed %T", err, typed)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	return out, result.IsError
}

// assertVolumeKept pins the pairs while the volume is not gone: split one per
// part, webpool's pair still naming the code volume.
func (v *codeVolumeRig) assertVolumeKept(t *testing.T, devServices []string) {
	t.Helper()
	web, _ := workflow.ReadServiceMeta(v.r.f.stateDir, "webpool")
	if web == nil || !web.DevIsPool() || web.CodeVolume == nil || web.CodeVolume.Hostname != "vol" {
		t.Fatalf("webpool's pair = %+v, want it keyed by the pool, the code volume still recorded", web)
	}
	if got := web.CodeVolume.DevServiceHostnames(); !slices.Equal(got, devServices) {
		t.Errorf("webpool's pair names dev services %q, want %q", got, devServices)
	}
}

// assertVolumeCleared pins the pairs once the volume is gone: split one per
// part, neither naming a code volume or a dev service.
func (v *codeVolumeRig) assertVolumeCleared(t *testing.T) {
	t.Helper()
	for _, pool := range []string{"webpool", "apipool"} {
		m, _ := workflow.ReadServiceMeta(v.r.f.stateDir, pool)
		if m == nil || !m.DevIsPool() || m.CodeVolume != nil {
			t.Fatalf("%s's pair = %+v, want it keyed by the pool with no code volume", pool, m)
		}
	}
	for _, host := range []string{"appdev", "apidev"} {
		if m, _ := workflow.FindServiceMeta(v.r.f.stateDir, host); m != nil {
			t.Errorf("%s still resolves to a pair: %+v", host, m)
		}
	}
}

// TestPoolMigrateRetire_VariantB_DeletesEveryDevServiceThenTheVolumeLast pins
// the variant-B retire: the diagnosis names every dev service and the code
// volume, each by the id it is deleted by; acknowledged, it bundles the
// volume's checkout through the dev's mount — the volume's own reading of it
// at /data taken before the bundle streams — checks the fingerprint, deletes appdev then apidev — each
// read gone by id — re-reads the checkout in the volume's container, deletes
// the volume last and clears CodeVolume once it reads gone.
func TestPoolMigrateRetire_VariantB_DeletesEveryDevServiceThenTheVolumeLast(t *testing.T) {
	t.Parallel()
	v := newCodeVolumeRig(t, variantBConfig)

	out, isErr := v.retire(t, variantBRetireInput(false, false))
	if !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("first call = %v, want DIAGNOSIS_REQUIRED", out)
	}
	wd, _ := out["wouldDestroy"].(map[string]any)
	if got := retireStrings(wd["targets"]); !slices.Equal(got, []string{"appdev", "apidev", "vol"}) {
		t.Fatalf("targets = %q, want every dev service and the code volume", got)
	}
	loss, _ := wd["wouldDestroy"].(map[string]any)
	if got := retireStrings(loss["serviceIds"]); !slices.Equal(got, []string{"svc-dev", "svc-api", "svc-vol"}) {
		t.Fatalf("serviceIds = %q", got)
	}
	if notKept := strings.Join(retireStrings(loss["notKept"]), " "); !strings.Contains(notKept, "vol") || !strings.Contains(notKept, "apidev") {
		t.Errorf("notKept = %q, want the volume and apidev named", notKept)
	}
	if len(v.client.deletes()) > 0 {
		t.Fatal("the diagnosis deleted")
	}

	// The acknowledgment names every target: the dev alone does not do.
	if out, isErr := v.retire(t, retireInput(true, false)); !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("an acknowledgment naming the dev alone = %v, want DIAGNOSIS_REQUIRED", out)
	}

	out, isErr = v.retire(t, variantBRetireInput(true, false))
	if isErr || out["step"] != "retired" {
		t.Fatalf("confirmed = %v, want retired", out)
	}
	if got := v.client.deletes(); !slices.Equal(got, []string{"svc-dev", "svc-api", "svc-vol"}) {
		t.Fatalf("deletes = %q, want appdev, apidev, then the volume", got)
	}
	events := v.r.events.all()
	order := []string{"unmount appdev", "unmount apidev", "fingerprint on vol", "bundle on appdev", "fingerprint on appdev", "delete svc-dev", "delete svc-api", "fingerprint on vol", "delete svc-vol"}
	at := 0
	for _, want := range order {
		i := slices.Index(events[at:], want)
		if i < 0 {
			t.Fatalf("events = %q, want %q in that order", events, order)
		}
		at += i + 1
	}
	rec := retireRecordOf(t, v.r.f.stateDir)
	if rec.Phase != poolRetireRetired || rec.CodeVolume == nil || rec.CodeVolume.ServiceID != "svc-vol" ||
		len(rec.DevServices) != 1 || rec.DevServices[0].ServiceID != "svc-api" || rec.Bundle == nil {
		t.Fatalf("record = %+v, want retired, naming apidev and the volume by id, the bundle kept", rec)
	}
	v.assertVolumeCleared(t)
	if why, _ := out["nextUnavailable"].(string); !strings.Contains(why, "vol") || !strings.Contains(why, "apidev") {
		t.Errorf("nextUnavailable = %q, want apidev and the volume named", why)
	}
}

// TestPoolMigrateRetire_VariantB_VolumeStillMounted_KeepsTheCodeVolume pins the
// platform's async refusal: the volume's delete answers a process that fails
// with containerAction "still mounted", naming no service. The retire keeps
// CodeVolume and its record at deleting-volume, says some service still
// mounts the volume — the platform does not say which — and the next call,
// once nothing mounts it, deletes the volume and clears CodeVolume.
func TestPoolMigrateRetire_VariantB_VolumeStillMounted_KeepsTheCodeVolume(t *testing.T) {
	t.Parallel()
	v := newCodeVolumeRig(t, variantBConfig)
	reason := "containerAction: This Local Storage volume is still mounted by other services. Delete them first, or remove run.volume from their zerops.yaml and deploy."
	v.client.after["svc-vol"] = volumeFailed
	v.r.f.mock.WithProcess(&platform.Process{ID: "proc-delete-svc-vol", Status: platform.ProcessStatusFailed, FailReason: &reason})

	out, isErr := v.retire(t, variantBRetireInput(true, false))
	if isErr || out["status"] != "step" || out["step"] != "retiring" {
		t.Fatalf("retire = %v, want retiring while the volume is still mounted", out)
	}
	if reasons := poolMigrateReasons(out); !slices.Equal(reasons, []string{"volume-mounted"}) {
		t.Fatalf("reasons = %q, want volume-mounted", reasons)
	}
	msg := poolMigrateRefusalMessage(out, "volume-mounted")
	for _, want := range []string{"vol", "still mount", "does not say which", "run.volume"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not say %q", msg, want)
		}
	}
	rec := retireRecordOf(t, v.r.f.stateDir)
	if rec.Phase != poolRetireDeletingVolume || rec.CodeVolume == nil || !strings.Contains(rec.CodeVolume.Refused, "still mounted") {
		t.Fatalf("record = %+v, want it kept at deleting-volume with the refusal", rec)
	}
	v.assertVolumeKept(t, nil)
	block := retireBlock(t, out)
	if cv, _ := block["codeVolume"].(map[string]any); cv == nil || cv["hostname"] != "vol" || !strings.Contains(cv["refused"].(string), "still mounted") {
		t.Errorf("retire.codeVolume = %v, want the volume and its refusal", block["codeVolume"])
	}

	// The read side answers from the record — named by a stage, by the gone
	// dev service or by the volume: retiring.
	for _, named := range []string{"webstage", "apidev", "vol"} {
		read, _, err := poolMigrateRun(context.Background(), v.deps().poolMigrateDeps, WorkflowInput{Workflow: "pool-migrate", Service: named})
		if err != nil {
			t.Fatal(err)
		}
		if text := getTextContent(t, read); !strings.Contains(text, `"step":"retiring"`) {
			t.Errorf("read side named by %s = %s, want retiring", named, text)
		}
	}

	// Nothing mounts it any more: the next call deletes it.
	v.r.f.mock.WithProcess(&platform.Process{ID: "proc-delete-svc-vol", Status: platform.ProcessStatusFinished})
	v.client.after["svc-vol"] = devGone
	out, isErr = v.retire(t, variantBRetireInput(true, false))
	if isErr || out["step"] != "retired" {
		t.Fatalf("the next call = %v, want retired", out)
	}
	if got := v.client.deletes(); !slices.Equal(got, []string{"svc-dev", "svc-api", "svc-vol", "svc-vol"}) {
		t.Fatalf("deletes = %q, want the volume deleted again by its id, no dev service twice", got)
	}
	v.assertVolumeCleared(t)
}

// seedVolumeRecord writes the record a variant-B retire left in phase, with
// a real bundle of the repository and the volume's own fingerprint of it;
// the pair split as the retire leaves it, apidev dropped from it once the
// dev services are gone.
func (v *codeVolumeRig) seedVolumeRecord(t *testing.T, phase string) {
	t.Helper()
	ctx := context.Background()
	b, err := ops.BuildPoolMigrateBundle(ctx, v.ssh, ops.PoolMigrateBundleInput{
		Dev: "appdev", Root: variantBMount, WorkDir: v.r.work, Path: poolRetireBundlePath(v.r.f.stateDir, "appdev"),
	})
	if err != nil {
		t.Fatalf("seed bundle: %v", err)
	}
	fp, err := ops.ReadPoolMigrateFingerprint(ctx, v.ssh, "vol", ops.CodeVolumeDataRoot, v.r.work)
	if err != nil {
		t.Fatalf("seed the volume's fingerprint: %v", err)
	}
	rec := &poolRetireRecord{
		Dev: "appdev", DevServiceID: "svc-dev", Stages: []string{"webstage", "apistage"},
		Parts: []poolRetirePart{
			{Stage: "webstage", Pool: "webpool", PoolServiceID: "svc-webpool", PoolSetup: "webpool"},
			{Stage: "apistage", Pool: "apipool", PoolServiceID: "svc-apipool", PoolSetup: "apipool"},
		},
		DevServices: []poolRetireService{{Hostname: "apidev", ServiceID: "svc-api"}},
		CodeVolume:  &poolRetireVolume{Hostname: "vol", ServiceID: "svc-vol", MountPath: variantBMount, Fingerprint: &fp},
		Phase:       phase, StartedAt: "2026-10-24T10:00:00Z", Bundle: b,
	}
	if phase == poolRetireDeleting || phase == poolRetireDeletingVolume {
		if _, err := workflow.MigratePairToPools(v.r.f.stateDir, "appdev", rec.partMoves()); err != nil {
			t.Fatal(err)
		}
	}
	if phase == poolRetireDeletingVolume {
		if err := workflow.SettleMigratedCodeVolume(v.r.f.stateDir, "appdev", rec.pools(), false); err != nil {
			t.Fatal(err)
		}
		v.client.set("svc-dev", devGone)
		v.client.set("svc-api", devGone)
	}
	if err := savePoolRetireRecord(v.r.f.stateDir, rec); err != nil {
		t.Fatal(err)
	}
	v.r.events.log = nil
}

// TestPoolMigrateRetire_VariantB_ResumeTable walks the volume's resume rows —
// a retire stopped at deleting-volume against every state the volume reads by
// id: live or FAILED (a refused delete leaves it working) deletes it again by
// its id, after re-reading its checkout at /data; DELETING keeps the record
// and says call again; gone finishes and clears CodeVolume — and the rows of
// a retire stopped at deleting while the dev services go: each dev service's
// own state by id decides, one already gone is never deleted twice.
func TestPoolMigrateRetire_VariantB_ResumeTable(t *testing.T) {
	t.Parallel()
	for _, state := range []string{devLive, volumeFailed, devDeleting, devGone} {
		t.Run("deleting-volume/"+state, func(t *testing.T) {
			t.Parallel()
			v := newCodeVolumeRig(t, variantBConfig)
			v.seedVolumeRecord(t, poolRetireDeletingVolume)
			v.client.set("svc-vol", state)

			out, isErr := v.retire(t, variantBRetireInput(true, false))
			rec := retireRecordOf(t, v.r.f.stateDir)
			switch state {
			case devDeleting:
				if isErr || out["step"] != "retiring" || rec.Phase != poolRetireDeletingVolume || len(v.client.deletes()) > 0 {
					t.Fatalf("out %v, record %s, deletes %q; want retiring, the record kept, nothing deleted", out, rec.Phase, v.client.deletes())
				}
				v.assertVolumeKept(t, nil)
				return
			case devGone:
				if len(v.client.deletes()) > 0 {
					t.Fatalf("a gone volume was deleted again: %q", v.client.deletes())
				}
			default:
				if got := v.client.deletes(); !slices.Equal(got, []string{"svc-vol"}) {
					t.Fatalf("deletes = %q, want the volume by its recorded id", got)
				}
				if v.r.events.index("fingerprint on vol") < 0 {
					t.Errorf("no re-read of the checkout at /data before the volume's delete: %q", v.r.events.all())
				}
			}
			if isErr || out["step"] != "retired" || rec.Phase != poolRetireRetired {
				t.Fatalf("out %v, record %+v; want retired", out, rec)
			}
			v.assertVolumeCleared(t)
		})
	}
	for _, tc := range []struct {
		name    string
		gone    []string
		deletes []string
	}{
		{name: "every dev service live", deletes: []string{"svc-dev", "svc-api", "svc-vol"}},
		{name: "the dev gone, apidev live", gone: []string{"svc-dev"}, deletes: []string{"svc-api", "svc-vol"}},
		{name: "every dev service gone", gone: []string{"svc-dev", "svc-api"}, deletes: []string{"svc-vol"}},
	} {
		t.Run("deleting/"+tc.name, func(t *testing.T) {
			t.Parallel()
			v := newCodeVolumeRig(t, variantBConfig)
			v.seedVolumeRecord(t, poolRetireDeleting)
			for _, id := range tc.gone {
				v.client.set(id, devGone)
			}
			// What is left to delete is deleted only on the acknowledged
			// call.
			if out, isErr := v.retire(t, variantBRetireInput(false, false)); !isErr || out["code"] != platform.ErrDiagnosisRequired || len(v.client.deletes()) > 0 {
				t.Fatalf("an unacknowledged resume = %v, deletes %q; want the diagnosis, nothing deleted", out, v.client.deletes())
			}
			out, isErr := v.retire(t, variantBRetireInput(true, false))
			if isErr || out["step"] != "retired" {
				t.Fatalf("resume = %v, want retired", out)
			}
			if got := v.client.deletes(); !slices.Equal(got, tc.deletes) {
				t.Fatalf("deletes = %q, want %q", got, tc.deletes)
			}
			v.assertVolumeCleared(t)
		})
	}
}

// TestPoolMigrateRetire_VariantB_AStageMountingTheVolume_Refused: a stage or
// pool whose setup in the checkout's zerops.yaml mounts the code volume would
// hold the volume's delete after every dev service is gone, so the retire
// refuses before anything — naming the service and its setup — and writes
// nothing.
func TestPoolMigrateRetire_VariantB_AStageMountingTheVolume_Refused(t *testing.T) {
	t.Parallel()
	config := strings.Replace(variantBConfig, "  - setup: web\n    run:\n      base: nodejs@22\n",
		"  - setup: web\n    run:\n      base: nodejs@22\n      volume:\n        hostname: vol\n        mountPath: /var/www/ws\n", 1)
	v := newCodeVolumeRig(t, config)
	out, isErr := v.retire(t, variantBRetireInput(true, true))
	if isErr || out["status"] != "retire-refused" || !slices.Contains(poolMigrateReasons(out), "volume-mounted") {
		t.Fatalf("retire = %v, want retire-refused for the mounting stage", out)
	}
	msg := poolMigrateRefusalMessage(out, "volume-mounted")
	for _, want := range []string{"webstage", "web", "run.volume", "vol"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not name %q", msg, want)
		}
	}
	if rec, _ := readPoolRetireRecord(v.r.f.stateDir, "appdev"); rec != nil {
		t.Errorf("a record was written: %+v", rec)
	}
	if len(v.client.deletes()) > 0 || v.r.events.index("unmount") >= 0 {
		t.Errorf("the retire acted: deletes %q, events %q", v.client.deletes(), v.r.events.all())
	}
}

// TestPoolMigrateRetire_VariantB_VolumeMovedWhileTheDevServicesWent_NeedsForce:
// the volume's checkout moved after its bundle — written while the dev
// services were being deleted. Nothing on it is deleted: the retire keeps its
// record at deleting-volume and names what moved; re-sent with force, once
// the person agreed, it takes a new verified bundle from the volume itself
// and deletes it.
func TestPoolMigrateRetire_VariantB_VolumeMovedWhileTheDevServicesWent_NeedsForce(t *testing.T) {
	t.Parallel()
	v := newCodeVolumeRig(t, variantBConfig)
	v.client.onDelete = func(id string) {
		if id == "svc-api" {
			if err := os.WriteFile(filepath.Join(v.r.ssh.repo, "app.txt"), []byte("v1\nwritten while the dev services went\n"), 0o644); err != nil {
				t.Error(err)
			}
		}
	}
	out, isErr := v.retire(t, variantBRetireInput(true, false))
	if isErr || out["step"] != "retiring" || !slices.Equal(poolMigrateReasons(out), []string{"fingerprint-changed"}) {
		t.Fatalf("retire = %v, want retiring, held by the moved checkout", out)
	}
	if got := v.client.deletes(); !slices.Equal(got, []string{"svc-dev", "svc-api"}) {
		t.Fatalf("deletes = %q, want the dev services only", got)
	}
	if rec := retireRecordOf(t, v.r.f.stateDir); rec.Phase != poolRetireDeletingVolume {
		t.Fatalf("record phase = %s, want deleting-volume", rec.Phase)
	}
	v.assertVolumeKept(t, nil)

	out, isErr = v.retire(t, variantBRetireInput(true, true))
	if isErr || out["step"] != "retired" {
		t.Fatalf("forced = %v, want retired", out)
	}
	rec := retireRecordOf(t, v.r.f.stateDir)
	if rec.CodeVolume == nil || rec.CodeVolume.Bundle == nil || !strings.HasPrefix(rec.CodeVolume.Bundle.Path, filepath.Join(v.r.f.stateDir, "migrate")) {
		t.Fatalf("record = %+v, want a second bundle taken from the volume", rec.CodeVolume)
	}
	if i, j := v.r.events.index("stream on vol"), v.r.events.index("delete svc-vol"); i < 0 || i > j {
		t.Fatalf("events = %q, want the volume's bundle streamed before its delete", v.r.events.all())
	}
	v.assertVolumeCleared(t)
}

// TestPoolMigrateRetire_VariantB_WrittenWhileTheVolumeRebundles_NothingDeleted:
// the forced call's new bundle of the volume records the fingerprint read
// before it streamed, so a write that lands while the bundle streams and is
// verified is in no bundle. The checkout is read again after the new bundle,
// right before the volume's delete: a checkout that moved meanwhile holds the
// delete, as it does before the first (PM-5, PM-13).
func TestPoolMigrateRetire_VariantB_WrittenWhileTheVolumeRebundles_NothingDeleted(t *testing.T) {
	t.Parallel()
	v := newCodeVolumeRig(t, variantBConfig)
	v.client.onDelete = func(id string) {
		if id == "svc-api" {
			if err := os.WriteFile(filepath.Join(v.r.ssh.repo, "app.txt"), []byte("v1\nwritten while the dev services went\n"), 0o644); err != nil {
				t.Error(err)
			}
		}
	}
	if out, _ := v.retire(t, variantBRetireInput(true, false)); out["step"] != "retiring" {
		t.Fatalf("retire = %v, want retiring, held by the moved checkout", out)
	}

	v.r.ssh.afterStream = func() {
		if err := os.WriteFile(filepath.Join(v.r.ssh.repo, "app.txt"), []byte("v1\nwritten while the volume's bundle streamed\n"), 0o644); err != nil {
			t.Error(err)
		}
	}
	out, isErr := v.retire(t, variantBRetireInput(true, true))
	if got := v.client.deletes(); slices.Contains(got, "svc-vol") {
		t.Fatalf("deletes = %q: the volume was deleted with a write its new bundle does not hold", got)
	}
	if isErr || out["step"] != "retiring" || !slices.Equal(poolMigrateReasons(out), []string{"fingerprint-changed"}) {
		t.Fatalf("forced = %v, want retiring, held by the checkout that moved while it was bundled", out)
	}
	if i := v.r.events.index("stream on vol"); i < 0 {
		t.Fatalf("events = %q, want the volume rebundled", v.r.events.all())
	}
	if rec := retireRecordOf(t, v.r.f.stateDir); rec.Phase != poolRetireDeletingVolume {
		t.Fatalf("record phase = %s, want deleting-volume", rec.Phase)
	}
}

// TestPoolMigrateRetire_VariantB_WrittenWhileTheDevBundleStreams_VolumeKept:
// the volume's own reading — the baseline its delete is checked against — is
// taken before the dev's bundle streams. A write that lands while the bundle
// streams is in no bundle; with a further dev service gone outside the retire
// meanwhile the dev's fingerprint is never re-checked, so only the volume's
// reading can catch it: the volume is not deleted unforced (PM-13).
func TestPoolMigrateRetire_VariantB_WrittenWhileTheDevBundleStreams_VolumeKept(t *testing.T) {
	t.Parallel()
	v := newCodeVolumeRig(t, variantBConfig)
	v.r.ssh.afterStream = func() {
		if err := os.WriteFile(filepath.Join(v.r.ssh.repo, "app.txt"), []byte("v1\nwritten while the dev's bundle streamed\n"), 0o644); err != nil {
			t.Error(err)
		}
		v.client.set("svc-api", devGone) // apidev deleted outside the retire
	}
	out, isErr := v.retire(t, variantBRetireInput(true, false))
	if got := v.client.deletes(); slices.Contains(got, "svc-vol") {
		t.Fatalf("deletes = %q: the volume was deleted with a write no bundle holds", got)
	}
	if isErr || out["step"] != "retiring" || !slices.Equal(poolMigrateReasons(out), []string{"fingerprint-changed"}) {
		t.Fatalf("retire = %v, want retiring, held by the checkout that moved while the bundle streamed", out)
	}
	fp, bundle := v.r.events.index("fingerprint on vol"), v.r.events.index("bundle on appdev")
	if fp < 0 || bundle < 0 || fp > bundle {
		t.Fatalf("events = %q, want the volume's reading taken before the dev's bundle", v.r.events.all())
	}
}

// TestDeleteTool_RetireRecord_VariantB_DevServiceAndVolumeRefused: while a
// variant-B retire is in flight, zerops_delete refuses its further dev
// services and its code volume as it refuses the dev, pointing at the retire.
func TestDeleteTool_RetireRecord_VariantB_DevServiceAndVolumeRefused(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"apidev", "vol"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if err := savePoolRetireRecord(stateDir, &poolRetireRecord{Dev: "appdev", DevServiceID: "svc-dev", Phase: poolRetireDeleting,
				DevServices: []poolRetireService{{Hostname: "apidev", ServiceID: "svc-api"}},
				CodeVolume:  &poolRetireVolume{Hostname: "vol", ServiceID: "svc-vol", MountPath: variantBMount}}); err != nil {
				t.Fatal(err)
			}
			mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-api", Name: "apidev"}, {ID: "svc-vol", Name: "vol"}})
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDelete(srv, mock, "proj-1", stateDir, nil, runtime.Info{})
			result := callTool(t, srv, "zerops_delete", map[string]any{"serviceHostname": host, "consent": host})
			text := getTextContent(t, result)
			if !result.IsError || !strings.Contains(text, `action=\"retire\"`) || !strings.Contains(text, `service=\"appdev\"`) {
				t.Fatalf("delete of %s during the retire = %s, want a refusal pointing at the retire of appdev", host, text)
			}
			if mock.CallCounts["DeleteService"] > 0 {
				t.Fatal("the service was deleted")
			}
		})
	}
}

// TestPoolRetireDevState_TheLiveNotFoundShape: a by-id read of a deleted
// service answers HTTP 400 serviceStackNotFound (live-verified), which the
// retire reads as gone, never as an error to call again on.
func TestPoolRetireDevState_TheLiveNotFoundShape(t *testing.T) {
	t.Parallel()
	c := &codeVolumeClient{Mock: platform.NewMock(), events: &retireEvents{}, state: map[string]string{"svc-dev": devGone}}
	got, err := poolRetireDevState(context.Background(), c, "svc-dev")
	if err != nil || got != poolRetireDevGone {
		t.Fatalf("state = %q, err %v; want gone", got, err)
	}
}

// TestPoolMigrateEnv_VariantB_EveryPartsPoolGetsThePairsGitToken: a variant-B
// pair is one repository — the volume's checkout — and git-push-setup writes
// its one GIT_TOKEN on the dev half, the service it pushes from. The api part
// takes apidev's env, but its pool is offered appdev's GIT_TOKEN though apidev
// holds none; on the person's yes zcp reads it in appdev's container and
// copies it, and the read side then answers that the pool holds it.
func TestPoolMigrateEnv_VariantB_EveryPartsPoolGetsThePairsGitToken(t *testing.T) {
	t.Parallel()
	f := variantBFixture(t, partLeased, partProvisioned, func(f *poolMigrateFixture) {
		f.env["svc-dev"] = append(f.env["svc-dev"], platform.ServiceEnvVar{ID: "d-git", Key: poolEnvGitTokenKey, Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true})
	})
	f.input = WorkflowInput{Workflow: "pool-migrate", Service: "appdev", Pool: "apipool"}
	read, _ := f.run(t)
	git, _ := poolEnvBlock(t, read)["gitToken"].(map[string]any)
	if msg, _ := git["message"].(string); git["state"] != ops.PoolGitTokenAsk || !strings.Contains(msg, "appdev's GIT_TOKEN") {
		t.Fatalf("api part's gitToken = %v, want the pair's GIT_TOKEN on appdev offered", git)
	}

	f.input.Action, f.input.CopyGitToken = actionPoolEnv, true
	out, mock := f.runEnv(t)
	if git, _ := poolEnvBlock(t, out)["gitToken"].(map[string]any); git["state"] != ops.PoolGitTokenCopied {
		t.Fatalf("gitToken after yes = %v, want copied", git)
	}
	rows, err := mock.GetServiceEnv(context.Background(), "svc-apipool")
	if err != nil {
		t.Fatal(err)
	}
	var token, secret platform.ServiceEnvVar
	for _, r := range rows {
		switch r.Key {
		case poolEnvGitTokenKey:
			token = r
		case "API_SECRET":
			secret = r
		}
	}
	if token.Content != poolEnvGitToken || !token.Sensitive {
		t.Errorf("apipool's GIT_TOKEN: sensitive=%v exact=%v", token.Sensitive, token.Content == poolEnvGitToken)
	}
	if !secret.Sensitive {
		t.Errorf("apipool lacks apidev's API_SECRET: %v", rows)
	}
	for _, c := range f.ssh.recorded() {
		if !strings.Contains(c, poolMigrateEnvReadMarker) {
			continue
		}
		readsToken := strings.Contains(c, "'"+poolEnvGitTokenKey+"'")
		switch {
		case readsToken && !strings.HasPrefix(c, "appdev: "):
			t.Errorf("GIT_TOKEN was read as %q, want it read on appdev", c)
		case strings.Contains(c, "'API_SECRET'") && !strings.HasPrefix(c, "apidev: "):
			t.Errorf("API_SECRET was read as %q, want it read on apidev", c)
		}
	}
	if strings.Contains(getTextOfJSON(t, out), poolEnvGitToken) {
		t.Fatal("the GIT_TOKEN's value reached the answer")
	}

	f.input.Action, f.input.CopyGitToken = "", false
	again, _ := f.run(t)
	if git, _ := poolEnvBlock(t, again)["gitToken"].(map[string]any); git["state"] != ops.PoolGitTokenUnchanged {
		t.Errorf("read side after the copy: gitToken = %v, want unchanged", git)
	}
}

// TestPoolRetireRecordAnswer_Retired_NamesTheBundleToRestoreFrom: the retired
// answer names the bundle holding the newest state of the repository as the
// one to restore from. A retire forced past a code volume whose checkout moved
// after the first bundle took a second one from the volume itself: that one
// holds the late writes, so the answer names it with retire.codeVolume.restore
// and the first as the older; an agent following the sentence never restores
// the older checkout.
func TestPoolRetireRecordAnswer_Retired_NamesTheBundleToRestoreFrom(t *testing.T) {
	t.Parallel()
	first := &ops.PoolMigrateBundle{Path: "/state/migrate/appdev.bundle"}
	volume := &ops.PoolMigrateBundle{Path: "/state/migrate/appdev.volume.bundle"}
	tests := []struct {
		name    string
		volume  *poolRetireVolume
		want    []string
		notWant []string
	}{
		{name: "a dev service of its own", want: []string{first.Path, "retire.restore"}, notWant: []string{"codeVolume"}},
		{
			name:    "a code volume, one bundle",
			volume:  &poolRetireVolume{Hostname: "vol", ServiceID: "svc-vol", MountPath: variantBMount},
			want:    []string{first.Path, "retire.restore"},
			notWant: []string{"volume.bundle", "codeVolume.restore"},
		},
		{
			name:    "a code volume forced past its moved checkout",
			volume:  &poolRetireVolume{Hostname: "vol", ServiceID: "svc-vol", MountPath: variantBMount, Bundle: volume, Restore: []string{"git init -q ."}},
			want:    []string{volume.Path, "retire.codeVolume.restore", "newer", first.Path, "older"},
			notWant: []string{"The bundle of its repository is " + first.Path + "; retire.restore puts it back"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &poolRetireRecord{
				Dev: "appdev", DevServiceID: "svc-dev", Stages: []string{"webstage", "apistage"},
				Parts: []poolRetirePart{
					{Stage: "webstage", Pool: "webpool", PoolServiceID: "svc-webpool"},
					{Stage: "apistage", Pool: "apipool", PoolServiceID: "svc-apipool"},
				},
				Phase: poolRetireRetired, Forced: tt.volume != nil && tt.volume.Bundle != nil, Bundle: first,
				Restore: []string{"git init -q ."},
			}
			if tt.volume != nil {
				rec.DevServices = []poolRetireService{{Hostname: "apidev", ServiceID: "svc-api"}}
				rec.CodeVolume = tt.volume
			}
			msg := poolRetireRecordAnswer(poolMigrateResponse{}, rec).NextUnavailable
			for _, w := range tt.want {
				if !strings.Contains(msg, w) {
					t.Errorf("retired answer %q does not say %q", msg, w)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(msg, w) {
					t.Errorf("retired answer %q says %q", msg, w)
				}
			}
		})
	}
}

// TestPoolMigrateRetire_VariantB_VolumeMounted_NamesEachMountingService: the
// volume-mounted refusal names each stage or pool whose setup in the
// checkout's zerops.yaml mounts the code volume, and its words agree with how
// many there are.
func TestPoolMigrateRetire_VariantB_VolumeMounted_NamesEachMountingService(t *testing.T) {
	t.Parallel()
	mountIn := func(config, setup string) string {
		block := "  - setup: " + setup + "\n    run:\n      base: nodejs@22\n"
		return strings.Replace(config, block, block+"      volume:\n        hostname: vol\n        mountPath: /var/www/ws\n", 1)
	}
	tests := []struct {
		name   string
		config string
		want   []string
	}{
		{
			name:   "one stage",
			config: mountIn(variantBConfig, "web"),
			want:   []string{"stage webstage (setup web) mounts the code volume vol", "Remove run.volume from that setup", "deploy the service"},
		},
		{
			name:   "two stages",
			config: mountIn(mountIn(variantBConfig, "web"), "api"),
			want:   []string{"stage webstage (setup web) and stage apistage (setup api) mount the code volume vol", "Remove run.volume from those setups", "deploy each service"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := newCodeVolumeRig(t, tt.config)
			out, isErr := v.retire(t, variantBRetireInput(true, true))
			if isErr || out["status"] != "retire-refused" {
				t.Fatalf("retire = %v, want retire-refused", out)
			}
			msg := poolMigrateRefusalMessage(out, "volume-mounted")
			for _, w := range tt.want {
				if !strings.Contains(msg, w) {
					t.Errorf("message %q does not say %q", msg, w)
				}
			}
		})
	}
}

// TestPoolMigrateRetire_VariantB_NotKept_NamesTheBundledPath: the bundle of a
// variant-B pair reads the checkout at the code volume's mount, so what the
// dev's delete loses of its container is everything outside that mount —
// /var/www itself among it — and notKept names the mount the bundle read, in
// the diagnosis and in the record alike.
func TestPoolMigrateRetire_VariantB_NotKept_NamesTheBundledPath(t *testing.T) {
	t.Parallel()
	v := newCodeVolumeRig(t, variantBConfig)
	want := "files on appdev's container outside " + variantBMount
	out, isErr := v.retire(t, variantBRetireInput(false, false))
	if !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("first call = %v, want DIAGNOSIS_REQUIRED", out)
	}
	wd, _ := out["wouldDestroy"].(map[string]any)
	loss, _ := wd["wouldDestroy"].(map[string]any)
	if notKept := retireStrings(loss["notKept"]); !slices.Contains(notKept, want) {
		t.Errorf("diagnosis notKept = %q, want %q", notKept, want)
	}
	if out, isErr := v.retire(t, variantBRetireInput(true, false)); isErr || out["step"] != "retired" {
		t.Fatalf("confirmed = %v, want retired", out)
	}
	if rec := retireRecordOf(t, v.r.f.stateDir); !slices.Contains(rec.NotKept, want) {
		t.Errorf("record notKept = %q, want %q", rec.NotKept, want)
	}
}

// TestPoolMigrate_VariantB_EveryPartReady_TheRetireCallNamesEveryServiceItDeletes:
// once every part of a variant-B pair is ready to retire, the retire call's
// why names what the retire deletes as its diagnosis does — the dev, every
// further dev service and, last, the code volume — never the dev alone.
func TestPoolMigrate_VariantB_EveryPartReady_TheRetireCallNamesEveryServiceItDeletes(t *testing.T) {
	t.Parallel()
	f := variantBFixture(t, partLeased, partLeased, nil)
	out, _ := f.run(t)
	next, _ := out["next"].(map[string]any)
	args, _ := next["args"].(map[string]any)
	if out["step"] != poolMigrateStepReadyToRetire || args["action"] != actionRetire {
		t.Fatalf("answer = %v, want ready-to-retire naming the retire", out)
	}
	why, _ := next["why"].(string)
	for _, w := range []string{"appdev", "apidev", "the code volume vol"} {
		if !strings.Contains(why, w) {
			t.Errorf("why %q does not name %q", why, w)
		}
	}
}
