// Package hq is zcp's side of a Mate's HQ: the key the project's Mate
// presents on every call, the Mate API those calls speak, git against HQ, and
// the Git credential HQ sends the Mate's server for the dev service
// (docs/spec-mate.md §3.3, §3.6). Nothing here enrolls: the key is the
// proof, and HQ recognises it by its hash.
package hq

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
)

const (
	// EnvOrigin names HQ's origin, the setting the Mate's server reads under
	// the same name (spec-mate §3.5): nothing discovers HQ, and development
	// points a container at another through the launch environment.
	EnvOrigin = "T3CODE_ZEROPS_HQ_ORIGIN"
	// EnvKey is the project's key: Zerops' own integration token on the zcp
	// service, a sensitive service variable (spec-mate §3.3).
	EnvKey = "ZCP_API_KEY"
	// EnvProjectID is the Zerops project this container belongs to.
	EnvProjectID = "projectId"
)

// ErrNoIdentity is a container with nothing to present to HQ: no key, no
// project or no origin to present them to.
var ErrNoIdentity = errors.New("this container cannot address its HQ")

// Identity is what a Mate presents to HQ and where: its project's key (never
// printed, never in a URL, a log line or a file), the project the key
// names, and HQ's origin.
type Identity struct {
	Origin    string
	Key       string
	ProjectID string
	// GitCredentialPath is the file the Mate's server keeps its Git
	// credential in (GitCredentialPath): not part of what is presented, but
	// of where this Mate's HQ lives on its disk.
	GitCredentialPath string
}

// IdentityFrom reads the identity from getenv — in a container the live env
// store, so a regenerated key is the one presented.
func IdentityFrom(getenv func(string) string) Identity {
	return Identity{
		Origin:    strings.TrimRight(strings.TrimSpace(getenv(EnvOrigin)), "/"),
		Key:       strings.TrimSpace(getenv(EnvKey)),
		ProjectID: strings.TrimSpace(getenv(EnvProjectID)),

		GitCredentialPath: GitCredentialPath(),
	}
}

// check says what the identity lacks, or nil.
func (id Identity) check() error {
	switch {
	case id.Key == "":
		return fmt.Errorf("%w: %s is not in its environment", ErrNoIdentity, EnvKey)
	case id.ProjectID == "":
		return fmt.Errorf("%w: %s is not in its environment", ErrNoIdentity, EnvProjectID)
	case id.Origin == "":
		return fmt.Errorf("%w: %s names no HQ", ErrNoIdentity, EnvOrigin)
	}
	return nil
}

// Fingerprint names the identity's key and HQ without carrying the key: what
// a failed read is remembered against, so a changed key, project or HQ is a
// new input.
func (id Identity) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{id.Origin, id.ProjectID, id.Key}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// EnvGitCredential names, to the Mate's server, the file it keeps the Mate's
// Git credential in (spec-mate C-3, C-7).
const EnvGitCredential = "T3CODE_ZEROPS_HQ_GIT_CREDENTIAL"

// GitCredentialPath is where the server keeps the Git credential HQ sends it:
// beside mate.env under the service user's home, which a container restart
// keeps. The server writes it, mode 0600, in one rename and removes it when
// HQ revokes; zcp only reads it.
func GitCredentialPath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "hq", "git-credential")
}

// ErrNoGitCredential is no Git credential in the file: the Mate's server is
// not linked to HQ yet, or HQ revoked what it had. The server's own word for
// why is its /mate/setup.json.
var ErrNoGitCredential = errors.New("the Mate's server holds no Git credential from HQ: it is not linked to HQ (its /mate/setup.json says why)")

// ReadGitCredential is the Mate's Git credential in the file at path, read at
// each use: HQ replaces it over the link, and the file is the only copy zcp
// consults.
func ReadGitCredential(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoGitCredential
	}
	if err != nil {
		return "", fmt.Errorf("read the Git credential: %w", err)
	}
	credential := strings.TrimSpace(string(raw))
	if credential == "" {
		return "", ErrNoGitCredential
	}
	return credential, nil
}

// GitCredentialFingerprint names a Git credential without carrying it: what a
// service's record keeps to know which credential its dev service was given.
func GitCredentialFingerprint(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:8])
}

// GitCredential is the Mate's Git credential as the file holds it now, with
// its fingerprint: ErrNoGitCredential while the Mate is not linked. zcp gives
// the dev service a copy and never writes the file.
func (c Client) GitCredential() (credential, fingerprint string, err error) {
	credential, err = ReadGitCredential(c.id.GitCredentialPath)
	if err != nil {
		return "", "", err
	}
	return credential, GitCredentialFingerprint(credential), nil
}
