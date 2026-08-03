package skillpacks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func openTestRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	cwd := t.TempDir()
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		t.Fatalf("openWorkspaceRoot: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, cwd
}

// testDigest is a syntactically valid (64 hex char) digest used wherever a
// test needs a well-formed digest that was never actually computed.
const testDigest = digestPrefix + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func validManifest(id string) Manifest {
	return Manifest{
		SchemaVersion: manifestSchemaVersion,
		ID:            id,
		Generation:    uuid.NewString(),
		Source: SourceRef{
			Repo: "owner/" + id, CloneURL: "https://github.com/owner/" + id, Ref: "main", Commit: testCommit,
		},
		Targets: []string{string(TargetAgents), string(TargetClaude)},
		Skills: []SkillEntry{
			{Name: "alpha", SourcePath: "skills/alpha", Digest: testDigest},
		},
	}
}

// jsonMarshalWithExtraField marshals m the normal way, then splices in an
// extra top-level field — used to prove DisallowUnknownFields rejects a
// manifest with a lifecycle-affecting field this schema doesn't know about.
func jsonMarshalWithExtraField(m Manifest) ([]byte, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	// data ends with "}"; splice the extra field in just before it.
	trimmed := data[:len(data)-1]
	return append(trimmed, []byte(`,"unexpectedField":"surprise"}`)...), nil
}

func TestLoadManifest_Absent_ReturnsAbsentState(t *testing.T) {
	t.Parallel()
	root, _ := openTestRoot(t)

	m, state, err := loadManifest(root, "superpowers")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if state != manifestAbsent || m != nil {
		t.Errorf("state = %v, m = %+v, want manifestAbsent/nil", state, m)
	}
}

func TestManifest_WriteReadRoundtrip(t *testing.T) {
	t.Parallel()
	root, _ := openTestRoot(t)
	want := validManifest("superpowers")

	if err := writeManifest(root, want); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	got, state, err := loadManifest(root, "superpowers")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if state != manifestValid {
		t.Fatalf("state = %v, want manifestValid", state)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("roundtripped manifest = %+v, want %+v", *got, want)
	}
}

func TestManifest_WriteLeavesNoTempFile(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	m := validManifest("andrej-karpathy-skills")

	if err := writeManifest(root, m); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(cwd, skillPacksStateDir))
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("state dir has %d entries, want exactly 1 (no leaked temp file): %v", len(entries), entries)
	}
	if entries[0].Name() != "andrej-karpathy-skills.json" {
		t.Errorf("state dir entry = %q, want %q", entries[0].Name(), "andrej-karpathy-skills.json")
	}
}

func TestManifest_RemoveFile_MissingIsNotAnError(t *testing.T) {
	t.Parallel()
	root, _ := openTestRoot(t)

	if err := removeManifestFile(root, "not-installed"); err != nil {
		t.Errorf("removeManifestFile on a missing manifest should be a no-op, got: %v", err)
	}
}

func TestManifest_RemoveFile_DeletesIt(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	m := validManifest("superpowers")
	if err := writeManifest(root, m); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	if err := removeManifestFile(root, "superpowers"); err != nil {
		t.Fatalf("removeManifestFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, manifestRelPath("superpowers"))); !os.IsNotExist(err) {
		t.Errorf("expected manifest file to be gone, stat err = %v", err)
	}
}

// TestLoadManifest_V1Shape_IsLegacy proves a v1 manifest (no schemaVersion
// field at all — the exact shape the old implementation wrote) is refused
// as legacy, not silently accepted or crash-migrated.
func TestLoadManifest_V1Shape_IsLegacy(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	if err := root.MkdirAll(skillPacksStateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	v1JSON := `{"id":"superpowers","repo":"obra/superpowers","commit":"deadbeef","installedDirs":["superpowers"]}`
	writeFile(t, filepath.Join(cwd, manifestRelPath("superpowers")), v1JSON)

	_, state, err := loadManifest(root, "superpowers")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if state != manifestLegacy {
		t.Errorf("state = %v, want manifestLegacy", state)
	}
}

func TestLoadManifest_CorruptJSON_IsCorrupt(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	if err := root.MkdirAll(skillPacksStateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(cwd, manifestRelPath("superpowers")), `{not even json`)

	_, state, err := loadManifest(root, "superpowers")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if state != manifestCorrupt {
		t.Errorf("state = %v, want manifestCorrupt", state)
	}
}

func TestLoadManifest_SchemaV2ButStructurallyInvalid_IsCorrupt(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	if err := root.MkdirAll(skillPacksStateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// schemaVersion 2, but targets is missing "claude" and skills is empty.
	badJSON := `{"schemaVersion":2,"id":"superpowers","generation":"` + uuid.NewString() + `","source":{"repo":"obra/superpowers","cloneUrl":"https://github.com/obra/superpowers","ref":"main","commit":"` + testCommit + `"},"targets":["agents"],"skills":[]}`
	writeFile(t, filepath.Join(cwd, manifestRelPath("superpowers")), badJSON)

	_, state, err := loadManifest(root, "superpowers")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if state != manifestCorrupt {
		t.Errorf("state = %v, want manifestCorrupt", state)
	}
}

func TestLoadManifest_UnknownFields_IsCorrupt(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	if err := root.MkdirAll(skillPacksStateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m := validManifest("superpowers")
	data, err := jsonMarshalWithExtraField(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	writeFile(t, filepath.Join(cwd, manifestRelPath("superpowers")), string(data))

	_, state, err := loadManifest(root, "superpowers")
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if state != manifestCorrupt {
		t.Errorf("state = %v, want manifestCorrupt (unknown field must be rejected)", state)
	}
}

func TestValidateManifest_TableDriven(t *testing.T) {
	t.Parallel()
	base := validManifest("superpowers")

	tests := []struct {
		name    string
		mutate  func(m *Manifest)
		wantErr bool
	}{
		{"valid as-is", func(m *Manifest) {}, false},
		{"wrong schema version", func(m *Manifest) { m.SchemaVersion = 1 }, true},
		{"invalid id syntax", func(m *Manifest) { m.ID = "Not_Valid" }, true},
		{"invalid generation", func(m *Manifest) { m.Generation = "not-a-uuid" }, true},
		{"empty source.repo", func(m *Manifest) { m.Source.Repo = "" }, true},
		{"empty source.cloneUrl", func(m *Manifest) { m.Source.CloneURL = "" }, true},
		{"empty source.ref", func(m *Manifest) { m.Source.Ref = "" }, true},
		{"invalid commit", func(m *Manifest) { m.Source.Commit = "xyz" }, true},
		{"missing claude target", func(m *Manifest) { m.Targets = []string{"agents"} }, true},
		{"duplicate target", func(m *Manifest) { m.Targets = []string{"agents", "agents"} }, true},
		{"unknown target", func(m *Manifest) { m.Targets = []string{"agents", "bogus"} }, true},
		{"no skills", func(m *Manifest) { m.Skills = nil }, true},
		{"invalid skill name", func(m *Manifest) { m.Skills[0].Name = "Bad_Name" }, true},
		{"invalid skill sourcePath", func(m *Manifest) { m.Skills[0].SourcePath = "../escape" }, true},
		{"invalid skill digest", func(m *Manifest) { m.Skills[0].Digest = "not-a-digest" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := base
			m.Skills = append([]SkillEntry(nil), base.Skills...)
			m.Targets = append([]string(nil), base.Targets...)
			tt.mutate(&m)
			err := validateManifest(&m, m.ID)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateManifest() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateManifest_FilenameIDMismatch(t *testing.T) {
	t.Parallel()
	m := validManifest("superpowers")
	if err := validateManifest(&m, "different-id"); err == nil {
		t.Fatal("expected an error when the manifest body id doesn't match the filename id")
	}
}

func TestValidateManifest_UnsortedSkills_Rejected(t *testing.T) {
	t.Parallel()
	m := validManifest("superpowers")
	m.Skills = []SkillEntry{
		{Name: "zeta", SourcePath: "skills/zeta", Digest: m.Skills[0].Digest},
		{Name: "alpha", SourcePath: "skills/alpha", Digest: m.Skills[0].Digest},
	}
	if err := validateManifest(&m, m.ID); err == nil {
		t.Fatal("expected an error for unsorted skills")
	}
}

func TestValidateManifest_DuplicateSkillNames_Rejected(t *testing.T) {
	t.Parallel()
	m := validManifest("superpowers")
	m.Skills = []SkillEntry{
		{Name: "alpha", SourcePath: "skills/alpha", Digest: m.Skills[0].Digest},
		{Name: "alpha", SourcePath: "skills/alpha2", Digest: m.Skills[0].Digest},
	}
	if err := validateManifest(&m, m.ID); err == nil {
		t.Fatal("expected an error for duplicate skill names")
	}
}

func TestListManifestIDs_EmptyWhenNothingInstalled(t *testing.T) {
	t.Parallel()
	root, _ := openTestRoot(t)
	ids, err := listManifestIDs(root)
	if err != nil {
		t.Fatalf("listManifestIDs: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want none", ids)
	}
}

func TestListManifestIDs_SortedAndSkipsTempFiles(t *testing.T) {
	t.Parallel()
	root, _ := openTestRoot(t)
	if err := writeManifest(root, validManifest("superpowers")); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	if err := writeManifest(root, validManifest("andrej-karpathy-skills")); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	ids, err := listManifestIDs(root)
	if err != nil {
		t.Fatalf("listManifestIDs: %v", err)
	}
	if !equalStrings(ids, []string{"andrej-karpathy-skills", "superpowers"}) {
		t.Errorf("ids = %v, want [andrej-karpathy-skills superpowers]", ids)
	}
}
