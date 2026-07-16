package provider

import "testing"

func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typ  string
		want Family
	}{
		{"postgresql:single@18", FamilyTabular},
		{"mariadb@10.6", FamilyTabular},
		{"mysql:ha@5.7", FamilyTabular},
		{"clickhouse@24", FamilyTabular},
		{"valkey@7", FamilyKV},
		{"keydb", FamilyKV}, // classified even though removed from the platform
		{"object-storage", FamilyObject},
		{"elasticsearch@8", FamilyDocument},
		{"meilisearch", FamilyDocument},
		{"typesense", FamilyDocument},
		{"qdrant", FamilyDocument},
		{"kafka", FamilyStream},
		{"nats", FamilyStream},
		{"shared-storage", FamilyFile},
		{"nodejs@22", FamilyUnknown},
		{"", FamilyUnknown},
	}
	for _, c := range cases {
		if got := Classify(c.typ); got != c.want {
			t.Errorf("Classify(%q) = %q, want %q", c.typ, got, c.want)
		}
	}
}

func TestSupportFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typ  string
		want Support
	}{
		{"object-storage", SupportFull},
		{"postgresql:single@18", SupportFull},
		{"valkey@7", SupportFull},
		{"elasticsearch@9.2", SupportFull},
		{"meilisearch:single@1.44", SupportFull},
		{"typesense@30.2", SupportFull},
		{"clickhouse@24", SupportViewOnly},
		{"qdrant", SupportViewOnly},
		{"kafka", SupportViewOnly},
		{"nats@2.12", SupportViewOnly},
		{"shared-storage", SupportNotYet},
		{"nodejs@22", SupportNotYet},
	}
	for _, c := range cases {
		if got := SupportFor(c.typ); got != c.want {
			t.Errorf("SupportFor(%q) = %q, want %q", c.typ, got, c.want)
		}
	}
}

func TestDerivedCaps_PostureAndClassificationGateTogether(t *testing.T) {
	t.Parallel()
	// writes off -> everything read-only, no edit/upload flags, regardless of family.
	for _, fam := range []Family{FamilyObject, FamilyTabular, FamilyKV, FamilyDocument} {
		c := DerivedCaps(fam, SupportFull, false)
		if !c.ReadOnly || c.EditBlob || c.EditTabular || c.Upload {
			t.Errorf("%s posture-off: want read-only with no edit flags, got %+v", fam, c)
		}
	}
	// writes on + full support -> the family's edit flags light up and ReadOnly clears.
	if c := DerivedCaps(FamilyObject, SupportFull, true); c.ReadOnly || !c.EditBlob || !c.Upload {
		t.Errorf("object rw: %+v", c)
	}
	if c := DerivedCaps(FamilyTabular, SupportFull, true); c.ReadOnly || !c.EditTabular || !c.Query {
		t.Errorf("tabular rw: %+v", c)
	}
	if c := DerivedCaps(FamilyKV, SupportFull, true); c.ReadOnly || !c.EditBlob || !c.EditTabular || !c.TTL {
		t.Errorf("kv rw: %+v", c)
	}
	// view-only family stays read-only even with writes on (the invariant).
	if c := DerivedCaps(FamilyTabular, SupportViewOnly, true); !c.ReadOnly || c.EditTabular {
		t.Errorf("view-only must stay read-only: %+v", c)
	}
	// query is always available on tabular (engine-enforced READ ONLY tx), even view-only.
	if c := DerivedCaps(FamilyTabular, SupportViewOnly, false); !c.Query {
		t.Errorf("tabular query must always be available: %+v", c)
	}
	// stream is never writable.
	if c := DerivedCaps(FamilyStream, SupportViewOnly, true); !c.ReadOnly {
		t.Errorf("stream must be read-only: %+v", c)
	}
}

func TestServiceActions_FamilyPostureMatrix_Result(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		family      Family
		support     Support
		allowWrites bool
		wantEnabled []ActionID
	}{
		{
			name:        "object/read-only",
			family:      FamilyObject,
			support:     SupportFull,
			allowWrites: false,
			wantEnabled: []ActionID{ActionReadBlob},
		},
		{
			name:        "object/writes",
			family:      FamilyObject,
			support:     SupportFull,
			allowWrites: true,
			wantEnabled: []ActionID{ActionReadBlob, ActionWriteBlob, ActionDeleteNode, ActionRenameObject, ActionUploadObject},
		},
		{
			name:        "tabular/read-only",
			family:      FamilyTabular,
			support:     SupportFull,
			allowWrites: false,
			wantEnabled: []ActionID{ActionQuerySQL, ActionReadTable, ActionShowVPNGate},
		},
		{
			name:        "tabular/writes",
			family:      FamilyTabular,
			support:     SupportFull,
			allowWrites: true,
			wantEnabled: []ActionID{ActionQuerySQL, ActionReadTable, ActionEditCell, ActionInsertRow, ActionDeleteRow, ActionShowVPNGate},
		},
		{
			name:        "tabular/view-only-writes",
			family:      FamilyTabular,
			support:     SupportViewOnly,
			allowWrites: true,
			wantEnabled: []ActionID{ActionQuerySQL, ActionReadTable, ActionShowVPNGate},
		},
		{
			name:        "kv/read-only",
			family:      FamilyKV,
			support:     SupportFull,
			allowWrites: false,
			wantEnabled: []ActionID{ActionReadBlob, ActionReadTable, ActionShowVPNGate},
		},
		{
			name:        "kv/writes",
			family:      FamilyKV,
			support:     SupportFull,
			allowWrites: true,
			wantEnabled: []ActionID{ActionReadBlob, ActionWriteBlob, ActionDeleteNode, ActionReadTable, ActionEditKVEntry, ActionSetTTL, ActionCreateKey, ActionShowVPNGate},
		},
		{
			name:        "document/writes",
			family:      FamilyDocument,
			support:     SupportFull,
			allowWrites: true,
			wantEnabled: []ActionID{ActionReadBlob, ActionSearchDocs, ActionWriteBlob, ActionDeleteNode, ActionCreateDoc, ActionShowVPNGate},
		},
		{
			// SearchDocs is a READ action, enabled for any non-not-yet document
			// service (view-only included); CreateDoc (mutating) stays disabled
			// under the view-only tier.
			name:        "document/view-only-writes",
			family:      FamilyDocument,
			support:     SupportViewOnly,
			allowWrites: true,
			wantEnabled: []ActionID{ActionReadBlob, ActionSearchDocs, ActionShowVPNGate},
		},
		{
			name:        "stream/view-only-writes",
			family:      FamilyStream,
			support:     SupportViewOnly,
			allowWrites: true,
			wantEnabled: []ActionID{ActionReadBlob, ActionShowVPNGate},
		},
		{
			name:        "file/not-yet-writes",
			family:      FamilyFile,
			support:     SupportNotYet,
			allowWrites: true,
			wantEnabled: nil,
		},
		{
			name:        "unknown/not-yet-writes",
			family:      FamilyUnknown,
			support:     SupportNotYet,
			allowWrites: true,
			wantEnabled: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ServiceActions(tc.family, tc.support, tc.allowWrites)
			assertEnabledActions(t, got, tc.wantEnabled)
			for _, a := range got {
				if !a.Enabled && a.Reason == "" {
					t.Fatalf("%s: disabled action %s has no reason: %+v", tc.name, a.ID, got)
				}
			}
		})
	}
}

func TestServiceActions_ConformsToSupportAndPosture(t *testing.T) {
	t.Parallel()
	families := []Family{FamilyObject, FamilyTabular, FamilyKV, FamilyDocument, FamilyStream, FamilyFile, FamilyUnknown}
	supports := []Support{SupportFull, SupportViewOnly, SupportNotYet}
	postures := []bool{false, true}
	for _, fam := range families {
		for _, sup := range supports {
			for _, allowWrites := range postures {
				actions := ServiceActions(fam, sup, allowWrites)
				enabled := enabledSet(actions)
				caps := DerivedCaps(fam, sup, allowWrites)
				for _, id := range MutatingActionIDs() {
					if enabled[id] && (!allowWrites || sup != SupportFull) {
						t.Fatalf("%s/%s/allowWrites=%v enabled mutating action %s: %+v", fam, sup, allowWrites, id, actions)
					}
					if enabled[id] && caps.ReadOnly {
						t.Fatalf("%s/%s/allowWrites=%v enabled mutating action %s while DerivedCaps is read-only: caps=%+v actions=%+v", fam, sup, allowWrites, id, caps, actions)
					}
				}
				if allowWrites && sup == SupportFull {
					for _, id := range familyMutatingActionIDs(fam) {
						if !enabled[id] {
							t.Fatalf("%s/%s/allowWrites=true did not enable family mutating action %s: %+v", fam, sup, id, actions)
						}
					}
				}
			}
		}
	}
}

func assertEnabledActions(t *testing.T, actions []Action, want []ActionID) {
	t.Helper()
	got := enabledSet(actions)
	if len(got) != len(want) {
		t.Fatalf("enabled actions = %v, want %v", keys(got), want)
	}
	for _, id := range want {
		if !got[id] {
			t.Fatalf("enabled actions = %v, want %v", keys(got), want)
		}
	}
}

func enabledSet(actions []Action) map[ActionID]bool {
	out := map[ActionID]bool{}
	for _, a := range actions {
		if a.Enabled {
			out[a.ID] = true
		}
	}
	return out
}

func keys(m map[ActionID]bool) []ActionID {
	out := make([]ActionID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}
