package skillpacks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func validMarker() Marker {
	return Marker{
		SchemaVersion: markerSchemaVersion,
		PackID:        "superpowers",
		Generation:    uuid.NewString(),
		Target:        string(TargetAgents),
		SkillName:     "alpha",
		SourcePath:    "skills/alpha",
		Commit:        testCommit,
		Digest:        testDigest,
	}
}

func TestValidateMarker_TableDriven(t *testing.T) {
	t.Parallel()
	base := validMarker()
	tests := []struct {
		name    string
		mutate  func(m *Marker)
		wantErr bool
	}{
		{"valid as-is", func(m *Marker) {}, false},
		{"wrong schema version", func(m *Marker) { m.SchemaVersion = 1 }, true},
		{"invalid packId", func(m *Marker) { m.PackID = "Bad_Id" }, true},
		{"invalid generation", func(m *Marker) { m.Generation = "not-a-uuid" }, true},
		{"invalid target", func(m *Marker) { m.Target = "bogus" }, true},
		{"invalid skillName", func(m *Marker) { m.SkillName = "Bad Name" }, true},
		{"invalid sourcePath", func(m *Marker) { m.SourcePath = "../escape" }, true},
		{"invalid commit", func(m *Marker) { m.Commit = "xyz" }, true},
		{"invalid digest", func(m *Marker) { m.Digest = "xyz" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := base
			tt.mutate(&m)
			err := validateMarker(&m)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMarker() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadCopyMarker_Missing(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	writeFile(t, filepath.Join(cwd, "dir", "SKILL.md"), "# x\n")

	outcome, _, err := readCopyMarker(root, "dir", markerIdentity{packID: "superpowers"})
	if err != nil {
		t.Fatalf("readCopyMarker: %v", err)
	}
	if outcome != markerMissing {
		t.Errorf("outcome = %v, want markerMissing", outcome)
	}
}

func TestReadCopyMarker_OwnedMatch(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	writeFile(t, filepath.Join(cwd, "dir", "SKILL.md"), "# x\n")
	m := validMarker()
	if err := writeMarker(root, "dir", m); err != nil {
		t.Fatalf("writeMarker: %v", err)
	}

	want := markerIdentity{packID: m.PackID, generation: m.Generation, target: m.Target, skillName: m.SkillName}
	outcome, got, err := readCopyMarker(root, "dir", want)
	if err != nil {
		t.Fatalf("readCopyMarker: %v", err)
	}
	if outcome != markerOwned {
		t.Errorf("outcome = %v, want markerOwned", outcome)
	}
	if got.Digest != m.Digest {
		t.Errorf("digest = %q, want %q", got.Digest, m.Digest)
	}
}

func TestReadCopyMarker_ForeignIdentity(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	writeFile(t, filepath.Join(cwd, "dir", "SKILL.md"), "# x\n")
	m := validMarker()
	if err := writeMarker(root, "dir", m); err != nil {
		t.Fatalf("writeMarker: %v", err)
	}

	want := markerIdentity{packID: "a-different-pack", generation: m.Generation, target: m.Target, skillName: m.SkillName}
	outcome, _, err := readCopyMarker(root, "dir", want)
	if err != nil {
		t.Fatalf("readCopyMarker: %v", err)
	}
	if outcome != markerForeign {
		t.Errorf("outcome = %v, want markerForeign", outcome)
	}
}

func TestReadCopyMarker_MalformedJSON_Unusable(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	writeFile(t, filepath.Join(cwd, "dir", markerFileName), "{not json")

	outcome, _, err := readCopyMarker(root, "dir", markerIdentity{})
	if err != nil {
		t.Fatalf("readCopyMarker: %v", err)
	}
	if outcome != markerUnusable {
		t.Errorf("outcome = %v, want markerUnusable", outcome)
	}
}

// TestReadCopyMarker_SymlinkedMarker_Unusable is the marker-trust proof: a
// marker path that is itself a symlink — even one pointing at ANOTHER
// copy's genuine marker, which stays inside the workspace and so would not
// trip os.Root's own escape-detection — must never be trusted.
func TestReadCopyMarker_SymlinkedMarker_Unusable(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	writeFile(t, filepath.Join(cwd, "genuine", "SKILL.md"), "# genuine\n")
	genuine := validMarker()
	if err := writeMarker(root, "genuine", genuine); err != nil {
		t.Fatalf("writeMarker: %v", err)
	}

	writeFile(t, filepath.Join(cwd, "borrowed", "SKILL.md"), "# borrowed, unrelated\n")
	writeSymlinkOrSkip(t, filepath.Join(cwd, "genuine", markerFileName), filepath.Join(cwd, "borrowed", markerFileName))

	want := markerIdentity{packID: genuine.PackID, generation: genuine.Generation, target: genuine.Target, skillName: genuine.SkillName}
	outcome, _, err := readCopyMarker(root, "borrowed", want)
	if err != nil {
		t.Fatalf("readCopyMarker: %v", err)
	}
	if outcome != markerUnusable {
		t.Errorf("outcome = %v, want markerUnusable (a symlinked marker must never be trusted)", outcome)
	}
}

func TestReadCopyMarker_UnknownFieldMarker_Unusable(t *testing.T) {
	t.Parallel()
	root, cwd := openTestRoot(t)
	writeFile(t, filepath.Join(cwd, "dir", markerFileName), `{"schemaVersion":2,"packId":"superpowers","generation":"`+uuid.NewString()+`","target":"agents","skillName":"alpha","sourcePath":"skills/alpha","commit":"`+testCommit+`","digest":"`+testDigest+`","extra":"field"}`)

	outcome, _, err := readCopyMarker(root, "dir", markerIdentity{})
	if err != nil {
		t.Fatalf("readCopyMarker: %v", err)
	}
	if outcome != markerUnusable {
		t.Errorf("outcome = %v, want markerUnusable (unknown field must be rejected)", outcome)
	}
}

func TestReadCopyMarker_HardReadError_ReturnsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping under root: permission bits are not enforced")
	}
	t.Parallel()
	root, cwd := openTestRoot(t)
	writeFile(t, filepath.Join(cwd, "dir", markerFileName), `{"schemaVersion":2}`)
	if err := os.Chmod(filepath.Join(cwd, "dir", markerFileName), 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(cwd, "dir", markerFileName), 0o644) })

	_, _, err := readCopyMarker(root, "dir", markerIdentity{})
	if err == nil {
		t.Fatal("expected a hard error for an unreadable marker file")
	}
}
