package ops

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"time"
)

// WorkspaceManifest is a structured snapshot of the recipe workspace that
// subagents consult instead of crawling the filesystem. v8.94 §5.8: every
// subagent dispatch (scaffold ×3, feature, code-review, content-authoring)
// previously started with empty context and re-read the same 30 files to
// orient. The manifest replaces those crawls with one JSON read.
//
// Main agent is the authoritative writer — subagents return structured data
// in their completion message; main applies an update. This matches the v8.90
// "workflow state is main-agent-only" policy (see recipe_substep_briefs_test.go).
type WorkspaceManifest struct {
	SessionID           string                     `json:"sessionId"`
	PlanSlug            string                     `json:"planSlug,omitempty"`
	LastUpdated         string                     `json:"lastUpdated"`
	Codebases           map[string]*CodebaseInfo   `json:"codebases,omitempty"`
	Contracts           *ContractInfo              `json:"contracts,omitempty"`
	FeaturesImplemented []FeatureRecord            `json:"featuresImplemented,omitempty"`
	Notes               map[string]json.RawMessage `json:"notes,omitempty"`
}

// CodebaseInfo captures what a single codebase's mount looks like after
// scaffold + feature work. Written by the main agent from each subagent's
// structured return. Kept small — only fields subsequent subagents need to
// avoid re-crawling.
type CodebaseInfo struct {
	Framework           string          `json:"framework,omitempty"`
	Runtime             string          `json:"runtime,omitempty"`
	ScaffoldCompletedAt string          `json:"scaffoldCompletedAt,omitempty"`
	SourceFiles         []SourceFile    `json:"sourceFiles,omitempty"`
	ZeropsYAML          *ZeropsYAMLInfo `json:"zeropsYaml,omitempty"`
	PreFlightChecks     *PreFlightInfo  `json:"preFlightChecks,omitempty"`
}

// SourceFile describes one source entry: path, purpose, exports. Purpose is
// a short human string written by the scaffold subagent based on what it
// actually wrote. Exports is a free-form list of top-level symbols (module
// exports, class names, etc).
type SourceFile struct {
	Path    string   `json:"path"`
	Purpose string   `json:"purpose,omitempty"`
	Exports []string `json:"exports,omitempty"`
}

// ZeropsYAMLInfo summarizes a codebase's zerops.yaml without requiring a
// re-parse. Captures what setups are declared and which managed services the
// run block wires to via cross-service env refs.
type ZeropsYAMLInfo struct {
	Path                 string   `json:"path,omitempty"`
	Setups               []string `json:"setups,omitempty"`
	ManagedServicesWired []string `json:"managedServicesWired,omitempty"`
	HasInitCommands      bool     `json:"hasInitCommands,omitempty"`
	ExposesHTTP          bool     `json:"exposesHttp,omitempty"`
	HTTPPort             int      `json:"httpPort,omitempty"`
}

// PreFlightInfo records pre-ship self-verification results. Each assertion
// name matches the 9 assertions in the scaffold-subagent-brief §"Pre-ship
// self-verification" block. Scaffold subagents populate this at return time.
type PreFlightInfo struct {
	Passed []string `json:"passed,omitempty"`
	Failed []string `json:"failed,omitempty"`
}

// ContractInfo captures cross-codebase shape-bindings the feature subagent
// established — queue groups, response shapes, shared entity ownership.
// Free-form maps because the bindings vary per recipe.
type ContractInfo struct {
	NATSSubjects       map[string]json.RawMessage `json:"natsSubjects,omitempty"`
	HTTPResponseShapes map[string]string          `json:"httpResponseShapes,omitempty"`
	SharedEntities     map[string]json.RawMessage `json:"sharedEntities,omitempty"`
}

// FeatureRecord pins one feature's implementation to the codebases it
// touches, with a completion timestamp. Content-authoring subagent uses it
// to attribute classification-map facts correctly.
type FeatureRecord struct {
	ID      string   `json:"id"`
	Touches []string `json:"touches,omitempty"`
	At      string   `json:"at,omitempty"`
}

// WorkspaceManifestPath returns the canonical manifest path for a session.
// Lives in /tmp — same convention as FactLogPath — because it's transient
// to a single recipe run. Post-run forensics survive via session logs.
func WorkspaceManifestPath(sessionID string) string {
	return filepath.Join(os.TempDir(), "zcp-workspace-"+sessionID+".json")
}

// ReadWorkspaceManifest loads the manifest at path, or returns a zero-value
// skeleton when the file doesn't exist yet. The skeleton is caller-visible
// so the first reader gets a well-formed document to patch rather than nil.
func ReadWorkspaceManifest(path, sessionID string) (*WorkspaceManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &WorkspaceManifest{
				SessionID:   sessionID,
				LastUpdated: time.Now().UTC().Format(time.RFC3339),
				Codebases:   map[string]*CodebaseInfo{},
			}, nil
		}
		return nil, fmt.Errorf("read workspace manifest: %w", err)
	}
	var m WorkspaceManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse workspace manifest: %w", err)
	}
	if m.Codebases == nil {
		m.Codebases = map[string]*CodebaseInfo{}
	}
	return &m, nil
}

// WriteWorkspaceManifest serializes the manifest to path atomically
// (write-temp → rename). Sets LastUpdated before write so concurrent readers
// have a fresh timestamp when they next load.
func WriteWorkspaceManifest(path string, m *WorkspaceManifest) error {
	if m == nil {
		return fmt.Errorf("workspace manifest: nil")
	}
	m.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace manifest: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write workspace manifest temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("finalize workspace manifest: %w", err)
	}
	return nil
}

// WorkspaceManifestUpdate is a partial patch applied to the on-disk
// manifest. Non-empty fields overwrite the corresponding manifest entries;
// Codebases entries merge per-hostname so a feature subagent can update its
// codebase without clobbering sibling codebases. Contracts and
// FeaturesImplemented are accumulate-only (the updater appends rather than
// replacing) because they represent cross-codebase outcomes recorded over
// multiple substeps.
type WorkspaceManifestUpdate struct {
	PlanSlug            string                     `json:"planSlug,omitempty"`
	Codebases           map[string]*CodebaseInfo   `json:"codebases,omitempty"`
	Contracts           *ContractInfo              `json:"contracts,omitempty"`
	FeaturesImplemented []FeatureRecord            `json:"featuresImplemented,omitempty"`
	Notes               map[string]json.RawMessage `json:"notes,omitempty"`
}

// ApplyWorkspaceManifestUpdate loads the manifest at path, merges the update,
// and writes it back. Codebases entries are replaced whole per-hostname (a
// subagent returning updated info on its own codebase overwrites prior info
// for that hostname; sibling codebases stay). Contracts is replaced entire
// when non-nil (contracts come from the feature subagent as one coherent set;
// partial updates don't make sense). FeaturesImplemented is appended.
func ApplyWorkspaceManifestUpdate(path, sessionID string, update WorkspaceManifestUpdate) (*WorkspaceManifest, error) {
	m, err := ReadWorkspaceManifest(path, sessionID)
	if err != nil {
		return nil, err
	}
	if update.PlanSlug != "" {
		m.PlanSlug = update.PlanSlug
	}
	for hostname, info := range update.Codebases {
		if info == nil {
			delete(m.Codebases, hostname)
			continue
		}
		m.Codebases[hostname] = info
	}
	if update.Contracts != nil {
		m.Contracts = update.Contracts
	}
	m.FeaturesImplemented = append(m.FeaturesImplemented, update.FeaturesImplemented...)
	if len(update.Notes) > 0 {
		if m.Notes == nil {
			m.Notes = make(map[string]json.RawMessage, len(update.Notes))
		}
		maps.Copy(m.Notes, update.Notes)
	}
	if err := WriteWorkspaceManifest(path, m); err != nil {
		return nil, err
	}
	return m, nil
}
