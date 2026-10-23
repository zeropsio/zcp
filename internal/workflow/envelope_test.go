package workflow

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestStateEnvelope_JSONRoundtrip confirms that every envelope fixture
// round-trips losslessly through JSON. This is the compaction-safety
// invariant: the same envelope must serialize identically on every call.
func TestStateEnvelope_JSONRoundtrip(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	cases := []StateEnvelope{
		// idle / empty
		{Phase: PhaseIdle, Environment: EnvLocal, Generated: fixed},
		// idle / container
		{Phase: PhaseIdle, Environment: EnvContainer, SelfService: &SelfService{Hostname: "zcp"}, Generated: fixed},
		// bootstrap-active with recipe
		{
			Phase:       PhaseBootstrapActive,
			Environment: EnvLocal,
			Project:     ProjectSummary{ID: "p1", Name: "weather"},
			Generated:   fixed,
		},
		// bootstrap-active with recipe route summary
		{
			Phase:       PhaseBootstrapActive,
			Environment: EnvContainer,
			Project:     ProjectSummary{ID: "p1", Name: "weather"},
			Bootstrap: &BootstrapSessionSummary{
				Route:       BootstrapRouteRecipe,
				Intent:      "laravel dashboard",
				RecipeMatch: &RecipeMatch{Slug: "laravel-dashboard", Confidence: 0.91},
			},
			Generated: fixed,
		},
		// bootstrap-active classic route (no recipe match)
		{
			Phase:       PhaseBootstrapActive,
			Environment: EnvLocal,
			Project:     ProjectSummary{ID: "p1", Name: "api"},
			Bootstrap:   &BootstrapSessionSummary{Route: BootstrapRouteClassic, Intent: "node api + postgres"},
			Generated:   fixed,
		},
		// bootstrap-active adopt route
		{
			Phase:       PhaseBootstrapActive,
			Environment: EnvLocal,
			Project:     ProjectSummary{ID: "p1", Name: "legacy"},
			Bootstrap:   &BootstrapSessionSummary{Route: BootstrapRouteAdopt, Closed: true},
			Generated:   fixed,
		},
		// develop-active, dev mode, push-dev, dynamic
		{
			Phase:       PhaseDevelopActive,
			Environment: EnvLocal,
			Project:     ProjectSummary{ID: "p1", Name: "weather"},
			Services: []ServiceSnapshot{{
				Hostname:        "appdev",
				TypeVersion:     "nodejs@20",
				RuntimeClass:    topology.RuntimeDynamic,
				Status:          "ACTIVE",
				Bootstrapped:    true,
				Mode:            topology.ModeDev,
				CloseDeployMode: topology.CloseModeAuto,
				StageHostname:   "appstage",
			}},
			WorkSession: &WorkSessionSummary{
				Intent:    "add login",
				Services:  []string{"appdev"},
				CreatedAt: fixed,
				Deploys: map[string][]AttemptInfo{
					"appdev": {{At: fixed, Success: true, Iteration: 1}},
				},
			},
			Generated: fixed,
		},
	}

	for i, env := range cases {
		data, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("case %d marshal: %v", i, err)
		}
		var got StateEnvelope
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("case %d unmarshal: %v", i, err)
		}
		data2, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("case %d remarshal: %v", i, err)
		}
		if string(data) != string(data2) {
			t.Errorf("case %d roundtrip differs:\nfirst:  %s\nsecond: %s", i, data, data2)
		}
	}
}

// TestNextAction_IsZero verifies the sentinel used by Plan validators.
func TestNextAction_IsZero(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a    NextAction
		zero bool
	}{
		{"empty", NextAction{}, true},
		{"label_only", NextAction{Label: "x"}, false},
		{"tool_only", NextAction{Tool: "zerops_workflow"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.a.IsZero(); got != c.zero {
				t.Errorf("IsZero()=%v, want=%v", got, c.zero)
			}
		})
	}
}

// leasesTestEnvelope is a minimal envelope whose Leases the caller sets.
func leasesTestEnvelope(leases *[]LeaseSnapshot) StateEnvelope {
	return StateEnvelope{
		Phase: PhaseIdle, Environment: EnvContainer,
		Generated: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
		Leases:    leases,
	}
}

// TestEnvelopeJSON_LeasesOnWire_CamelCase pins the leases[] wire shape Mate is
// built against (docs/spec-mate.md §2.8a.3): a top-level key, camelCase fields.
// It replaces TestEnvelopeJSON_LeasesOffWire — the assertion flips by design.
func TestEnvelopeJSON_LeasesOnWire_CamelCase(t *testing.T) {
	t.Parallel()

	env := leasesTestEnvelope(&[]LeaseSnapshot{{
		Alias: "webpool-3", Pool: "webpool", Branch: "feat-x",
		ContainerHostname: "node-id-3.runtime.webpool.zerops", State: "held",
	}})
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := doc["leases"]
	if !ok {
		t.Fatalf("no top-level leases key: %s", raw)
	}
	const want = `[{"alias":"webpool-3","pool":"webpool","branch":"feat-x",` +
		`"containerHostname":"node-id-3.runtime.webpool.zerops","state":"held","unverified":false}]`
	if string(got) != want {
		t.Errorf("leases =\n%s\nwant\n%s", got, want)
	}
	if _, nested := doc["services"]; !nested {
		t.Errorf("leases rides beside services at the top level: %s", raw)
	}
}

// TestEnvelopeJSON_LeaseAcquiredAt pins leases[].acquiredAt (docs/spec-mate.md
// §2.8a.3): when the lease was acquired, the key a client rebinds by when a
// bound lease of a branch ends and another stays held — after unverified,
// before devServers, and absent when unknown (additive).
func TestEnvelopeJSON_LeaseAcquiredAt(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(leasesTestEnvelope(&[]LeaseSnapshot{
		{Alias: "hqpool-3", Pool: "hqpool", Branch: "feat-x", ContainerHostname: "node-id-3.runtime.hqpool.zerops", State: "held", AcquiredAt: "2026-10-23T12:00:01Z"},
		{Alias: "webpool-7", Pool: "webpool", Branch: "feat-x", ContainerHostname: "node-id-7.runtime.webpool.zerops", State: "unknown-holder"},
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	const want = `[{"alias":"hqpool-3","pool":"hqpool","branch":"feat-x","containerHostname":"node-id-3.runtime.hqpool.zerops",` +
		`"state":"held","unverified":false,"acquiredAt":"2026-10-23T12:00:01Z"},` +
		`{"alias":"webpool-7","pool":"webpool","branch":"feat-x","containerHostname":"node-id-7.runtime.webpool.zerops",` +
		`"state":"unknown-holder","unverified":false}]`
	if got := string(doc["leases"]); got != want {
		t.Errorf("leases =\n%s\nwant\n%s", got, want)
	}
}

// TestEnvelopeJSON_LeasesNotRead_NoKey pins that an envelope whose leases were
// not read carries no leases key — never null, never [].
func TestEnvelopeJSON_LeasesNotRead_NoKey(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(leasesTestEnvelope(nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "leases") {
		t.Errorf("not-read envelope carries a leases key: %s", raw)
	}
}

// TestEnvelopeJSON_LeasesReadNone_EmptyArray pins that leases read and none
// held is `"leases":[]`, distinct from absent.
func TestEnvelopeJSON_LeasesReadNone_EmptyArray(t *testing.T) {
	t.Parallel()

	for name, leases := range map[string]*[]LeaseSnapshot{
		"empty slice": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(leasesTestEnvelope(leases))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(raw), `"leases":[]`) {
				t.Errorf("read-none envelope lacks \"leases\":[]: %s", raw)
			}
		})
	}
}

// TestEnvelopeJSON_NoLeaseString pins DP-3 on the wire: a snapshot has no field
// that could carry the lease string, so no JSON key but the six named ones.
func TestEnvelopeJSON_NoLeaseString(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(LeaseSnapshot{Alias: "webpool-3", Pool: "webpool", State: "held"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"alias", "branch", "containerHostname", "pool", "state", "unverified"}
	got := make([]string, 0, len(keys))
	for k := range keys {
		got = append(got, k)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("lease keys = %v, want %v (a lease string has no field)", got, want)
	}
}

// TestWithParts_LeaseDevServers: a call that read a lease's supervisor
// (zerops_dev_server on the alias) carries its servers on that lease —
// leases[].devServers, keyed by alias — the way a dev service carries its own;
// a lease the call did not read carries none (no key).
func TestWithParts_LeaseDevServers(t *testing.T) {
	t.Parallel()

	env := leasesTestEnvelope(&[]LeaseSnapshot{
		{Alias: "webpool-3", Pool: "webpool", State: "held"},
		{Alias: "webpool-4", Pool: "webpool", State: "held"},
	})
	read := []topology.DevServerState{{Name: "web", Port: 3000, URL: "https://webpool-29f-8003.ny1.zerops.app", Status: "running"}}
	out := env.WithParts(map[string][]topology.DevServerState{"webpool-3": read})

	leases := out.LeaseList()
	if len(leases) != 2 || !slices.Equal(leases[0].DevServers, read) {
		t.Fatalf("leases = %+v, want webpool-3 carrying the servers read on it", leases)
	}
	if leases[1].DevServers != nil {
		t.Errorf("webpool-4 was not read but carries %+v", leases[1].DevServers)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"alias":"webpool-3"`) || !strings.Contains(string(raw), `"devServers":[{"name":"web"`) {
		t.Errorf("wire lacks leases[].devServers: %s", raw)
	}
	if env.LeaseList()[0].DevServers != nil {
		t.Error("WithParts changed the envelope it was called on")
	}
}
