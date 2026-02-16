// Tests for: Store GetBriefing composition/layer tests
package knowledge

import (
	"strings"
	"testing"
)

func TestStore_GetBriefing_RuntimeOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		runtime string
		want    []string // substrings that must be present
	}{
		{
			name:    "PHP runtime",
			runtime: "php-nginx@8.4",
			want:    []string{"Zerops Platform Model", "Zerops Rules", "Zerops Grammar", "PHP", "Build php@X", "Port 80"},
		},
		{
			name:    "Node.js runtime",
			runtime: "nodejs@22",
			want:    []string{"Zerops Platform Model", "Zerops Rules", "Zerops Grammar", "Node.js", "node_modules"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := testStoreWithCore(t)

			briefing, err := store.GetBriefing(tt.runtime, nil, nil)
			if err != nil {
				t.Fatal(err)
			}

			for _, substr := range tt.want {
				if !strings.Contains(briefing, substr) {
					t.Errorf("briefing missing %q", substr)
				}
			}
		})
	}
}

func TestStore_GetBriefing_ServicesOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		services []string
		want     []string
	}{
		{
			name:     "PostgreSQL only",
			services: []string{"postgresql@16"},
			want:     []string{"Zerops Grammar", "PostgreSQL", "Port 5432", "${hostname_var}", "DATABASE_URL"},
		},
		{
			name:     "Multiple services",
			services: []string{"postgresql@16", "valkey@7.2"},
			want:     []string{"Zerops Grammar", "PostgreSQL", "Valkey", "Port 6379", "REDIS_URL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := testStoreWithCore(t)

			briefing, err := store.GetBriefing("", tt.services, nil)
			if err != nil {
				t.Fatal(err)
			}

			for _, substr := range tt.want {
				if !strings.Contains(briefing, substr) {
					t.Errorf("briefing missing %q", substr)
				}
			}
		})
	}
}

func TestStore_GetBriefing_RuntimeAndServices(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("nodejs@22", []string{"postgresql@16", "valkey@7.2"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain all layers
	required := []string{
		"Zerops Platform Model",
		"Zerops Rules",
		"Zerops Grammar",
		"Node.js",
		"node_modules",
		"PostgreSQL",
		"Port 5432",
		"Valkey",
		"Port 6379",
		"${hostname_var}",
		"DATABASE_URL",
		"REDIS_URL",
		"Choose Database",
		"Choose Cache",
	}

	for _, substr := range required {
		if !strings.Contains(briefing, substr) {
			t.Errorf("briefing missing %q", substr)
		}
	}
}

func TestStore_GetBriefing_EmptyInputs(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain all always-on layers
	if !strings.Contains(briefing, "Zerops Platform Model") {
		t.Error("empty briefing should contain platform model")
	}
	if !strings.Contains(briefing, "Zerops Rules") {
		t.Error("empty briefing should contain rules")
	}
	if !strings.Contains(briefing, "Zerops Grammar") {
		t.Error("empty briefing should contain grammar")
	}
}

func TestStore_GetBriefing_UnknownRuntime(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("unknown@1.0", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain grammar, no exception section (graceful)
	if !strings.Contains(briefing, "Zerops Grammar") {
		t.Error("briefing should contain grammar")
	}
	// Should NOT contain PHP/Node.js specific content
	if strings.Contains(briefing, "Build php@X") {
		t.Error("briefing should not contain PHP exceptions for unknown runtime")
	}
}

func TestStore_GetBriefing_UnknownService(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("", []string{"unknown-service@1"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain grammar + wiring syntax (services were requested)
	if !strings.Contains(briefing, "Zerops Grammar") {
		t.Error("briefing should contain grammar")
	}
	if !strings.Contains(briefing, "${hostname_var}") {
		t.Error("briefing should contain wiring syntax when services provided")
	}
}

// --- GetBriefing Layered Composition Tests ---

func TestStore_GetBriefing_LayerOrder(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("php-nginx@8.4", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	platformIdx := strings.Index(briefing, "Zerops Platform Model")
	rulesIdx := strings.Index(briefing, "Zerops Rules")
	grammarIdx := strings.Index(briefing, "Zerops Grammar")
	runtimeIdx := strings.Index(briefing, "Runtime-Specific:")
	serviceIdx := strings.Index(briefing, "Service Cards")

	if platformIdx < 0 {
		t.Fatal("briefing missing platform model")
	}
	if rulesIdx < 0 {
		t.Fatal("briefing missing rules")
	}
	if grammarIdx < 0 {
		t.Fatal("briefing missing grammar")
	}

	// L0 platform model → L1 rules → L2 grammar → L3 runtime → L4 services
	if platformIdx >= rulesIdx {
		t.Errorf("platform model (pos %d) should come before rules (pos %d)", platformIdx, rulesIdx)
	}
	if rulesIdx >= grammarIdx {
		t.Errorf("rules (pos %d) should come before grammar (pos %d)", rulesIdx, grammarIdx)
	}
	if runtimeIdx >= 0 && grammarIdx >= runtimeIdx {
		t.Errorf("grammar (pos %d) should come before runtime (pos %d)", grammarIdx, runtimeIdx)
	}
	if serviceIdx >= 0 && runtimeIdx >= 0 && runtimeIdx >= serviceIdx {
		t.Errorf("runtime (pos %d) should come before services (pos %d)", runtimeIdx, serviceIdx)
	}
}

func TestStore_GetBriefing_WiringIncluded(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(briefing, "Wiring Patterns") {
		t.Error("briefing with services should include wiring patterns")
	}
	if !strings.Contains(briefing, "Wiring: PostgreSQL") {
		t.Error("briefing should include per-service wiring template")
	}
}

func TestStore_GetBriefing_NoWiringWithoutServices(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("nodejs@22", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(briefing, "Wiring Patterns") {
		t.Error("briefing without services should NOT include wiring patterns")
	}
}

func TestStore_GetBriefing_DecisionsIncluded(t *testing.T) {
	t.Parallel()
	store := testStoreWithCore(t)

	briefing, err := store.GetBriefing("", []string{"postgresql@16", "valkey@7.2"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(briefing, "Decision Hints") {
		t.Error("briefing with managed services should include decision hints")
	}
	if !strings.Contains(briefing, "Choose Database") {
		t.Error("briefing with postgresql should include database decision")
	}
	if !strings.Contains(briefing, "Choose Cache") {
		t.Error("briefing with valkey should include cache decision")
	}
}
