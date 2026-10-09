package hq

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/runtime"
)

// Enrollment is what this container holds of its HQ: the HQ it enrolled
// with, and the Mate credential HQ issued it. The file is the only place the
// credential is kept; nothing prints it.
type Enrollment struct {
	HQ          string `json:"hq"`
	HQProjectID string `json:"hqProjectId"`
	ProjectID   string `json:"projectId"`
	Credential  string `json:"credential"`
	// KeyTokenID is the id of this container's key once HQ took it under the
	// credential (PUT /api/mate/key); empty until then, and after each new
	// enrollment.
	KeyTokenID string `json:"keyTokenId,omitempty"`
	// KeyRefusedID is the id of this container's key HQ answered
	// key_not_its_own for under the credential: not the Mate's own key (it
	// reaches past its project, or is no Mate's key). It is not offered again;
	// a key with another id is.
	KeyRefusedID string `json:"keyRefusedId,omitempty"`
}

// EnrollmentPath is where the enrollment lives: beside mate.env under the
// service user's home, which a container restart keeps and a redeploy loses
// (spec-mate §2.6) — after which the Mate enrolls again and the rotation
// revokes the credential it lost.
func EnrollmentPath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "hq", "enrollment.json")
}

// SaveEnrollment writes e to path, readable by its owner only, replacing
// any earlier one in a single rename.
func SaveEnrollment(path string, e Enrollment) error {
	if err := saveOwnerOnly(path, e); err != nil {
		return fmt.Errorf("hq enrollment: %w", err)
	}
	return nil
}

// saveOwnerOnly writes v as JSON to path in a directory only its owner may
// enter, replacing any earlier file in a single rename: a reader sees the old
// document or the new one, never a torn one.
func saveOwnerOnly(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// LoadEnrollment reads the enrollment at path; found is false when there is
// none.
func LoadEnrollment(path string) (e Enrollment, found bool, err error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Enrollment{}, false, nil
	}
	if err != nil {
		return Enrollment{}, false, fmt.Errorf("hq enrollment: %w", err)
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return Enrollment{}, false, fmt.Errorf("hq enrollment: %w", err)
	}
	return e, true, nil
}
