package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
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

// TestKeepPicture_KeepsAWorkingDaysPictures: run 12's Sage took 69 pictures
// over seven and a half hours — the "before" of every page at the start, the
// "after" at the end — and described its change with 30 of them. Every one is
// still kept when the description comes: a picture is kept for PictureKeepFor,
// not only while it is among the newest few.
func TestKeepPicture_KeepsAWorkingDaysPictures(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	start := time.Date(2026, 10, 5, 14, 37, 0, 0, time.UTC)
	for i := range 69 {
		at := start.Add(time.Duration(i) * 6 * time.Minute)
		if _, err := keepPicture(stateDir, fakePNG(i+1), 1440, 900, at, defaultPictureBounds()); err != nil {
			t.Fatalf("keep picture %d: %v", i+1, err)
		}
	}
	for i := 1; i <= 69; i++ {
		if _, _, err := KeptPicture(stateDir, fmt.Sprintf("shot-%d", i)); err != nil {
			t.Errorf("shot-%d is gone after a day's work: %v", i, err)
		}
	}
}

// TestKeepPicture_ForgetsByAgeAndSize: the store's bound is time and bytes —
// a picture goes once it is older than the store keeps, or, oldest first, once
// the newer ones fill the bytes the store keeps — and never the newest, nor
// one a kept description names, which is waiting to go onto a change.
func TestKeepPicture_ForgetsByAgeAndSize(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	size := int64(len(fakePNG(1)))
	tests := []struct {
		name      string
		bounds    pictureBounds
		taken     []time.Duration // after start, one picture each
		described string          // a kept description's words
		wantKept  []string
	}{
		{
			name:     "a picture older than the store keeps goes",
			bounds:   pictureBounds{keepFor: 48 * time.Hour, maxBytes: 1 << 20},
			taken:    []time.Duration{0, time.Hour, 49 * time.Hour},
			wantKept: []string{"shot-2", "shot-3"},
		},
		{
			name:     "past the store's bytes the oldest go first",
			bounds:   pictureBounds{keepFor: 48 * time.Hour, maxBytes: 3 * size},
			taken:    []time.Duration{0, time.Minute, 2 * time.Minute, 3 * time.Minute, 4 * time.Minute},
			wantKept: []string{"shot-3", "shot-4", "shot-5"},
		},
		{
			name:     "the newest is kept even when it alone fills the store",
			bounds:   pictureBounds{keepFor: 48 * time.Hour, maxBytes: 1},
			taken:    []time.Duration{0, time.Minute},
			wantKept: []string{"shot-2"},
		},
		{
			name:      "a kept description's picture outlives both bounds",
			bounds:    pictureBounds{keepFor: 48 * time.Hour, maxBytes: 2 * size},
			taken:     []time.Duration{0, time.Minute, 2 * time.Minute, 3 * time.Minute, 72 * time.Hour},
			described: "## How I checked it\n\n![The count](shot-2)",
			wantKept:  []string{"shot-2", "shot-5"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if tt.described != "" {
				if err := WriteServiceMeta(stateDir, &ServiceMeta{
					Hostname: "appdev", StageHostname: "appstage", BootstrapSession: "test", BootstrappedAt: "2026-09-29",
					HQ: &HQRepoRef{AppID: "a1", Repo: "appdev", Branch: "mate/p1", ChangeDescription: &ChangeDescription{Text: tt.described}},
				}); err != nil {
					t.Fatalf("WriteServiceMeta: %v", err)
				}
			}
			for i, after := range tt.taken {
				if _, err := keepPicture(stateDir, fakePNG(i+1), 800, 600, start.Add(after), tt.bounds); err != nil {
					t.Fatalf("keep picture %d: %v", i+1, err)
				}
			}
			kept, err := KeptPictures(stateDir)
			if err != nil {
				t.Fatalf("KeptPictures: %v", err)
			}
			if !slices.Equal(kept, tt.wantKept) {
				t.Errorf("kept %v, want %v", kept, tt.wantKept)
			}
			for i := range tt.taken {
				id := fmt.Sprintf("shot-%d", i+1)
				_, statErr := os.Stat(filepath.Join(stateDir, "pictures", id+".png"))
				if slices.Contains(tt.wantKept, id) != (statErr == nil) {
					t.Errorf("%s's file: %v, want it there only while it is kept", id, statErr)
				}
			}
			next, err := KeepPicture(stateDir, fakePNG(99), 800, 600)
			if want := fmt.Sprintf("shot-%d", len(tt.taken)+1); err != nil || next.ID != want {
				t.Errorf("the next picture is %q (%v), want %s: an id is never given twice", next.ID, err, want)
			}
		})
	}
}

// TestKeepPicture_OneStoreForEveryProcess: the Mate's conversation, its
// helpers and their subagents each run their own zcp, all on one state dir;
// pictures they take at once share one store and one count — every id once,
// every picture kept.
func TestKeepPicture_OneStoreForEveryProcess(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	const takers, each = 6, 5
	var wg sync.WaitGroup
	ids := make(chan string, takers*each)
	for range takers {
		wg.Go(func() {
			for i := range each {
				pic, err := KeepPicture(stateDir, fakePNG(i), 800, 600)
				if err != nil {
					t.Errorf("KeepPicture: %v", err)
					return
				}
				ids <- pic.ID
			}
		})
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Errorf("%s given twice", id)
		}
		seen[id] = true
	}
	kept, err := KeptPictures(stateDir)
	if err != nil || len(kept) != takers*each || len(seen) != takers*each {
		t.Errorf("kept %d pictures of %d given (%v), want all %d", len(kept), len(seen), err, takers*each)
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
		{"![a link to it is not a picture](https://example.invalid/x.png)", nil},
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
