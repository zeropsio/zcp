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
// is published as its pull request's body, and the pictures it names go with
// it as the request's attachments; the store remembers where Gitea serves each
// one, so a picture goes onto a request once.
//
// Kept: the newest PictureKeep, and every picture a kept description names —
// words waiting for a request to carry them must find their pictures there.
// An id is never given twice: the next one only ever grows.

// PictureKeep is how many of the newest pictures are kept.
const PictureKeep = 20

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
	// Uploads is where Gitea serves the picture, by the pull request it was
	// attached to ("{org}/{repo}#{number}").
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
	var kept Picture
	err := withPictures(stateDir, func(index *pictureIndex) error {
		if index.Next < 1 {
			index.Next = 1
		}
		kept = Picture{
			ID: pictureIDPrefix + strconv.Itoa(index.Next), Width: width, Height: height,
			TakenAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := os.WriteFile(picturePath(stateDir, kept.ID), png, 0o600); err != nil {
			return fmt.Errorf("keep picture %s: %w", kept.ID, err)
		}
		index.Next++
		index.Pictures = append(index.Pictures, kept)
		prunePictures(stateDir, index)
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

// RecordPictureUpload remembers that the picture id is served at url on the
// pull request target ("{org}/{repo}#{number}").
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

// prunePictures forgets every picture past the newest PictureKeep that no kept
// description names — its entry and its file.
func prunePictures(stateDir string, index *pictureIndex) {
	if len(index.Pictures) <= PictureKeep {
		return
	}
	named := map[string]bool{}
	if metas, err := ListServiceMetas(stateDir); err == nil {
		for _, m := range metas {
			if m == nil || m.Gitea == nil || m.Gitea.ChangeDescription == nil {
				continue
			}
			for _, id := range PictureRefs(m.Gitea.ChangeDescription.Text) {
				named[id] = true
			}
		}
	}
	oldest := len(index.Pictures) - PictureKeep
	kept := make([]Picture, 0, PictureKeep+len(named))
	for i, pic := range index.Pictures {
		if i >= oldest || named[pic.ID] {
			kept = append(kept, pic)
			continue
		}
		_ = os.Remove(picturePath(stateDir, pic.ID))
	}
	index.Pictures = kept
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
