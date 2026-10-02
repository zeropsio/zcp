package hq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// ChallengeEnv is the project env key the challenge's nonce is written into:
// unmarked, so HQ's Read only credential reads it in the clear.
const ChallengeEnv = "MATE_HQ_CHALLENGE"

// envPolls bounds the wait for the challenge env to show in the project
// search before it is deleted: the search trails a write by up to ~1.6 s
// (T0 §1), while HQ's direct env-file read sees it at once.
const envPolls = 40

// mismatchTries bounds the credential call while HQ does not see the nonce
// yet; a mismatch does not spend it.
const mismatchTries = 3

// Zerops is what enrollment asks of the platform, with this container's own
// key: the org's member list, and its own project's env.
type Zerops interface {
	ListOrgMembers(ctx context.Context, orgID string) ([]platform.OrgMember, error)
	GetProjectEnv(ctx context.Context, projectID string) ([]platform.ProjectEnvVar, error)
	CreateProjectEnv(ctx context.Context, projectID, key, content string, sensitive bool) (*platform.Process, error)
	DeleteProjectEnv(ctx context.Context, envID string) (*platform.Process, error)
}

// NoHQError is the member list naming no single official HQ: nothing to
// enroll with now; a later try reads the list again.
type NoHQError struct{ Official Official }

func (e *NoHQError) Error() string {
	return fmt.Sprintf("no official HQ (%s)", e.Official.Verdict)
}

// Enroller enrolls this container's Mate with its org's official HQ.
type Enroller struct {
	Zerops    Zerops
	HTTP      *http.Client
	OrgID     string
	ProjectID string
	// Path is the enrollment file (EnrollmentPath in a container).
	Path string
	// Poll paces the retries; zero is 500 ms.
	Poll time.Duration
}

// Result is an enrollment's outcome: the HQ, and whether this run issued a
// new credential.
type Result struct {
	HQ      string `json:"hq"`
	Changed bool   `json:"changed"`
}

// Enroll proves the Mate's project to the official HQ and keeps the
// credential HQ issues: HQ hands out a challenge, this container writes its
// nonce into its own project's env with its own key, HQ reads it back with
// its own. The env variable is removed again whatever HQ answered. A
// credential HQ still knows is kept as it is.
func (e Enroller) Enroll(ctx context.Context) (Result, error) {
	official, err := e.official(ctx)
	if err != nil {
		return Result{}, err
	}
	hq := hqClient{http: e.HTTP, address: official.Address}
	known, err := e.known(ctx, hq)
	if err != nil {
		return Result{}, err
	}
	if known {
		return Result{HQ: official.Address}, nil
	}
	if err := e.removeChallenge(ctx, 1); err != nil {
		return Result{}, err
	}
	nonce, err := hq.challenge(ctx, e.ProjectID)
	if err != nil {
		return Result{}, err
	}
	if _, err := e.Zerops.CreateProjectEnv(ctx, e.ProjectID, ChallengeEnv, nonce, false); err != nil {
		return Result{}, fmt.Errorf("write %s: %w", ChallengeEnv, err)
	}
	credential, err := e.present(ctx, hq, nonce)
	removeErr := e.removeChallenge(context.WithoutCancel(ctx), envPolls)
	if err != nil {
		return Result{}, errors.Join(err, removeErr)
	}
	if err := SaveEnrollment(e.Path, Enrollment{
		HQ: official.Address, HQProjectID: official.ProjectID, ProjectID: e.ProjectID, Credential: credential,
	}); err != nil {
		return Result{}, errors.Join(err, removeErr)
	}
	return Result{HQ: official.Address, Changed: true}, removeErr
}

// Status is what this container knows of its HQ: the official one, and
// whether HQ knows a credential this Mate holds. It never carries the
// credential.
type Status struct {
	HQ       Official `json:"hq"`
	Enrolled bool     `json:"enrolled"`
	Error    string   `json:"error,omitempty"`
}

// Status reads the member list and asks HQ about the kept credential; a read
// that fails is Status.Error, never a guess.
func (e Enroller) Status(ctx context.Context) Status {
	official, err := e.official(ctx)
	var noHQ *NoHQError
	if errors.As(err, &noHQ) {
		return Status{HQ: noHQ.Official}
	}
	if err != nil {
		return Status{HQ: Official{Verdict: VerdictUnknown}, Error: err.Error()}
	}
	known, err := e.known(ctx, hqClient{http: e.HTTP, address: official.Address})
	if err != nil {
		return Status{HQ: official, Error: err.Error()}
	}
	return Status{HQ: official, Enrolled: known}
}

// present presents the nonce until HQ sees it in the project's env, at most
// mismatchTries times.
func (e Enroller) present(ctx context.Context, hq hqClient, nonce string) (string, error) {
	for try := 1; ; try++ {
		credential, err := hq.credential(ctx, e.ProjectID, nonce)
		if err == nil || try == mismatchTries || !refusedAs(err, "env_mismatch") {
			return credential, err
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("present challenge: %w", ctx.Err())
		case <-time.After(e.poll()):
		}
	}
}

// known reports whether the kept enrollment is with hq and HQ still knows
// its credential as this project's.
func (e Enroller) known(ctx context.Context, hq hqClient) (bool, error) {
	kept, found, err := LoadEnrollment(e.Path)
	if err != nil || !found || kept.HQ != hq.address || kept.ProjectID != e.ProjectID {
		return false, err
	}
	projectID, err := hq.whoami(ctx, kept.Credential)
	if refusedAs(err, "mate_credential_required") {
		return false, nil
	}
	return err == nil && projectID == e.ProjectID, err
}

// official is the org's official HQ, or a NoHQError.
func (e Enroller) official(ctx context.Context) (Official, error) {
	members, err := e.Zerops.ListOrgMembers(ctx, e.OrgID)
	if err != nil {
		return Official{}, fmt.Errorf("read org members: %w", err)
	}
	official := FindOfficial(members)
	if official.Verdict != VerdictOfficial {
		return Official{}, &NoHQError{Official: official}
	}
	return official, nil
}

// removeChallenge deletes every ChallengeEnv of the project, reading the
// project search up to polls times until one shows.
func (e Enroller) removeChallenge(ctx context.Context, polls int) error {
	for poll := range polls {
		if poll > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("remove %s: %w", ChallengeEnv, ctx.Err())
			case <-time.After(e.poll()):
			}
		}
		env, err := e.Zerops.GetProjectEnv(ctx, e.ProjectID)
		if err != nil {
			return fmt.Errorf("read project env: %w", err)
		}
		found := false
		for _, v := range env {
			if v.Key != ChallengeEnv {
				continue
			}
			found = true
			if _, err := e.Zerops.DeleteProjectEnv(ctx, v.ID); err != nil {
				return fmt.Errorf("remove %s: %w", ChallengeEnv, err)
			}
		}
		if found {
			return nil
		}
	}
	return nil
}

func (e Enroller) poll() time.Duration {
	if e.Poll == 0 {
		return 500 * time.Millisecond
	}
	return e.Poll
}
