// Package hq is zcp's side of HQ: the project's Mate API and git, the Git
// credential Mate holds for its dev services, and the profile's Observe reads
// (docs/spec-mate.md §3.3, §3.6). Observe presents its narrow child credential
// alongside the current native project key as parent proof.
package hq

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	// OrgID is the Zerops organisation (clientId) that owns the project: the
	// first path segment of every repository address in HQ (RepoURLAt). It is
	// not in the environment, so IdentityFrom leaves it empty and the caller
	// that resolved the project sets it; nothing else needs it, so check()
	// does not.
	OrgID string
	// ChangesPath is the file the Mate's server keeps the link's last
	// `state` frame's changes in (ChangesPath), on the same disk.
	ChangesPath string
	// State is the Mate server's last state and link standing.
	State MateStateSource
}

// IdentityFrom reads the identity from getenv — in a container the live env
// store, so a regenerated key is the one presented.
func IdentityFrom(getenv func(string) string) Identity {
	return Identity{
		Origin:    strings.TrimRight(strings.TrimSpace(getenv(EnvOrigin)), "/"),
		Key:       strings.TrimSpace(getenv(EnvKey)),
		ProjectID: strings.TrimSpace(getenv(EnvProjectID)),

		ChangesPath: ChangesPath(),
		State:       NewFileMateStateSource(MateStatePath(), strings.TrimSpace(getenv(EnvProjectID))),
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
		return fmt.Errorf("%w: no HQ: %s is unset and the installed Mate declares none", ErrNoIdentity, EnvOrigin)
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
// HQ revokes; zcp names it to the server and reads it nowhere: its git
// reaches HQ through the relay with the project key.
func GitCredentialPath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "hq", "git-credential")
}
