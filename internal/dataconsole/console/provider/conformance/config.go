package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// Environment variables the live run reads. Named as constants so config.go
// and the e2e-tagged harness agree on exactly one spelling.
const (
	EnvConfig   = "DC_LIVE_CONFIG"   // path to the gitignored live-services JSON
	EnvProfile  = "DC_LIVE_PROFILE"  // "partial" (default) | "full"
	EnvManifest = "DC_LIVE_MANIFEST" // comma-separated hostnames required under "full"
)

// SQLDescriptor mirrors provider.SQLConn's wire shape for the tabular family
// (postgresql, mariadb, mysql, clickhouse). Driver/Dialect may be left empty —
// Descriptor defaults Dialect from the service Type, exactly like the platform
// classifier; tabular.New derives Driver from Dialect.
type SQLDescriptor struct {
	Driver   string `json:"driver,omitempty"`
	Dialect  string `json:"dialect,omitempty"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
}

// KVDescriptor mirrors provider.KVConn (valkey).
type KVDescriptor struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Password string `json:"password"`
}

// ObjectDescriptor mirrors provider.ObjectConn (S3-compatible object storage).
type ObjectDescriptor struct {
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Bucket    string `json:"bucket"`
	Secure    bool   `json:"secure"`
}

// DocumentDescriptor mirrors provider.DocumentConn (elasticsearch, meilisearch,
// typesense, qdrant). Engine may be left empty — Descriptor defaults it from
// the service Type.
type DocumentDescriptor struct {
	Engine  string `json:"engine,omitempty"`
	BaseURL string `json:"baseUrl"`
	User    string `json:"user,omitempty"`
	APIKey  string `json:"apiKey,omitempty"`
}

// StreamDescriptor mirrors provider.StreamConn (kafka, nats). Engine may be
// left empty — Descriptor defaults it from the service Type.
type StreamDescriptor struct {
	Engine   string `json:"engine,omitempty"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

// ServiceEntry is one configured live service: its Zerops hostname + service
// type (the classification discriminator — provider.Classify(Type) picks the
// family exactly like the real adapter does) + the family-specific descriptor
// block matching that classification. Exactly one of Tabular/KV/Object/
// Document/Stream is read, chosen by Family().
type ServiceEntry struct {
	Hostname string `json:"hostname"`
	Type     string `json:"type"`

	Tabular  *SQLDescriptor      `json:"tabular,omitempty"`
	KV       *KVDescriptor       `json:"kv,omitempty"`
	Object   *ObjectDescriptor   `json:"object,omitempty"`
	Document *DocumentDescriptor `json:"document,omitempty"`
	Stream   *StreamDescriptor   `json:"stream,omitempty"`
}

// Family classifies the entry's service Type exactly like the platform
// adapter does — the config file never carries a redundant, independently-
// wrong "family" field.
func (e ServiceEntry) Family() provider.Family {
	return provider.Classify(e.Type)
}

// Descriptor builds the typed provider.ConnectionDescriptor for this entry,
// dispatched by Family(). It is called once at load time (so a malformed
// config fails at LoadLiveConfig, not deep inside a live test) and again by
// the live harness immediately before constructing the real provider.
func (e ServiceEntry) Descriptor() (provider.ConnectionDescriptor, error) {
	switch e.Family() {
	case provider.FamilyTabular:
		if e.Tabular == nil {
			return nil, fmt.Errorf("type %q classifies tabular but carries no \"tabular\" descriptor block", e.Type)
		}
		if strings.TrimSpace(e.Tabular.Host) == "" {
			return nil, fmt.Errorf("tabular descriptor: host required")
		}
		dialect := e.Tabular.Dialect
		if dialect == "" {
			dialect = provider.BaseType(e.Type)
		}
		return provider.SQLConn{
			Driver: e.Tabular.Driver, Dialect: dialect,
			Host: e.Tabular.Host, Port: e.Tabular.Port,
			User: e.Tabular.User, Password: e.Tabular.Password,
			Database: e.Tabular.Database,
		}, nil
	case provider.FamilyKV:
		if e.KV == nil {
			return nil, fmt.Errorf("type %q classifies kv but carries no \"kv\" descriptor block", e.Type)
		}
		if strings.TrimSpace(e.KV.Host) == "" {
			return nil, fmt.Errorf("kv descriptor: host required")
		}
		return provider.KVConn{Host: e.KV.Host, Port: e.KV.Port, Password: e.KV.Password}, nil
	case provider.FamilyObject:
		if e.Object == nil {
			return nil, fmt.Errorf("type %q classifies object but carries no \"object\" descriptor block", e.Type)
		}
		if strings.TrimSpace(e.Object.Endpoint) == "" || strings.TrimSpace(e.Object.Bucket) == "" {
			return nil, fmt.Errorf("object descriptor: endpoint and bucket required")
		}
		return provider.ObjectConn{
			Endpoint: e.Object.Endpoint, AccessKey: e.Object.AccessKey,
			SecretKey: e.Object.SecretKey, Bucket: e.Object.Bucket, Secure: e.Object.Secure,
		}, nil
	case provider.FamilyDocument:
		if e.Document == nil {
			return nil, fmt.Errorf("type %q classifies document but carries no \"document\" descriptor block", e.Type)
		}
		if strings.TrimSpace(e.Document.BaseURL) == "" {
			return nil, fmt.Errorf("document descriptor: baseUrl required")
		}
		engine := e.Document.Engine
		if engine == "" {
			engine = provider.BaseType(e.Type)
		}
		return provider.DocumentConn{
			Engine: engine, BaseURL: e.Document.BaseURL,
			User: e.Document.User, APIKey: e.Document.APIKey,
		}, nil
	case provider.FamilyStream:
		if e.Stream == nil {
			return nil, fmt.Errorf("type %q classifies stream but carries no \"stream\" descriptor block", e.Type)
		}
		if strings.TrimSpace(e.Stream.Host) == "" {
			return nil, fmt.Errorf("stream descriptor: host required")
		}
		engine := e.Stream.Engine
		if engine == "" {
			engine = provider.BaseType(e.Type)
		}
		return provider.StreamConn{
			Engine: engine, Host: e.Stream.Host, Port: e.Stream.Port,
			User: e.Stream.User, Password: e.Stream.Password,
		}, nil
	case provider.FamilyFile, provider.FamilyUnknown:
		// no console provider for either — falls through to the shared error below.
	}
	return nil, fmt.Errorf("type %q does not classify to a supported data-console family", e.Type)
}

// LiveConfig is the decoded DC_LIVE_CONFIG document: the live services a
// conformance run may drive.
type LiveConfig struct {
	Services []ServiceEntry `json:"services"`
}

// Lookup finds a configured service by hostname.
func (c *LiveConfig) Lookup(hostname string) (ServiceEntry, bool) {
	if c == nil {
		return ServiceEntry{}, false
	}
	for _, e := range c.Services {
		if e.Hostname == hostname {
			return e, true
		}
	}
	return ServiceEntry{}, false
}

// ByFamily returns every configured service classifying to family, in
// DC_LIVE_CONFIG order.
func (c *LiveConfig) ByFamily(family provider.Family) []ServiceEntry {
	if c == nil {
		return nil
	}
	var out []ServiceEntry
	for _, e := range c.Services {
		if e.Family() == family {
			out = append(out, e)
		}
	}
	return out
}

// validate checks structural integrity — unique non-empty hostnames, a
// non-empty type, and a Family-matching descriptor block for every entry —
// so a config typo fails LOUDLY at load time instead of surfacing as a
// mysterious dial failure deep inside a live test.
func (c *LiveConfig) validate() error {
	seen := make(map[string]bool, len(c.Services))
	for i, e := range c.Services {
		if strings.TrimSpace(e.Hostname) == "" {
			return fmt.Errorf("service[%d]: hostname required", i)
		}
		if seen[e.Hostname] {
			return fmt.Errorf("service[%d] %q: duplicate hostname", i, e.Hostname)
		}
		seen[e.Hostname] = true
		if strings.TrimSpace(e.Type) == "" {
			return fmt.Errorf("service %q: type required", e.Hostname)
		}
		if _, err := e.Descriptor(); err != nil {
			return fmt.Errorf("service %q: %w", e.Hostname, err)
		}
	}
	return nil
}

// LoadLiveConfig reads + validates a DC_LIVE_CONFIG file.
func LoadLiveConfig(path string) (*LiveConfig, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is an operator-supplied env var, not request input
	if err != nil {
		return nil, fmt.Errorf("conformance: read %s: %w", path, err)
	}
	var cfg LiveConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("conformance: parse %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("conformance: %s: %w", path, err)
	}
	return &cfg, nil
}

// LoadFromEnv reads DC_LIVE_CONFIG. An unset/empty env var is NOT an error —
// it yields an empty config (every test skips-with-reason under the partial
// profile, which is the correct behavior for "no live tier configured").
func LoadFromEnv() (*LiveConfig, error) {
	path := os.Getenv(EnvConfig)
	if path == "" {
		return &LiveConfig{}, nil
	}
	return LoadLiveConfig(path)
}

// Profile is the live run's strictness — see doc.go.
type Profile string

const (
	ProfilePartial Profile = "partial"
	ProfileFull    Profile = "full"
)

// ParseProfile validates a DC_LIVE_PROFILE value (case-insensitive, trimmed);
// empty defaults to partial.
func ParseProfile(raw string) (Profile, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(ProfilePartial):
		return ProfilePartial, nil
	case string(ProfileFull):
		return ProfileFull, nil
	default:
		return "", fmt.Errorf("conformance: invalid %s %q: want %q or %q", EnvProfile, raw, ProfilePartial, ProfileFull)
	}
}

// ProfileFromEnv reads DC_LIVE_PROFILE.
func ProfileFromEnv() (Profile, error) {
	return ParseProfile(os.Getenv(EnvProfile))
}

// ParseManifest splits a comma-separated hostname list, trimming whitespace
// and dropping empty entries. An empty/whitespace-only input yields nil.
func ParseManifest(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ManifestFromEnv reads DC_LIVE_MANIFEST.
func ManifestFromEnv() []string {
	return ParseManifest(os.Getenv(EnvManifest))
}

// RequiredByManifest reports whether hostname's absence/unreachability/
// failure must FAIL the run rather than skip-with-reason: only under the full
// profile, and only for a hostname the release manifest actually names. Pure
// decision function — the live harness applies it, but every branch is
// exercised offline (see config_test.go).
func RequiredByManifest(profile Profile, manifest []string, hostname string) bool {
	if profile != ProfileFull {
		return false
	}
	return slices.Contains(manifest, hostname)
}
