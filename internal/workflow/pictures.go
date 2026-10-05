package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// A Mate's pictures: every screenshot zerops_browser takes is kept here for a
// while, under an id the tool's result names ("shot-3"), so the Mate can show
// it in its change's description as ![what it shows](shot-3). The description
// is published as its change's body in HQ, and the pictures it names go with
// it as the change's attachments; the store remembers where HQ serves each
// one, so a picture goes onto a change once.
//
// Kept: every picture for PictureKeepFor after it is taken — a change's
// "before" is often taken hours, or a whole step, ahead of its description,
// so the bound is time, not a count of the newest — while the store holds at
// most PictureStoreBytes on disk, past which the oldest go first; and, beyond
// both, every picture a kept description names: words waiting for a change to
// carry them must find their pictures there. The newest is always kept. One
// store per state dir, shared by every zcp process on it under its own lock.
// An id is never given twice: the next one only ever grows.

// PictureKeepFor is how long a picture is kept after it is taken.
const PictureKeepFor = 7 * 24 * time.Hour

// PictureStoreBytes bounds the pictures on disk: past it, the oldest go first.
const PictureStoreBytes int64 = 256 << 20

const (
	picturesDir      = "pictures"
	picturesIndex    = "index.json"
	picturesLockName = ".pictures.lock"
	pictureIDPrefix  = "shot-"
)

// ErrPictureNotKept is a picture id the store has no picture for: one never
// taken, or one pruned since.
var ErrPictureNotKept = errors.New("no such picture is kept")

// Picture is one kept picture.
type Picture struct {
	ID      string `json:"id"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	TakenAt string `json:"takenAt,omitempty"`
	// Bytes is the picture's size on disk.
	Bytes int64 `json:"bytes,omitempty"`
	// Uploads is where HQ serves the picture, by the change it was attached
	// to ("{appId}/{repo}#{number}").
	Uploads map[string]string `json:"uploads,omitempty"`
}

type pictureIndex struct {
	Next     int       `json:"next"`
	Pictures []Picture `json:"pictures"` // oldest first
}

// pictureRef is how a description names a picture: a markdown image whose
// target is the picture's id and nothing else.
var pictureRef = regexp.MustCompile(`!\[([^\]\n]*)\]\(\s*(shot-[1-9][0-9]*)\s*\)`)

// PictureRefs is the pictures text names, each once, in the order it first
// names them.
func PictureRefs(text string) []string {
	var ids []string
	for _, match := range pictureRef.FindAllStringSubmatch(text, -1) {
		if id := match[2]; !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// ReplacePictureRefs replaces every picture text names with what replace
// answers for its alt text and id, leaving everything else as it is.
func ReplacePictureRefs(text string, replace func(alt, id string) string) string {
	return pictureRef.ReplaceAllStringFunc(text, func(ref string) string {
		match := pictureRef.FindStringSubmatch(ref)
		return replace(match[1], match[2])
	})
}

// KeepPicture keeps png as the next picture, with its size in pixels, and
// forgets what the store no longer keeps.
func KeepPicture(stateDir string, png []byte, width, height int) (Picture, error) {
	return keepPicture(stateDir, png, width, height, time.Now(), defaultPictureBounds())
}

// pictureBounds is how long and how much the store keeps.
type pictureBounds struct {
	keepFor  time.Duration
	maxBytes int64
}

func defaultPictureBounds() pictureBounds {
	return pictureBounds{keepFor: PictureKeepFor, maxBytes: PictureStoreBytes}
}

// KeptPictures is the id of every picture the store keeps, oldest first.
func KeptPictures(stateDir string) ([]string, error) {
	var ids []string
	err := withPictures(stateDir, func(index *pictureIndex) error {
		for _, pic := range index.Pictures {
			ids = append(ids, pic.ID)
		}
		return nil
	})
	return ids, err
}

// keepPicture is KeepPicture taken at now, within bounds.
func keepPicture(stateDir string, png []byte, width, height int, now time.Time, bounds pictureBounds) (Picture, error) {
	var kept Picture
	err := withPictures(stateDir, func(index *pictureIndex) error {
		if index.Next < 1 {
			index.Next = 1
		}
		kept = Picture{
			ID: pictureIDPrefix + strconv.Itoa(index.Next), Width: width, Height: height,
			TakenAt: now.UTC().Format(time.RFC3339), Bytes: int64(len(png)),
		}
		if err := os.WriteFile(picturePath(stateDir, kept.ID), png, 0o600); err != nil {
			return fmt.Errorf("keep picture %s: %w", kept.ID, err)
		}
		index.Next++
		index.Pictures = append(index.Pictures, kept)
		prunePictures(stateDir, index, now, bounds)
		return nil
	})
	return kept, err
}

// KeptPicture is the picture id names and its bytes, or ErrPictureNotKept.
func KeptPicture(stateDir, id string) (Picture, []byte, error) {
	var found Picture
	var png []byte
	err := withPictures(stateDir, func(index *pictureIndex) error {
		for _, pic := range index.Pictures {
			if pic.ID != id {
				continue
			}
			data, err := os.ReadFile(picturePath(stateDir, id))
			if err != nil {
				return fmt.Errorf("%s: %w", id, ErrPictureNotKept)
			}
			found, png = pic, data
			return nil
		}
		return fmt.Errorf("%s: %w", id, ErrPictureNotKept)
	})
	return found, png, err
}

// RecordPictureUpload remembers that the picture id is served at url for the
// change target ("{appId}/{repo}#{number}").
func RecordPictureUpload(stateDir, id, target, url string) error {
	return withPictures(stateDir, func(index *pictureIndex) error {
		for i := range index.Pictures {
			if index.Pictures[i].ID != id {
				continue
			}
			if index.Pictures[i].Uploads == nil {
				index.Pictures[i].Uploads = map[string]string{}
			}
			index.Pictures[i].Uploads[target] = url
			return nil
		}
		return fmt.Errorf("%s: %w", id, ErrPictureNotKept)
	})
}

// prunePictures forgets every picture the store no longer keeps — its entry
// and its file: one taken longer than bounds.keepFor before now, and, newest
// to oldest, every one past bounds.maxBytes; never the newest, nor one a kept
// description names.
func prunePictures(stateDir string, index *pictureIndex, now time.Time, bounds pictureBounds) {
	named := map[string]bool{}
	if metas, err := ListServiceMetas(stateDir); err == nil {
		for _, m := range metas {
			if m == nil || m.HQ == nil || m.HQ.ChangeDescription == nil {
				continue
			}
			for _, id := range PictureRefs(m.HQ.ChangeDescription.Text) {
				named[id] = true
			}
		}
	}
	keep := make([]bool, len(index.Pictures))
	var held int64
	full := false
	for i := len(index.Pictures) - 1; i >= 0; i-- {
		pic := index.Pictures[i]
		size := pictureBytes(stateDir, pic)
		if named[pic.ID] {
			keep[i] = true
			continue
		}
		newest := i == len(index.Pictures)-1
		if !newest && (full || pictureExpired(pic, now, bounds.keepFor) || held+size > bounds.maxBytes) {
			full = full || held+size > bounds.maxBytes
			continue
		}
		keep[i] = true
		held += size
	}
	kept := make([]Picture, 0, len(index.Pictures))
	for i, pic := range index.Pictures {
		if keep[i] {
			kept = append(kept, pic)
			continue
		}
		_ = os.Remove(picturePath(stateDir, pic.ID))
	}
	index.Pictures = kept
}

// pictureBytes is pic's size on disk: as kept, or as its file says for a
// picture kept before the store recorded sizes.
func pictureBytes(stateDir string, pic Picture) int64 {
	if pic.Bytes > 0 {
		return pic.Bytes
	}
	if info, err := os.Stat(picturePath(stateDir, pic.ID)); err == nil {
		return info.Size()
	}
	return 0
}

// pictureExpired reports whether pic was taken longer than keepFor before
// now. A picture whose time cannot be read is not expired: the byte bound
// still holds it.
func pictureExpired(pic Picture, now time.Time, keepFor time.Duration) bool {
	taken, err := time.Parse(time.RFC3339, pic.TakenAt)
	return err == nil && now.Sub(taken) > keepFor
}

// withPictures runs fn on the store's index under the store's own lock — a
// state-dir flock distinct from every other, so processes sharing the state
// dir never interleave — and writes the index back when fn succeeded.
func withPictures(stateDir string, fn func(*pictureIndex) error) error {
	if err := os.MkdirAll(filepath.Join(stateDir, picturesDir), 0o700); err != nil {
		return fmt.Errorf("pictures dir: %w", err)
	}
	return withFileLock(filepath.Join(stateDir, picturesLockName), func() error {
		var index pictureIndex
		path := filepath.Join(stateDir, picturesDir, picturesIndex)
		if data, err := os.ReadFile(path); err == nil {
			if jsonErr := json.Unmarshal(data, &index); jsonErr != nil {
				return fmt.Errorf("pictures index: %w", jsonErr)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("pictures index: %w", err)
		}
		if err := fn(&index); err != nil {
			return err
		}
		data, err := json.Marshal(index)
		if err != nil {
			return fmt.Errorf("pictures index: %w", err)
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return fmt.Errorf("pictures index: %w", err)
		}
		return os.Rename(tmp, path)
	})
}

func picturePath(stateDir, id string) string {
	return filepath.Join(stateDir, picturesDir, id+".png")
}
