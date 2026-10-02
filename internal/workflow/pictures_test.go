package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fakePNG is not a picture anyone could look at; the store never decodes one.
func fakePNG(n int) []byte { return fmt.Appendf(nil, "\x89PNG-picture-%d", n) }

// TestKeepPicture_NamesEachPictureOnce: every screenshot a Mate takes is kept
// under the next id, with its size, and an id is never given twice — not even
// after the pictures before it are gone.
func TestKeepPicture_NamesEachPictureOnce(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	for i := 1; i <= 3; i++ {
		pic, err := KeepPicture(stateDir, fakePNG(i), 1280, 720+i)
		if err != nil {
			t.Fatalf("KeepPicture: %v", err)
		}
		if want := fmt.Sprintf("shot-%d", i); pic.ID != want {
			t.Errorf("picture %d is %q, want %q", i, pic.ID, want)
		}
	}
	pic, png, err := KeptPicture(stateDir, "shot-2")
	if err != nil {
		t.Fatalf("KeptPicture: %v", err)
	}
	if !bytes.Equal(png, fakePNG(2)) || pic.Width != 1280 || pic.Height != 722 {
		t.Errorf("shot-2 = %+v %q, want its own bytes and size", pic, png)
	}
	info, err := os.Stat(filepath.Join(stateDir, "pictures", "shot-2.png"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("a picture is the Mate's own: %v %v", info, err)
	}
}

// TestKeepPicture_KeepsTheNewestAndThoseAKeptDescriptionNames: the store keeps
// the newest twenty, and every picture a kept description names however old
// it is — those are waiting to go onto a change.
func TestKeepPicture_KeepsTheNewestAndThoseAKeptDescriptionNames(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := WriteServiceMeta(stateDir, &ServiceMeta{
		Hostname: "appdev", StageHostname: "appstage", BootstrapSession: "test", BootstrappedAt: "2026-09-29",
		HQ: &HQRepoRef{AppID: "a1", Repo: "appdev", Branch: "mate/p1", ChangeDescription: &ChangeDescription{
			Text: "## How I checked it\n\n![The count](shot-2)",
		}},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	for i := 1; i <= PictureKeep+5; i++ {
		if _, err := KeepPicture(stateDir, fakePNG(i), 800, 600); err != nil {
			t.Fatalf("KeepPicture %d: %v", i, err)
		}
	}
	for _, gone := range []string{"shot-1", "shot-3", "shot-4", "shot-5"} {
		if _, _, err := KeptPicture(stateDir, gone); !errors.Is(err, ErrPictureNotKept) {
			t.Errorf("%s is still kept (%v)", gone, err)
		}
		if _, err := os.Stat(filepath.Join(stateDir, "pictures", gone+".png")); !os.IsNotExist(err) {
			t.Errorf("%s's file is still there (%v)", gone, err)
		}
	}
	for _, kept := range []string{"shot-2", "shot-6", fmt.Sprintf("shot-%d", PictureKeep+5)} {
		if _, _, err := KeptPicture(stateDir, kept); err != nil {
			t.Errorf("%s is gone: %v", kept, err)
		}
	}
	next, err := KeepPicture(stateDir, fakePNG(99), 800, 600)
	if err != nil || next.ID != fmt.Sprintf("shot-%d", PictureKeep+6) {
		t.Errorf("the next picture is %q (%v), want a new id", next.ID, err)
	}
}

// TestRecordPictureUpload: a picture is attached to each change once; where
// HQ serves it there is remembered with the picture.
func TestRecordPictureUpload(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if _, err := KeepPicture(stateDir, fakePNG(1), 800, 600); err != nil {
		t.Fatal(err)
	}
	const url = "https://hq.example.invalid/api/apps/a1/changes/appdev/9/attachments/3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b"
	if err := RecordPictureUpload(stateDir, "shot-1", "a1/appdev#9", url); err != nil {
		t.Fatalf("RecordPictureUpload: %v", err)
	}
	pic, _, err := KeptPicture(stateDir, "shot-1")
	if err != nil || pic.Uploads["a1/appdev#9"] != url {
		t.Errorf("uploads = %v (%v), want the URL recorded for the change", pic.Uploads, err)
	}
	if err := RecordPictureUpload(stateDir, "shot-7", "a1/appdev#9", url); !errors.Is(err, ErrPictureNotKept) {
		t.Errorf("recording an upload of a picture nobody keeps = %v", err)
	}
}

// TestPictureRefs: a description names a picture the one way — as a markdown
// image whose target is the picture's id — and nothing else counts.
func TestPictureRefs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		text string
		want []string
	}{
		{"![The count](shot-3)", []string{"shot-3"}},
		{"![a](shot-3) then ![b](shot-12) and ![c](shot-3)", []string{"shot-3", "shot-12"}},
		{"![spaced]( shot-4 )", []string{"shot-4"}},
		{"![](shot-5)", []string{"shot-5"}},
		{"![a link to it is not a picture](https://gitea.example.invalid/x.png)", nil},
		{"[a link](shot-3)", nil},
		{"![not an id](shot-03)", nil},
		{"![not an id](shot-3.png)", nil},
		{"words about shot-3 only", nil},
	}
	for _, tt := range tests {
		if got := PictureRefs(tt.text); !slices.Equal(got, tt.want) {
			t.Errorf("PictureRefs(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

// TestReplacePictureRefs: each reference is replaced where it stands, with
// the words around it untouched.
func TestReplacePictureRefs(t *testing.T) {
	t.Parallel()
	got := ReplacePictureRefs("See ![The count](shot-3) — and ![](shot-4).\n![x](https://elsewhere.invalid/y.png)",
		func(alt, id string) string { return "<" + id + "|" + alt + ">" })
	want := "See <shot-3|The count> — and <shot-4|>.\n![x](https://elsewhere.invalid/y.png)"
	if got != want {
		t.Errorf("ReplacePictureRefs = %q, want %q", got, want)
	}
}
