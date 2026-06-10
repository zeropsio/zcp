package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Verified-setup evidence — the durable extract of "this setup was
// green-verified, when" (F4 ledger completion). The work session's
// Deploys/Verifies maps are per-PID and deleted at close/prune, so before
// this file existed ZCP could not answer "was the stage setup ever
// verified" once a session ended — the question the dev→prod transition
// asks. One sidecar JSON per pair (keyed like the meta file, canonical
// dev-half hostname), latest evidence per setup name.
//
// Evidence is a RECORD with a timestamp, rendered as "verified at T",
// never as current truth (the stamped/derived boundary: live state is
// re-derived; what is no longer derivable after the session dies is
// stamped with its observation time).

// VerifiedSetupEvidence is the latest green-verify observation for one
// zerops.yaml setup name on a pair.
type VerifiedSetupEvidence struct {
	// SetupName is the zerops.yaml setup-block identity the verified
	// deploy used (meta cascade value at verify time).
	SetupName string `json:"setupName"`
	// TargetHostname is the runtime the verify ran against (dev or stage
	// half — the build target of the verified deploy).
	TargetHostname string `json:"targetHostname"`
	// VerifiedAt is the RFC3339 time the verify passed.
	VerifiedAt string `json:"verifiedAt"`
	// Summary echoes the verify outcome line ("healthy", ...).
	Summary string `json:"summary,omitempty"`
}

// verifiedEvidencePath returns the sidecar path for a pair's canonical
// hostname. Lives next to the meta file under services/.
func verifiedEvidencePath(stateDir, pairHostname string) string {
	return filepath.Join(stateDir, "services", pairHostname+".verified.json")
}

// RecordVerifiedSetup persists the latest evidence for (pair, setup).
// hostname may be either half — resolved pair-keyed via FindServiceMeta;
// unknown hostnames no-op (no meta = nothing to anchor the record to).
// Write is atomic (temp + rename) and idempotent per setup (latest wins).
func RecordVerifiedSetup(stateDir, hostname string, ev VerifiedSetupEvidence) error {
	if ev.SetupName == "" {
		return nil // no setup identity — nothing durable to record
	}
	meta, err := FindServiceMeta(stateDir, hostname)
	if err != nil || meta == nil {
		return err
	}
	path := verifiedEvidencePath(stateDir, meta.Hostname)
	evidence, err := readVerifiedEvidenceFile(path)
	if err != nil {
		return err
	}
	if evidence == nil {
		evidence = map[string]VerifiedSetupEvidence{}
	}
	evidence[ev.SetupName] = ev

	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal verified evidence: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir services dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write verified evidence: %w", err)
	}
	return os.Rename(tmp, path)
}

// ReadVerifiedSetups returns the per-setup latest evidence for the pair
// the hostname belongs to. Missing file or missing meta yields an empty
// map — callers render "never verified" honestly.
func ReadVerifiedSetups(stateDir, hostname string) (map[string]VerifiedSetupEvidence, error) {
	meta, err := FindServiceMeta(stateDir, hostname)
	if err != nil || meta == nil {
		return map[string]VerifiedSetupEvidence{}, err
	}
	evidence, err := readVerifiedEvidenceFile(verifiedEvidencePath(stateDir, meta.Hostname))
	if err != nil {
		return nil, err
	}
	if evidence == nil {
		return map[string]VerifiedSetupEvidence{}, nil
	}
	return evidence, nil
}

func readVerifiedEvidenceFile(path string) (map[string]VerifiedSetupEvidence, error) {
	empty := map[string]VerifiedSetupEvidence{}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return empty, nil // absent file = no evidence yet (non-nil value)
		}
		return nil, fmt.Errorf("read verified evidence: %w", err)
	}
	// Corrupt sidecar degrades to empty by design (evidence is advisory; a
	// re-verify rebuilds it) — the parse error is intentionally not
	// propagated, so it is not bound to a checked variable.
	out := map[string]VerifiedSetupEvidence{}
	_ = json.Unmarshal(data, &out)
	return out, nil
}
