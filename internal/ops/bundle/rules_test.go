package bundle

import "testing"

func TestManagedEntryWithRules_LocalStorageSingle_PreservesTypeWithoutModeOrProfile(t *testing.T) {
	t.Parallel()
	entry := managedEntryWithRules(ManagedServiceEntry{
		Hostname: "data", Type: "local-storage:single@1", Mode: "NON_HA",
	}, true, false)
	if got := entry["type"]; got != "local-storage:single@1" {
		t.Errorf("type = %v, want exact local-storage:single@1", got)
	}
	for _, forbidden := range []string{"mode", "profile", "objectStorageSize", "objectStoragePolicy"} {
		if got, ok := entry[forbidden]; ok {
			t.Errorf("%s = %v, want field absent", forbidden, got)
		}
	}
}

// TestManagedEntryWithRules pins the single-owner emission contract for managed
// service entries: HA-ness lives in the type VARIANT (not a `mode:` field), a
// sibling `mode` survives only as a bare-legacy BC fallback, and PostgreSQL/
// Valkey carry a `profile` (launch → production default; export → source tier).
func TestManagedEntryWithRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		entry         ManagedServiceEntry
		launchPromote bool
		keepNonHA     bool
		wantType      string
		wantMode      any // nil = absent
		wantProfile   any // nil = absent
	}{
		{
			name:          "launch promote postgres → :ha variant + production profile, no mode",
			entry:         ManagedServiceEntry{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			launchPromote: true,
			wantType:      "postgresql:ha@16",
			wantMode:      nil,
			wantProfile:   "oltp-staging",
		},
		{
			name:          "launch keepNonHA postgres → :single variant + production profile, no mode",
			entry:         ManagedServiceEntry{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			launchPromote: true,
			keepNonHA:     true,
			wantType:      "postgresql:single@16",
			wantMode:      nil,
			wantProfile:   "oltp-staging",
		},
		{
			name:          "launch promote valkey → :ha variant + staging profile",
			entry:         ManagedServiceEntry{Hostname: "cache", Type: "valkey@7.2"},
			launchPromote: true,
			wantType:      "valkey:ha@7.2",
			wantMode:      nil,
			wantProfile:   "staging",
		},
		{
			name:        "export variant postgres → type kept, no redundant mode, live profile carried",
			entry:       ManagedServiceEntry{Hostname: "db", Type: "postgresql:single@18", Mode: "NON_HA", Profile: "oltp-hobby"},
			wantType:    "postgresql:single@18",
			wantMode:    nil, // variant authoritative → no sibling mode
			wantProfile: "oltp-hobby",
		},
		{
			name:        "export bare legacy postgres → BC mode kept, no profile (none read)",
			entry:       ManagedServiceEntry{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
			wantType:    "postgresql@16",
			wantMode:    "NON_HA",
			wantProfile: nil,
		},
		{
			name:        "export non-profile-bearing variant (mariadb) → no profile, no redundant mode",
			entry:       ManagedServiceEntry{Hostname: "db", Type: "mariadb:single@10.6", Mode: "NON_HA"},
			wantType:    "mariadb:single@10.6",
			wantMode:    nil,
			wantProfile: nil,
		},
		{
			name:        "object-storage → no mode, no profile, objectStorageSize emitted",
			entry:       ManagedServiceEntry{Hostname: "storage", Type: "object-storage", QuotaGBytes: 5},
			wantType:    "object-storage",
			wantMode:    nil,
			wantProfile: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry := managedEntryWithRules(tt.entry, tt.launchPromote, tt.keepNonHA)
			if entry["type"] != tt.wantType {
				t.Errorf("type = %v, want %v", entry["type"], tt.wantType)
			}
			if got := entry["mode"]; got != tt.wantMode {
				t.Errorf("mode = %v, want %v", got, tt.wantMode)
			}
			if got := entry["profile"]; got != tt.wantProfile {
				t.Errorf("profile = %v, want %v", got, tt.wantProfile)
			}
		})
	}
}
