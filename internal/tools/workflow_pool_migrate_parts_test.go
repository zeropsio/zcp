package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A dev service serving several parts — the monorepo model: one repository,
// one dev service running every part's dev servers, a stage per part — moves
// onto one pool per part (docs/spec-workflows.md §13 PM-12). These tests walk
// appdev, serving web (stage webstage) and api (stage apistage), onto webpool
// and apipool.

// poolMigrateMultiPartMeta is appdev serving the web and api parts.
func poolMigrateMultiPartMeta() *workflow.ServiceMeta {
	m := poolMigratePairMeta()
	m.Stages = []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}
	return m
}

// multiPartYAML is a zerops.yaml whose two pool setups each pass every image
// check for a pool of three containers and a previewPortBase of 4000.
func multiPartYAML() string {
	ready := poolMigrateReadyYAML()
	_, poolSetup, _ := strings.Cut(ready, "  - setup: apppool\n")
	devSetup, _, _ := strings.Cut(ready, "  - setup: apppool\n")
	return devSetup + "  - setup: webpool\n" + poolSetup + "  - setup: apipool\n" + poolSetup
}

// multiPartDeclaration gives each part's dev server its pool as service.
const multiPartDeclaration = `{"version": 1, "previewPortBase": 4000, "servers": [` +
	`{"name": "web", "port": 3000, "command": "npm run dev", "cwd": "apps/web", "service": "webpool"},` +
	`{"name": "api", "port": 8080, "command": "go run .", "cwd": "apps/api", "service": "apipool"}]}`

// partProgress is how far a part has come: its pool absent, provisioned,
// holding the dev's env, running the commit's pool setup, leased.
type partProgress int

const (
	partAbsent partProgress = iota
	partProvisioned
	partEnv
	partImage
	partLeased
)

// multiPartPools are the parts' pools by stage, with their service ids.
var multiPartPools = []struct{ stage, pool, id string }{
	{"webstage", "webpool", "svc-webpool"},
	{"apistage", "apipool", "svc-apipool"},
}

// multiPartFixture is appdev serving web and api, each part as far as given;
// the dev holds a plain value and a secret, as poolEnvFixture's does.
func multiPartFixture(t *testing.T, web, api partProgress, mutate func(*poolMigrateFixture)) poolMigrateFixture {
	t.Helper()
	f := poolEnvFixture()
	f.meta = poolMigrateMultiPartMeta()
	f.services = []platform.ServiceStack{
		poolMigrateRuntime("appdev", "svc-dev"),
		poolMigrateRuntime("webstage", "svc-webstage"),
		poolMigrateRuntime("apistage", "svc-apistage"),
	}
	delete(f.env, "svc-pool")
	delete(f.env, "svc-stage")
	f.ssh.image = poolMigrateImageReadFor(multiPartYAML(), multiPartDeclaration)
	progress := map[string]partProgress{"webpool": web, "apipool": api}
	for _, p := range multiPartPools {
		if progress[p.pool] < partProvisioned {
			continue
		}
		pool := poolMigratePool(p.pool, p.id)
		if progress[p.pool] >= partImage {
			pool.ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "av-" + p.pool, Created: "2026-10-24T09:00:00Z", Source: "CLI", Built: true}
		}
		f.services = append(f.services, pool)
		f.env[p.id] = poolMigratePoolOwnEnv()
		if progress[p.pool] >= partEnv {
			f.env[p.id] = append(f.env[p.id],
				platform.ServiceEnvVar{ID: "p-url", Key: "APP_URL", Content: "${zeropsSubdomain}", Type: platform.ServiceEnvUser},
				platform.ServiceEnvVar{ID: "p-secret", Key: "STRIPE_SECRET", Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true},
			)
		}
	}
	if mutate != nil {
		mutate(&f)
	}
	f.build(t)
	for _, p := range multiPartPools {
		if progress[p.pool] >= partEnv {
			if got := ops.SeedPoolDefaultRepo(f.stateDir, p.pool, f.meta.RemoteURL); got.State != ops.PoolDefaultRepoSeeded {
				t.Fatalf("seed %s: %+v", p.pool, got)
			}
		}
		if progress[p.pool] >= partLeased {
			writePoolLeaseRecord(t, f.stateDir, p.pool, p.id)
		}
	}
	return f
}

// writePoolLeaseRecord records zcp's lease <pool>-1 of pool on main of the
// dev's origin.
func writePoolLeaseRecord(t *testing.T, stateDir, pool, poolID string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := fmt.Sprintf(`{"poolHostname":%q,"poolServiceId":%q,"containerId":"c-%s","containerNumber":1,`+
		`"containerHostname":"node-id-1.runtime.%s.zerops","lease":"secret-lease-value","branch":"main",`+
		`"repo":"https://github.com/example/app.git","acquiredAt":"2026-10-24T09:30:00Z"}`, pool, poolID, pool, pool)
	if err := os.WriteFile(filepath.Join(dir, pool+"-1.json"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
}

// answerParts is the answer's parts list, each part as stage → step,
// with its pool and setup.
func answerParts(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	list, _ := out["parts"].([]any)
	parts := make([]map[string]any, 0, len(list))
	for _, item := range list {
		parts = append(parts, item.(map[string]any))
	}
	return parts
}

func poolMigrateNextArgs(out map[string]any) (string, map[string]any) {
	next, _ := out["next"].(map[string]any)
	args, _ := next["args"].(map[string]any)
	tool, _ := next["tool"].(string)
	return tool, args
}

// TestPoolMigrate_MultiPart_OnePoolPerPart_WalksTheFirstUnfinishedPart pins
// the read side of a dev serving several parts: every part gets a pool of its
// own, named after its stage; the answer lists each part with its step and
// walks the first part not yet ready, naming its pool for the calls that
// follow; and it writes nothing.
func TestPoolMigrate_MultiPart_OnePoolPerPart_WalksTheFirstUnfinishedPart(t *testing.T) {
	t.Parallel()
	t.Run("no pool yet: the first part's provision", func(t *testing.T) {
		t.Parallel()
		f := multiPartFixture(t, partAbsent, partAbsent, nil)
		before := snapshotTree(t, f.stateDir)
		out, mock := f.run(t)

		if out["status"] != "step" || out["step"] != "provision" || out["pool"] != "webpool" || out["part"] != "webstage" {
			t.Fatalf("status/step/pool/part = %v/%v/%v/%v; out %v", out["status"], out["step"], out["pool"], out["part"], out)
		}
		if content := poolMigrateNextContent(t, out); !strings.Contains(content, "hostname: webpool") {
			t.Fatalf("stanza = %s, want the web part's pool", content)
		}
		want := []map[string]any{
			{"stage": "webstage", "setup": "web", "pool": "webpool", "step": "provision"},
			{"stage": "apistage", "setup": "api", "pool": "apipool", "step": "provision"},
		}
		if got := answerParts(t, out); !reflect.DeepEqual(got, want) {
			t.Fatalf("parts = %v, want %v", got, want)
		}
		if inputs, _ := out["inputs"].(map[string]any); inputs["service"] != "appdev" || inputs["pool"] != "webpool" {
			t.Errorf("inputs = %v, want the dev and the walked part's pool", inputs)
		}
		if after := snapshotTree(t, f.stateDir); !reflect.DeepEqual(before, after) {
			t.Error("the read side changed the state dir")
		}
		if n := mock.CallCount("CreateServiceEnvVar") + mock.CallCount("DeleteUserData") + mock.CallCount("DeleteService"); n != 0 {
			t.Fatalf("the read side wrote (%d calls)", n)
		}
	})
	t.Run("the first part ready: the next part's step", func(t *testing.T) {
		t.Parallel()
		f := multiPartFixture(t, partLeased, partEnv, nil)
		out, _ := f.run(t)
		if out["step"] != "image" || out["pool"] != "apipool" || out["part"] != "apistage" {
			t.Fatalf("step/pool/part = %v/%v/%v, want the api part's image step", out["step"], out["pool"], out["part"])
		}
		if tool, args := poolMigrateNextArgs(out); tool != "zerops_deploy" || args["targetService"] != "apipool" || args["setup"] != "apipool" {
			t.Fatalf("next = %v %v, want the api pool's sha deploy", tool, args)
		}
		steps := map[string]string{}
		for _, p := range answerParts(t, out) {
			steps[p["stage"].(string)] = p["step"].(string)
		}
		if !reflect.DeepEqual(steps, map[string]string{"webstage": "ready-to-retire", "apistage": "image"}) {
			t.Errorf("part steps = %v", steps)
		}
		env := poolEnvBlock(t, out)
		if diff, _ := env["stageDiff"].([]any); len(diff) > 0 {
			for _, d := range diff {
				if stage := d.(map[string]any)["stage"]; stage != "apistage" {
					t.Errorf("the api part's env compares with %v, want only its own stage", stage)
				}
			}
		}
	})
}

// TestPoolMigrate_MultiPart_PartNamedByStageOrPool: a call names one part by
// its stage or by its pool; a pool that is no part's is refused, naming the
// parts' pools.
func TestPoolMigrate_MultiPart_PartNamedByStageOrPool(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		service string
		pool    string
	}{
		{"by its stage", "apistage", ""},
		{"by its pool", "appdev", "apipool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := multiPartFixture(t, partAbsent, partAbsent, func(f *poolMigrateFixture) {
				f.input.Service, f.input.Pool = tc.service, tc.pool
			})
			out, _ := f.run(t)
			if out["step"] != "provision" || out["part"] != "apistage" || out["service"] != "appdev" {
				t.Fatalf("step/part/service = %v/%v/%v, want the api part of appdev", out["step"], out["part"], out["service"])
			}
			if content := poolMigrateNextContent(t, out); !strings.Contains(content, "hostname: apipool") {
				t.Fatalf("stanza = %s, want the api part's pool", content)
			}
		})
	}
	t.Run("a pool that is no part's", func(t *testing.T) {
		t.Parallel()
		f := multiPartFixture(t, partAbsent, partAbsent, func(f *poolMigrateFixture) { f.input.Pool = "apppool" })
		out, _ := f.run(t)
		if out["status"] != "not-a-candidate" || !slices.Equal(poolMigrateReasons(out), []string{"pool-not-a-part"}) {
			t.Fatalf("out = %v, want pool-not-a-part", out)
		}
		msg := poolMigrateRefusalMessage(out, "pool-not-a-part")
		for _, want := range []string{"apppool", "webpool", "apipool"} {
			if !strings.Contains(msg, want) {
				t.Errorf("message %q does not name %q", msg, want)
			}
		}
		if cmds := f.ssh.recorded(); len(cmds) != 0 {
			t.Errorf("a refusal the inputs decide reached the dev: %v", cmds)
		}
	})
}

// TestPoolMigrate_MultiPart_PoolHostnameRefused: a part's pool named after
// its stage must be a hostname free for it — not another service's, and not
// another part's.
func TestPoolMigrate_MultiPart_PoolHostnameRefused(t *testing.T) {
	t.Parallel()
	t.Run("taken by a service that is no pool", func(t *testing.T) {
		t.Parallel()
		f := multiPartFixture(t, partAbsent, partAbsent, func(f *poolMigrateFixture) {
			f.services = append(f.services, poolMigrateRuntime("webpool", "svc-other"))
		})
		out, _ := f.run(t)
		if out["status"] != "not-a-candidate" || !slices.Contains(poolMigrateReasons(out), "pool-hostname-taken") {
			t.Fatalf("out = %v, want pool-hostname-taken", out)
		}
		if msg := poolMigrateRefusalMessage(out, "pool-hostname-taken"); !strings.Contains(msg, "webpool") || !strings.Contains(msg, "webstage") {
			t.Errorf("message %q, want the pool and its part named", msg)
		}
	})
	t.Run("two parts on one pool", func(t *testing.T) {
		t.Parallel()
		f := multiPartFixture(t, partAbsent, partAbsent, func(f *poolMigrateFixture) {
			f.meta.Stages = append(f.meta.Stages, topology.StageRef{Hostname: "web", SetupName: "webdocs"})
			f.services = append(f.services, poolMigrateRuntime("web", "svc-web"))
		})
		out, _ := f.run(t)
		if out["status"] != "not-a-candidate" || !slices.Contains(poolMigrateReasons(out), "pool-hostname-taken") {
			t.Fatalf("out = %v, want pool-hostname-taken", out)
		}
		if msg := poolMigrateRefusalMessage(out, "pool-hostname-taken"); !strings.Contains(msg, "webstage") || !strings.Contains(msg, `"web"`) {
			t.Errorf("message %q, want both parts named", msg)
		}
	})
}

// TestPoolMigrate_MultiPart_ReadyPartPointsAtTheNext: a part that is ready
// while another is not names no retire — the retire waits for every part —
// but the flow call that walks the next unfinished part.
func TestPoolMigrate_MultiPart_ReadyPartPointsAtTheNext(t *testing.T) {
	t.Parallel()
	f := multiPartFixture(t, partLeased, partEnv, func(f *poolMigrateFixture) { f.input.Pool = "webpool" })
	out, _ := f.run(t)
	if out["step"] != "ready-to-retire" || out["part"] != "webstage" {
		t.Fatalf("step/part = %v/%v, want the web part ready", out["step"], out["part"])
	}
	tool, args := poolMigrateNextArgs(out)
	if tool != "zerops_workflow" || args["action"] != nil || args["service"] != "appdev" || args["pool"] != "apipool" {
		t.Fatalf("next = %v %v, want the flow call for the api part", tool, args)
	}
	next, _ := out["next"].(map[string]any)
	if why, _ := next["why"].(string); !strings.Contains(why, "apistage") || !strings.Contains(why, "image") {
		t.Errorf("why = %q, want the api part and its step named", why)
	}
}

// TestPoolMigrate_MultiPart_EveryPartReady_NamesTheRetire: once every part's
// pool is leased on the work's branch, the whole dev is ready to retire, and
// the retire covers every part.
func TestPoolMigrate_MultiPart_EveryPartReady_NamesTheRetire(t *testing.T) {
	t.Parallel()
	f := multiPartFixture(t, partLeased, partLeased, nil)
	out, _ := f.run(t)
	if out["step"] != "ready-to-retire" {
		t.Fatalf("step = %v, want ready-to-retire", out["step"])
	}
	tool, args := poolMigrateNextArgs(out)
	if tool != "zerops_workflow" || args["action"] != "retire" || args["service"] != "appdev" || args["pool"] != nil {
		t.Fatalf("next = %v %v, want the retire of appdev naming no single pool", tool, args)
	}
	for _, p := range answerParts(t, out) {
		if p["step"] != "ready-to-retire" {
			t.Errorf("part %v, want ready-to-retire", p)
		}
	}
	if out["pool"] != nil || out["part"] != nil {
		t.Errorf("pool/part = %v/%v, want none: the answer is the whole dev's", out["pool"], out["part"])
	}
}

// TestPoolMigrate_MultiPart_ImageChecksTheDeclarationsServiceKeys: the image
// step of a part reads the declaration's service keys — a declaration whose
// entries name no pool holds the deploy.
func TestPoolMigrate_MultiPart_ImageChecksTheDeclarationsServiceKeys(t *testing.T) {
	t.Parallel()
	f := multiPartFixture(t, partEnv, partAbsent, func(f *poolMigrateFixture) {
		f.ssh.image = poolMigrateImageReadFor(multiPartYAML(), poolMigrateReadyDeclaration)
	})
	out, _ := f.run(t)
	if out["step"] != "image" || out["next"] != nil {
		t.Fatalf("step = %v next = %v, want the image step holding the deploy", out["step"], out["next"])
	}
	if checks := poolImageChecks(poolImageBlock(t, out), "missing"); !slices.Equal(checks, []string{"service"}) {
		t.Fatalf("missing = %v, want the service check", checks)
	}
}

// TestPoolMigrateEnv_MultiPart_WritesOnlyTheNamedPartsPool: the env step of a
// part gives its pool the dev's env and touches no other part's pool.
func TestPoolMigrateEnv_MultiPart_WritesOnlyTheNamedPartsPool(t *testing.T) {
	t.Parallel()
	f := multiPartFixture(t, partLeased, partProvisioned, func(f *poolMigrateFixture) {
		f.input.Pool = "apipool"
		f.input.Action = actionPoolEnv
	})
	webBefore, _ := f.mock.GetServiceEnv(context.Background(), "svc-webpool")
	out, mock := f.runEnv(t)
	if out["action"] != actionPoolEnv || out["part"] != "apistage" {
		t.Fatalf("action/part = %v/%v", out["action"], out["part"])
	}
	apiRows, _ := mock.GetServiceEnv(context.Background(), "svc-apipool")
	keys := map[string]bool{}
	for _, r := range apiRows {
		keys[r.Key] = true
	}
	if !keys["APP_URL"] || !keys["STRIPE_SECRET"] {
		t.Fatalf("apipool env = %v, want the dev's env copied", apiRows)
	}
	if webAfter, _ := mock.GetServiceEnv(context.Background(), "svc-webpool"); !reflect.DeepEqual(webBefore, webAfter) {
		t.Errorf("webpool's env changed: %v → %v", webBefore, webAfter)
	}
}

// TestPoolMigrateEnv_MultiPart_NoPartNamed_CopiesEveryPartsEnv: pool-env
// without pool gives every part's settled pool the dev's env in one call —
// Zerops Apps made six pool-env calls back to back, one per part and again
// (2026-10-27) — and the answer lists each part past its env step.
func TestPoolMigrateEnv_MultiPart_NoPartNamed_CopiesEveryPartsEnv(t *testing.T) {
	t.Parallel()
	f := multiPartFixture(t, partProvisioned, partProvisioned, func(f *poolMigrateFixture) {
		f.input.Action = actionPoolEnv
	})
	out, mock := f.runEnv(t)
	if out["action"] != actionPoolEnv {
		t.Fatalf("action = %v", out["action"])
	}
	for _, id := range []string{"svc-webpool", "svc-apipool"} {
		rows, _ := mock.GetServiceEnv(context.Background(), id)
		keys := map[string]bool{}
		for _, r := range rows {
			keys[r.Key] = true
		}
		if !keys["APP_URL"] || !keys["STRIPE_SECRET"] {
			t.Errorf("%s env = %v, want the dev's env copied in the one call", id, rows)
		}
	}
	for _, p := range answerParts(t, out) {
		if p["step"] == "env" {
			t.Errorf("part %v is still at its env step: %v", p["stage"], answerParts(t, out))
		}
	}
}

// newMultiPartRetireRig is appdev with web leased and api as far as given,
// under the retire's rig: its repository real, its mount up, each pool's
// orchestrator listing zcp's lease <pool>-1 held.
func newMultiPartRetireRig(t *testing.T, api partProgress) *retireRig {
	t.Helper()
	events := &retireEvents{}
	f := multiPartFixture(t, partLeased, api, nil)
	f.mock.WithProcess(&platform.Process{ID: "proc-delete-svc-dev", Status: platform.ProcessStatusFinished})
	rig := &retireRig{
		f:       f,
		client:  &retireClient{Mock: f.mock, events: events, devState: devLive, afterDelete: devGone},
		ssh:     &retireSSH{poolMigrateSSH: f.ssh, repo: retireRepo(t), events: events, leases: []string{"webpool-1", "apipool-1"}, down: map[string]bool{}},
		mounter: &retireMounter{events: events, mounted: map[string]bool{"appdev": true}},
		events:  events,
		work:    filepath.Join(t.TempDir(), "zcp-pool-migrate"),
	}
	rig.swarm = func(baseURL, _ string) ops.SwarmAPI {
		for _, p := range multiPartPools {
			if strings.Contains(baseURL, "."+p.pool+".") {
				return retireSwarm{containers: []platform.SwarmContainer{{ID: "c-" + p.pool, Number: 1, Hostname: "node-id-1.runtime." + p.pool + ".zerops", LockState: "reserved"}}}
			}
		}
		return retireSwarm{}
	}
	return rig
}

// multiPartLeaseEnv hands the retire each pool's bearer.
func (r *retireRig) multiPartDeps() poolRetireDeps {
	d := r.deps()
	d.leaseEnv = func(key string) (string, bool) {
		if strings.HasSuffix(key, "pool_API_TOKEN") {
			return "pool-api-token", true
		}
		return "", false
	}
	return d
}

func (r *retireRig) retireParts(t *testing.T, input WorkflowInput) (map[string]any, bool) {
	t.Helper()
	result, typed, err := poolMigrateRetireRun(context.Background(), r.multiPartDeps(), input)
	if err != nil || typed != nil {
		t.Fatalf("retire: %v, typed %T", err, typed)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	return out, result.IsError
}

// assertSplit pins the pairs after a multi-part retire: each stage's pair is
// keyed by its part's pool, marked as moved from appdev, naming both pools;
// no pair is keyed by appdev.
func (r *retireRig) assertSplit(t *testing.T) {
	t.Helper()
	for _, p := range multiPartPools {
		meta, _ := workflow.FindServiceMeta(r.f.stateDir, p.stage)
		if meta == nil || meta.Hostname != p.pool || !meta.DevIsPool() || meta.DevPool.ServiceID != p.id ||
			meta.DevPool.MigratedFrom == nil || meta.DevPool.MigratedFrom.Hostname != "appdev" ||
			!slices.Equal(meta.StageHostnames(), []string{p.stage}) || !slices.Equal(meta.DevPool.MigratedFrom.Parts, []string{"webpool", "apipool"}) {
			t.Fatalf("%s's pair = %+v, want it keyed by %s alone", p.stage, meta, p.pool)
		}
	}
	if m, _ := workflow.ReadServiceMeta(r.f.stateDir, "appdev"); m != nil {
		t.Fatalf("a pair is still keyed by appdev: %+v", m)
	}
}

// assertKeyedByDev pins that the pair is whole and keyed by appdev again.
func (r *retireRig) assertKeyedByDev(t *testing.T) {
	t.Helper()
	meta, _ := workflow.ReadServiceMeta(r.f.stateDir, "appdev")
	if meta == nil || meta.DevIsPool() || !slices.Equal(meta.StageHostnames(), []string{"webstage", "apistage"}) {
		t.Fatalf("the pair = %+v, want it keyed by appdev with both stages", meta)
	}
	for _, p := range multiPartPools {
		if m, _ := workflow.ReadServiceMeta(r.f.stateDir, p.pool); m != nil {
			t.Fatalf("a pair is keyed by %s: %+v", p.pool, m)
		}
	}
}

// TestPoolMigrateRetire_MultiPart_WaitsForEveryPart pins the retire's waiting
// rule: the dev is deleted only after every part's pool is provisioned,
// holds the dev's env, runs the commit's pool setup and is leased on the
// work's branch with the lease passing verify. One part short refuses,
// naming that part, and nothing is written.
func TestPoolMigrateRetire_MultiPart_WaitsForEveryPart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		api    partProgress
		rig    func(*retireRig)
		reason string
		want   []string
	}{
		{name: "the part's pool absent", api: partAbsent, reason: "not-ready", want: []string{"apistage", "provision"}},
		{name: "the part's pool without the dev's env", api: partProvisioned, reason: "not-ready", want: []string{"apistage", "env"}},
		{name: "the part's pool without its image", api: partEnv, reason: "not-ready", want: []string{"apistage", "image"}},
		{name: "the part's pool not leased", api: partImage, reason: "no-lease", want: []string{"apipool"}},
		{name: "the part's lease does not answer", api: partLeased, rig: func(r *retireRig) { r.ssh.down["apipool-1"] = true }, reason: "lease-unverified", want: []string{"apipool", "Connection refused"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newMultiPartRetireRig(t, tc.api)
			if tc.rig != nil {
				tc.rig(r)
			}
			out, isErr := r.retireParts(t, retireInput(true, true))
			if isErr || out["status"] != "retire-refused" {
				t.Fatalf("retire = %v, want retire-refused", out)
			}
			if !slices.Contains(poolMigrateReasons(out), tc.reason) {
				t.Fatalf("reasons = %q, want %q", poolMigrateReasons(out), tc.reason)
			}
			msg := poolMigrateRefusalMessage(out, tc.reason)
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Errorf("%s message %q does not name %q", tc.reason, msg, want)
				}
			}
			if rec, _ := readPoolRetireRecord(r.f.stateDir, "appdev"); rec != nil {
				t.Errorf("a retire record was written: %+v", rec)
			}
			if got := r.client.deletes(); len(got) > 0 {
				t.Errorf("deleted %q", got)
			}
			if r.events.index("unmount") >= 0 || r.events.index("ssh bundle") >= 0 {
				t.Errorf("the retire acted: %q", r.events.all())
			}
			r.assertKeyedByDev(t)
		})
	}
}

// TestPoolMigrateRetire_MultiPart_SplitsThePairOnePoolPerPart: with every
// part ready the retire diagnoses — naming every part's pool and lease —
// then, acknowledged, bundles the dev's repository once, splits the pair into
// one pool-keyed pair per part and deletes the dev by its id once.
func TestPoolMigrateRetire_MultiPart_SplitsThePairOnePoolPerPart(t *testing.T) {
	t.Parallel()
	r := newMultiPartRetireRig(t, partLeased)
	var held []string
	r.ssh.onBundle = func() {
		for _, host := range []string{"appdev", "webpool", "apipool"} {
			if release, err := workflow.LockPair(context.Background(), r.f.stateDir, host, 0); err == nil {
				release()
			} else {
				held = append(held, host)
			}
		}
	}

	out, isErr := r.retireParts(t, retireInput(false, false))
	if !isErr || out["code"] != platform.ErrDiagnosisRequired {
		t.Fatalf("first call = %v, want DIAGNOSIS_REQUIRED", out)
	}
	wd, _ := out["wouldDestroy"].(map[string]any)
	diag, _ := wd["diagnoses"].([]any)
	cause := diag[0].(map[string]any)["cause"].(string)
	for _, want := range []string{"webpool", "webpool-1", "apipool", "apipool-1"} {
		if !strings.Contains(cause, want) {
			t.Errorf("cause %q does not name %q", cause, want)
		}
	}
	r.assertKeyedByDev(t)

	out, isErr = r.retireParts(t, retireInput(true, false))
	if isErr || out["step"] != "retired" {
		t.Fatalf("confirmed = %v, want retired", out)
	}
	if got := r.client.deletes(); !slices.Equal(got, []string{"svc-dev"}) {
		t.Fatalf("deletes = %q, want svc-dev once", got)
	}
	if n := strings.Count(strings.Join(r.events.all(), "\n"), "ssh stream"); n != 1 {
		t.Errorf("bundles streamed = %d, want one: the dev's repository is one", n)
	}
	if !slices.Equal(held, []string{"appdev", "webpool", "apipool"}) {
		t.Errorf("pair locks held during the bundle = %q, want the dev's and every pool's", held)
	}
	r.assertSplit(t)
	rec := retireRecordOf(t, r.f.stateDir)
	wantParts := []poolRetirePart{
		{Stage: "webstage", Pool: "webpool", PoolServiceID: "svc-webpool", PoolSetup: "webpool"},
		{Stage: "apistage", Pool: "apipool", PoolServiceID: "svc-apipool", PoolSetup: "apipool"},
	}
	if rec.Phase != poolRetireRetired || !reflect.DeepEqual(rec.Parts, wantParts) {
		t.Fatalf("record = %+v, want retired with both parts", rec)
	}
	if why, _ := out["nextUnavailable"].(string); !strings.Contains(why, "webpool") || !strings.Contains(why, "apipool") {
		t.Errorf("nextUnavailable = %q, want every part's pool named", why)
	}
	// The read side answers from the record, named by any part's stage.
	for _, stage := range []string{"webstage", "apistage"} {
		result, _, err := poolMigrateRun(context.Background(), r.multiPartDeps().poolMigrateDeps, WorkflowInput{Workflow: "pool-migrate", Service: stage})
		if err != nil {
			t.Fatal(err)
		}
		var read map[string]any
		if err := json.Unmarshal([]byte(getTextContent(t, result)), &read); err != nil {
			t.Fatal(err)
		}
		if read["step"] != "retired" {
			t.Errorf("%s: read side = %v, want retired", stage, read["step"])
		}
	}
}

// seedMultiPartRecord writes the record a multi-part retire left in phase,
// with a real bundle; from rewritten on, the pair is split as the retire
// leaves it.
func (r *retireRig) seedMultiPartRecord(t *testing.T, phase string) {
	t.Helper()
	b, err := ops.BuildPoolMigrateBundle(context.Background(), r.ssh, ops.PoolMigrateBundleInput{
		Dev: "appdev", Root: topology.CodeMountRoot, WorkDir: r.work, Path: poolRetireBundlePath(r.f.stateDir, "appdev"),
	})
	if err != nil {
		t.Fatalf("seed bundle: %v", err)
	}
	rec := &poolRetireRecord{
		Dev: "appdev", DevServiceID: "svc-dev", Stages: []string{"webstage", "apistage"},
		Parts: []poolRetirePart{
			{Stage: "webstage", Pool: "webpool", PoolServiceID: "svc-webpool", PoolSetup: "webpool"},
			{Stage: "apistage", Pool: "apipool", PoolServiceID: "svc-apipool", PoolSetup: "apipool"},
		},
		Phase: phase, StartedAt: "2026-10-24T10:00:00Z", Bundle: b,
	}
	if phase == poolRetireRewritten || phase == poolRetireDeleting {
		if _, err := workflow.MigratePairToPools(r.f.stateDir, "appdev", rec.partMoves()); err != nil {
			t.Fatal(err)
		}
	}
	if err := savePoolRetireRecord(r.f.stateDir, rec); err != nil {
		t.Fatal(err)
	}
	r.events.log = nil
}

// TestPoolMigrateRetire_MultiPart_ResumeTable walks every phase a multi-part
// retire can stop in against every state the dev reads by id: live resumes
// — the guards of every part read again before the split — DELETING keeps
// the record, gone finishes; and whatever the phase, a finished retire leaves
// every stage under its own part's pool, never one under the dead dev's key.
func TestPoolMigrateRetire_MultiPart_ResumeTable(t *testing.T) {
	t.Parallel()
	phases := []string{poolRetireStarted, poolRetireFenced, poolRetireBundled, poolRetireRewritten, poolRetireDeleting}
	for _, phase := range phases {
		for _, state := range []string{devLive, devDeleting, devGone} {
			t.Run(phase+"/"+state, func(t *testing.T) {
				t.Parallel()
				r := newMultiPartRetireRig(t, partLeased)
				r.seedMultiPartRecord(t, phase)
				r.client.devState = state

				out, isErr := r.retireParts(t, retireInput(true, false))
				rec := retireRecordOf(t, r.f.stateDir)
				switch state {
				case devDeleting:
					if isErr || out["step"] != "retiring" || rec.Phase != phase {
						t.Fatalf("out %v, record phase %s; want retiring, the record kept at %s", out, rec.Phase, phase)
					}
					if len(r.client.deletes()) > 0 || r.events.index("ssh") >= 0 || r.events.index("unmount") >= 0 {
						t.Fatalf("a DELETING dev was acted on: %q", r.events.all())
					}
					return
				case devGone:
					if len(r.client.deletes()) > 0 {
						t.Fatalf("a gone dev was deleted again: %q", r.client.deletes())
					}
				case devLive:
					if got := r.client.deletes(); !slices.Equal(got, []string{"svc-dev"}) {
						t.Fatalf("deletes = %q, want the recorded id", got)
					}
					if r.events.index("ssh fingerprint") < 0 {
						t.Errorf("no fingerprint re-check before the delete: %q", r.events.all())
					}
					if n := countEvents(r.events.all(), "ssh lease: true"); phase != poolRetireRewritten && phase != poolRetireDeleting && n != 2 {
						t.Errorf("lease probes on resume = %d, want one per part: %q", n, r.events.all())
					}
				}
				if isErr || out["step"] != "retired" || rec.Phase != poolRetireRetired {
					t.Fatalf("out %v, record %+v; want retired", out, rec)
				}
				r.assertSplit(t)
			})
		}
	}
}

// TestPoolMigrateRetire_MultiPart_ResumeWaitsForEveryPart: a live dev's
// retire resumed from before the split reads every part's guards again — a
// part no longer ready aborts it, the pair whole and keyed by the dev.
func TestPoolMigrateRetire_MultiPart_ResumeWaitsForEveryPart(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{poolRetireStarted, poolRetireFenced, poolRetireBundled} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			r := newMultiPartRetireRig(t, partLeased)
			r.seedMultiPartRecord(t, phase)
			if err := os.Remove(filepath.Join(r.f.stateDir, "leases", "apipool-1.json")); err != nil {
				t.Fatal(err)
			}
			out, isErr := r.retireParts(t, retireInput(true, false))
			if isErr || out["status"] != "aborted" || !slices.Contains(poolMigrateReasons(out), "no-lease") {
				t.Fatalf("resume = %v, want aborted for the api part's lease", out)
			}
			if len(r.client.deletes()) > 0 {
				t.Fatalf("deleted %q", r.client.deletes())
			}
			if rec := retireRecordOf(t, r.f.stateDir); rec.Phase != poolRetireAborted {
				t.Fatalf("record = %+v", rec)
			}
			r.assertKeyedByDev(t)
		})
	}
}

// TestPoolMigrateRetire_MultiPart_CutSplitFinishes: a retire cut inside its
// split — one part's file written, the dev's still there — finishes the split
// on the next call and deletes the dev once.
func TestPoolMigrateRetire_MultiPart_CutSplitFinishes(t *testing.T) {
	t.Parallel()
	r := newMultiPartRetireRig(t, partLeased)
	r.seedMultiPartRecord(t, poolRetireBundled)
	rec := retireRecordOf(t, r.f.stateDir)
	rec.Phase = poolRetireRewritten
	if err := savePoolRetireRecord(r.f.stateDir, rec); err != nil {
		t.Fatal(err)
	}
	// The web part's file as a cut split leaves it: written, the dev's file
	// not removed yet.
	scratch := t.TempDir()
	if err := workflow.WriteServiceMeta(scratch, poolMigrateMultiPartMeta()); err != nil {
		t.Fatal(err)
	}
	split, err := workflow.MigratePairToPools(scratch, "appdev", rec.partMoves())
	if err != nil {
		t.Fatal(err)
	}
	if err := workflow.WriteServiceMeta(r.f.stateDir, split[0]); err != nil {
		t.Fatal(err)
	}

	out, isErr := r.retireParts(t, retireInput(true, false))
	if isErr || out["step"] != "retired" {
		t.Fatalf("resume = %v, want retired", out)
	}
	if got := r.client.deletes(); !slices.Equal(got, []string{"svc-dev"}) {
		t.Fatalf("deletes = %q", got)
	}
	r.assertSplit(t)
}

// countEvents counts the events equal to event.
func countEvents(events []string, event string) int {
	n := 0
	for _, e := range events {
		if e == event {
			n++
		}
	}
	return n
}

// TestPoolMigrateRetire_MultiPart_AbortAfterTheSplit_MergesBack: a retire
// that aborts after the split — the repository moved since the bundle —
// merges the parts' pairs back into the one keyed by the dev.
func TestPoolMigrateRetire_MultiPart_AbortAfterTheSplit_MergesBack(t *testing.T) {
	t.Parallel()
	r := newMultiPartRetireRig(t, partLeased)
	r.ssh.afterStream = func() {
		if err := os.WriteFile(filepath.Join(r.ssh.repo, "app.txt"), []byte("v1\nwritten after the bundle\n"), 0o644); err != nil {
			t.Error(err)
		}
	}
	out, isErr := r.retireParts(t, retireInput(true, false))
	if isErr || out["status"] != "aborted" || !slices.Equal(poolMigrateReasons(out), []string{"fingerprint-changed"}) {
		t.Fatalf("retire = %v, want aborted for the moved repository", out)
	}
	if len(r.client.deletes()) > 0 {
		t.Fatalf("deleted %q", r.client.deletes())
	}
	r.assertKeyedByDev(t)
	if !r.mounter.state("appdev") {
		t.Error("the dev's mount was not put back")
	}
}
