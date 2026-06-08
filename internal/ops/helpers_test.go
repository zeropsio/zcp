// Tests for: plans/analysis/ops.md $ helpers
package ops

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestResolveServiceID(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1", Status: "RUNNING"},
	}

	tests := []struct {
		name      string
		services  []platform.ServiceStack
		projectID string
		hostname  string
		wantID    string
		wantErr   string
	}{
		{
			name:      "Found",
			services:  services,
			projectID: "proj-1",
			hostname:  "api",
			wantID:    "svc-1",
		},
		{
			name:      "NotFound",
			services:  services,
			projectID: "proj-1",
			hostname:  "missing",
			wantErr:   platform.ErrServiceNotFound,
		},
		{
			name:      "EmptyList",
			services:  []platform.ServiceStack{},
			projectID: "proj-1",
			hostname:  "api",
			wantErr:   platform.ErrServiceNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, err := FindService(tt.services, tt.hostname)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				pe, ok := err.(*platform.PlatformError)
				if !ok {
					t.Fatalf("expected *PlatformError, got %T: %v", err, err)
				}
				if pe.Code != tt.wantErr {
					t.Fatalf("expected code %s, got %s", tt.wantErr, pe.Code)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if svc.ID != tt.wantID {
				t.Fatalf("expected ID %s, got %s", tt.wantID, svc.ID)
			}
		})
	}
}

func TestParseSince(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantDelta time.Duration // expected offset from now (negative)
		wantExact time.Time     // for ISO8601 case
		wantErr   bool
	}{
		{
			name:      "Seconds",
			input:     "30s",
			wantDelta: -30 * time.Second,
		},
		{
			name:      "Seconds_Ninety",
			input:     "90s",
			wantDelta: -90 * time.Second,
		},
		{
			name:      "Minutes",
			input:     "30m",
			wantDelta: -30 * time.Minute,
		},
		{
			name:      "Hours",
			input:     "1h",
			wantDelta: -1 * time.Hour,
		},
		{
			name:      "Days",
			input:     "7d",
			wantDelta: -7 * 24 * time.Hour,
		},
		{
			name:      "ISO8601",
			input:     "2024-01-01T00:00:00Z",
			wantExact: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "Empty",
			input:     "",
			wantDelta: -1 * time.Hour,
		},
		{
			name:    "Invalid",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "OutOfRange_Hours",
			input:   "200h",
			wantErr: true,
		},
		{
			name:    "OutOfRange_Minutes",
			input:   "1500m",
			wantErr: true,
		},
		{
			name:    "OutOfRange_Days",
			input:   "31d",
			wantErr: true,
		},
		{
			name:    "Zero_Minutes",
			input:   "0m",
			wantErr: true,
		},
		{
			name:    "Zero_Seconds",
			input:   "0s",
			wantErr: true,
		},
		{
			name:    "OutOfRange_Seconds",
			input:   "100000s",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSince(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tt.wantExact.IsZero() {
				if !got.Equal(tt.wantExact) {
					t.Fatalf("expected %v, got %v", tt.wantExact, got)
				}
				return
			}

			// For duration-based tests, check that result is within tolerance
			expected := time.Now().Add(tt.wantDelta)
			diff := math.Abs(float64(got.Sub(expected)))
			if diff > float64(2*time.Second) {
				t.Fatalf("result %v too far from expected %v (diff: %v)", got, expected, time.Duration(diff))
			}
		})
	}
}

func TestParseEnvPairs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		wantKeys []string
		wantVals []string
		wantErr  string
	}{
		{
			name:     "Valid",
			input:    []string{"KEY=value", "K2=v=2"},
			wantKeys: []string{"KEY", "K2"},
			wantVals: []string{"value", "v=2"},
		},
		{
			name:     "EmptyValue",
			input:    []string{"KEY="},
			wantKeys: []string{"KEY"},
			wantVals: []string{""},
		},
		{
			name:    "NoEquals",
			input:   []string{"NOVALUE"},
			wantErr: platform.ErrInvalidEnvFormat,
		},
		{
			name:    "EmptyKey",
			input:   []string{"=value"},
			wantErr: platform.ErrInvalidEnvFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pairs, err := parseEnvPairs(tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				pe, ok := err.(*platform.PlatformError)
				if !ok {
					t.Fatalf("expected *PlatformError, got %T: %v", err, err)
				}
				if pe.Code != tt.wantErr {
					t.Fatalf("expected code %s, got %s", tt.wantErr, pe.Code)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(pairs) != len(tt.wantKeys) {
				t.Fatalf("expected %d pairs, got %d", len(tt.wantKeys), len(pairs))
			}

			for i, p := range pairs {
				if p.Key != tt.wantKeys[i] {
					t.Errorf("pair[%d].Key = %q, want %q", i, p.Key, tt.wantKeys[i])
				}
				if p.Value != tt.wantVals[i] {
					t.Errorf("pair[%d].Value = %q, want %q", i, p.Value, tt.wantVals[i])
				}
			}
		})
	}
}

func TestFindServiceByHostname(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api"},
		{ID: "svc-2", Name: "db"},
	}

	tests := []struct {
		name     string
		hostname string
		wantID   string
		wantNil  bool
	}{
		{name: "Found", hostname: "api", wantID: "svc-1"},
		{name: "NotFound", hostname: "missing", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := findServiceByHostname(services, tt.hostname)

			if tt.wantNil {
				if svc != nil {
					t.Fatalf("expected nil, got %+v", svc)
				}
				return
			}

			if svc == nil {
				t.Fatal("expected non-nil service")
			}
			if svc.ID != tt.wantID {
				t.Fatalf("expected ID %s, got %s", tt.wantID, svc.ID)
			}
		})
	}
}

func TestListHostnames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		services []platform.ServiceStack
		want     string
	}{
		{
			name:     "Multiple",
			services: []platform.ServiceStack{{Name: "api"}, {Name: "db"}},
			want:     "api, db",
		},
		{
			name:     "Single",
			services: []platform.ServiceStack{{Name: "api"}},
			want:     "api",
		},
		{
			name:     "Empty",
			services: []platform.ServiceStack{},
			want:     "(none)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ListHostnames(tt.services)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestEnvVarsToMaps_PlatformInjected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		envs                []platform.ServiceEnvVar
		wantKeys            []string
		wantPlatformKeys    []string // keys that should have isPlatformInjected: true
		wantNonPlatformKeys []string // keys that should NOT have isPlatformInjected
	}{
		{
			name: "zeropsSubdomain annotated as platform-injected",
			envs: []platform.ServiceEnvVar{
				{ID: "e1", Key: "PORT", Content: "3000"},
				{ID: "e2", Key: "zeropsSubdomain", Content: "https://app-1df2-3000.prg1.zerops.app"},
				{ID: "e3", Key: "HOST", Content: "0.0.0.0"},
			},
			wantKeys:            []string{"PORT", "zeropsSubdomain", "HOST"},
			wantPlatformKeys:    []string{"zeropsSubdomain"},
			wantNonPlatformKeys: []string{"PORT", "HOST"},
		},
		{
			name: "no platform-injected keys present",
			envs: []platform.ServiceEnvVar{
				{ID: "e1", Key: "PORT", Content: "3000"},
				{ID: "e2", Key: "HOST", Content: "0.0.0.0"},
			},
			wantKeys:            []string{"PORT", "HOST"},
			wantNonPlatformKeys: []string{"PORT", "HOST"},
		},
		{
			name:     "empty envs",
			envs:     []platform.ServiceEnvVar{},
			wantKeys: []string{},
		},
		{
			name: "only zeropsSubdomain",
			envs: []platform.ServiceEnvVar{
				{ID: "e1", Key: "zeropsSubdomain", Content: "https://app-1df2.prg1.zerops.app"},
			},
			wantKeys:         []string{"zeropsSubdomain"},
			wantPlatformKeys: []string{"zeropsSubdomain"},
		},
		{
			name: "platform-injected with isReference interaction",
			envs: []platform.ServiceEnvVar{
				{ID: "e1", Key: "DB_URL", Content: "${db_connectionString}"},
				{ID: "e2", Key: "zeropsSubdomain", Content: "https://app-1df2.prg1.zerops.app"},
			},
			wantKeys:            []string{"DB_URL", "zeropsSubdomain"},
			wantPlatformKeys:    []string{"zeropsSubdomain"},
			wantNonPlatformKeys: []string{"DB_URL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := envVarsToMaps(tt.envs, true)
			if len(result) != len(tt.wantKeys) {
				t.Fatalf("expected %d envs, got %d", len(tt.wantKeys), len(result))
			}
			for i, wantKey := range tt.wantKeys {
				if result[i]["key"] != wantKey {
					t.Errorf("env[%d] key = %v, want %s", i, result[i]["key"], wantKey)
				}
			}

			// Build lookup by key for annotation checks
			byKey := make(map[string]map[string]any, len(result))
			for _, m := range result {
				byKey[m["key"].(string)] = m
			}

			for _, key := range tt.wantPlatformKeys {
				m, ok := byKey[key]
				if !ok {
					t.Errorf("expected key %q in result", key)
					continue
				}
				if m["isPlatformInjected"] != true {
					t.Errorf("key %q: expected isPlatformInjected=true, got %v", key, m["isPlatformInjected"])
				}
			}

			for _, key := range tt.wantNonPlatformKeys {
				m, ok := byKey[key]
				if !ok {
					t.Errorf("expected key %q in result", key)
					continue
				}
				if _, has := m["isPlatformInjected"]; has {
					t.Errorf("key %q: should NOT have isPlatformInjected, but it does", key)
				}
			}
		})
	}
}

func TestEnvVarsToMaps_KeysOnly(t *testing.T) {
	t.Parallel()

	envs := []platform.ServiceEnvVar{
		{ID: "e1", Key: "PORT", Content: "3000"},
		{ID: "e2", Key: "DB_URL", Content: "${db_connectionString}"},
		{ID: "e3", Key: "zeropsSubdomain", Content: "https://app-1df2-3000.prg1.zerops.app"},
	}

	result := envVarsToMaps(envs, false)
	if len(result) != 3 {
		t.Fatalf("expected 3 envs, got %d", len(result))
	}

	for _, m := range result {
		if _, hasValue := m["value"]; hasValue {
			t.Errorf("key %q: should NOT have value when includeValues=false", m["key"])
		}
		if m["key"] == nil {
			t.Error("key should always be present")
		}
	}

	// Annotations still present without values.
	byKey := make(map[string]map[string]any, len(result))
	for _, m := range result {
		byKey[m["key"].(string)] = m
	}

	if byKey["DB_URL"]["isReference"] != true {
		t.Error("DB_URL should have isReference=true even without values")
	}
	if byKey["zeropsSubdomain"]["isPlatformInjected"] != true {
		t.Error("zeropsSubdomain should have isPlatformInjected=true even without values")
	}
	if _, has := byKey["PORT"]["isReference"]; has {
		t.Error("PORT should not have isReference")
	}
}

// TestEnvVarsToMaps_RedactsCredentialValues pins B10d: a ZCP-managed credential
// (GIT_TOKEN, ZCP_API_KEY) must have its VALUE masked when includeValues=true.
// The platform doesn't mask project-level GIT_TOKEN (its sensitive flag does
// not persist), so a value dump would otherwise leak the PAT verbatim.
func TestEnvVarsToMaps_RedactsCredentialValues(t *testing.T) {
	t.Parallel()

	envs := []platform.ServiceEnvVar{
		{ID: "e1", Key: "PORT", Content: "3000"},
		{ID: "e2", Key: GitTokenEnvKey, Content: "ghp_SECRET_TOKEN_VALUE"},
		{ID: "e3", Key: "ZCP_API_KEY", Content: "zcp_SECRET_KEY_VALUE"},
	}

	result := envVarsToMaps(envs, true)
	byKey := make(map[string]map[string]any, len(result))
	for _, m := range result {
		byKey[m["key"].(string)] = m
	}

	// Non-credential value passes through.
	if byKey["PORT"]["value"] != "3000" {
		t.Errorf("PORT value should pass through, got %v", byKey["PORT"]["value"])
	}
	// Credentials are masked, never echoed.
	for _, key := range []string{GitTokenEnvKey, "ZCP_API_KEY"} {
		v, _ := byKey[key]["value"].(string)
		if strings.Contains(v, "SECRET") {
			t.Errorf("%s value leaked: %q", key, v)
		}
		if byKey[key]["isCredentialRedacted"] != true {
			t.Errorf("%s should be flagged isCredentialRedacted", key)
		}
	}
}

func TestFindEnvIDByKey(t *testing.T) {
	t.Parallel()

	envs := []platform.ServiceEnvVar{
		{ID: "env-1", Key: "DB_HOST", Content: "localhost"},
		{ID: "env-2", Key: "DB_PORT", Content: "5432"},
	}

	tests := []struct {
		name   string
		key    string
		wantID string
	}{
		{name: "Found", key: "DB_HOST", wantID: "env-1"},
		{name: "NotFound", key: "MISSING", wantID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := findEnvIDByKey(envs, tt.key)
			if got != tt.wantID {
				t.Fatalf("expected %q, got %q", tt.wantID, got)
			}
		})
	}
}

// TestAnnotateConnectionStringShape pins the discover-output annotation
// for Postgres + MariaDB connectionString (key omits /${dbName}).
// Empirical basis: plans/audit-env-vars-20260515/VERIFY-reserved-names.md §D
// — postgresql@18 in eval-zcp returns
// "postgresql://${user}:${password}@${hostname}:${port}" verbatim.
func TestAnnotateConnectionStringShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		serviceType      string
		hostname         string
		envs             []map[string]any
		wantAnnotated    bool
		wantWarnContains string
	}{
		{
			name:        "postgresql_db_hostname_canonical_prefix",
			serviceType: "postgresql@18",
			hostname:    "db",
			envs: []map[string]any{
				{"key": "hostname"},
				{"key": "connectionString", "isReference": true},
				{"key": "dbName"},
			},
			wantAnnotated:    true,
			wantWarnContains: "${db_user}",
		},
		{
			name:        "postgresql_descriptive_hostname_uses_actual_name",
			serviceType: "postgresql@18",
			hostname:    "appdb",
			envs: []map[string]any{
				{"key": "connectionString", "isReference": true},
			},
			wantAnnotated:    true,
			wantWarnContains: "${appdb_user}",
		},
		{
			name:        "mariadb_multi_db_hostname_uses_actual_name",
			serviceType: "mariadb@10.6",
			hostname:    "customers_pg",
			envs: []map[string]any{
				{"key": "connectionString", "isReference": true},
			},
			wantAnnotated:    true,
			wantWarnContains: "${customers_pg_hostname}",
		},
		{
			name:        "valkey_no_annotation",
			serviceType: "valkey@7.2",
			hostname:    "cache",
			envs: []map[string]any{
				{"key": "connectionString", "isReference": true},
			},
			wantAnnotated: false,
		},
		{
			name:        "clickhouse_no_annotation",
			serviceType: "clickhouse@25.3",
			hostname:    "analytics",
			envs: []map[string]any{
				{"key": "connectionString", "isReference": true},
			},
			wantAnnotated: false,
		},
		{
			name:        "postgresql_no_connectionString_no_annotation",
			serviceType: "postgresql@18",
			hostname:    "db",
			envs: []map[string]any{
				{"key": "hostname"},
				{"key": "port"},
			},
			wantAnnotated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			annotateConnectionStringShape(tt.envs, tt.serviceType, tt.hostname)
			var annotated map[string]any
			for _, m := range tt.envs {
				if m["key"] == "connectionString" {
					annotated = m
					break
				}
			}
			if !tt.wantAnnotated {
				if annotated != nil {
					if _, hasWarn := annotated["warning"]; hasWarn {
						t.Errorf("unexpected warning on serviceType=%q connectionString: %v", tt.serviceType, annotated["warning"])
					}
					if _, hasFlags := annotated["completenessFlags"]; hasFlags {
						t.Errorf("unexpected completenessFlags on serviceType=%q connectionString: %v", tt.serviceType, annotated["completenessFlags"])
					}
				}
				return
			}
			if annotated == nil {
				t.Fatal("expected connectionString entry, got none")
			}
			flags, ok := annotated["completenessFlags"].(map[string]any)
			if !ok {
				t.Fatalf("expected completenessFlags map, got %T", annotated["completenessFlags"])
			}
			if flags["includesDbName"] != false {
				t.Errorf("completenessFlags.includesDbName = %v, want false", flags["includesDbName"])
			}
			warn, ok := annotated["warning"].(string)
			if !ok {
				t.Fatalf("expected warning string, got %T", annotated["warning"])
			}
			if !strings.Contains(warn, tt.wantWarnContains) {
				t.Errorf("warning missing %q\n\tgot: %s", tt.wantWarnContains, warn)
			}
		})
	}
}

// TestConnectionStringAnnotation_AtomConsistency pins the discover-output
// warning text + atom worked example to use the SAME ${var} placeholders.
// If either drifts (warning mentions ${db_dbName}, atom uses ${db_name}),
// the agent gets contradictory templates.
//
// Empirical basis: VERIFY-reserved-names.md §D — postgresql@18 connectionString
// shape verified live.
func TestConnectionStringAnnotation_AtomConsistency(t *testing.T) {
	t.Parallel()

	// Synthesize what the warning looks like for postgresql with the
	// canonical "db" hostname — the convention the env-var-model atom
	// documents. Custom hostnames propagate to the placeholders (e.g.
	// ${appdb_user} for a service hostnamed `appdb`); this regression
	// case pins the canonical shape against the atom.
	envs := []map[string]any{{"key": "connectionString"}}
	annotateConnectionStringShape(envs, "postgresql@18", "db")
	warn, _ := envs[0]["warning"].(string)

	atomPath := "../content/atoms/develop-env-var-model.md"
	body, err := os.ReadFile(atomPath)
	if err != nil {
		t.Fatalf("read env-var-model atom: %v", err)
	}
	atomText := string(body)

	// Every ${...} placeholder in the warning must also appear in the
	// atom — verifies the worked example and the runtime annotation
	// teach the same composition shape.
	placeholders := []string{
		"${db_user}",
		"${db_password}",
		"${db_hostname}",
		"${db_port}",
		"${db_dbName}",
	}
	for _, p := range placeholders {
		if !strings.Contains(warn, p) {
			t.Errorf("connectionString warning missing placeholder %s\n\twarning: %s", p, warn)
		}
		if !strings.Contains(atomText, p) {
			t.Errorf("develop-env-var-model.md missing placeholder %s — atom and runtime warning must use the same composition shape", p)
		}
	}
}
