package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// ---- LoadLiveConfig: JSON parsing + structural validation, no network ----

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "live-config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLoadLiveConfig_ValidAllFamilies_Decodes(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, `{
		"services": [
			{"hostname": "db", "type": "postgresql",
			 "tabular": {"host": "db", "port": "5432", "user": "u", "password": "p", "database": "d"}},
			{"hostname": "cache", "type": "valkey",
			 "kv": {"host": "cache", "port": "6379", "password": "p"}},
			{"hostname": "storage", "type": "object-storage",
			 "object": {"endpoint": "s3.local", "accessKey": "ak", "secretKey": "sk", "bucket": "b", "secure": true}},
			{"hostname": "es", "type": "elasticsearch",
			 "document": {"baseUrl": "http://es:9200", "user": "elastic", "apiKey": "k"}},
			{"hostname": "queue", "type": "kafka",
			 "stream": {"host": "queue", "port": "9092"}}
		]
	}`)
	cfg, err := LoadLiveConfig(path)
	if err != nil {
		t.Fatalf("LoadLiveConfig: %v", err)
	}
	if len(cfg.Services) != 5 {
		t.Fatalf("Services = %d, want 5", len(cfg.Services))
	}

	wantFamily := map[string]provider.Family{
		"db": provider.FamilyTabular, "cache": provider.FamilyKV, "storage": provider.FamilyObject,
		"es": provider.FamilyDocument, "queue": provider.FamilyStream,
	}
	for _, e := range cfg.Services {
		if got := e.Family(); got != wantFamily[e.Hostname] {
			t.Errorf("%s: Family() = %q, want %q", e.Hostname, got, wantFamily[e.Hostname])
		}
		if _, err := e.Descriptor(); err != nil {
			t.Errorf("%s: Descriptor(): %v", e.Hostname, err)
		}
	}

	db, ok := cfg.Lookup("db")
	if !ok {
		t.Fatal("Lookup(db): not found")
	}
	desc, err := db.Descriptor()
	if err != nil {
		t.Fatalf("db.Descriptor(): %v", err)
	}
	sql, ok := desc.(provider.SQLConn)
	if !ok {
		t.Fatalf("db.Descriptor() = %T, want provider.SQLConn", desc)
	}
	if sql.Host != "db" || sql.Port != "5432" || sql.Database != "d" {
		t.Errorf("SQLConn fields didn't round-trip: %+v", sql)
	}
}

func TestLoadLiveConfig_MissingFile_Errors(t *testing.T) {
	t.Parallel()
	if _, err := LoadLiveConfig(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("want error for a missing file, got nil")
	}
}

func TestLoadLiveConfig_MalformedJSON_Errors(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, `{ not valid json`)
	if _, err := LoadLiveConfig(path); err == nil {
		t.Fatal("want error for malformed JSON, got nil")
	}
}

func TestLoadLiveConfig_MissingHostname_Errors(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, `{"services":[{"type":"postgresql","tabular":{"host":"db","port":"5432"}}]}`)
	if _, err := LoadLiveConfig(path); err == nil {
		t.Fatal("want error for a missing hostname, got nil")
	}
}

func TestLoadLiveConfig_DuplicateHostname_Errors(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, `{"services":[
		{"hostname":"db","type":"postgresql","tabular":{"host":"db","port":"5432"}},
		{"hostname":"db","type":"valkey","kv":{"host":"db","port":"6379"}}
	]}`)
	if _, err := LoadLiveConfig(path); err == nil {
		t.Fatal("want error for a duplicate hostname, got nil")
	}
}

func TestLoadLiveConfig_MissingType_Errors(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, `{"services":[{"hostname":"db","tabular":{"host":"db","port":"5432"}}]}`)
	if _, err := LoadLiveConfig(path); err == nil {
		t.Fatal("want error for a missing type, got nil")
	}
}

func TestLoadLiveConfig_ClassifiedFamilyMissingBlock_Errors(t *testing.T) {
	t.Parallel()
	// "postgresql" classifies tabular, but the config carries a "kv" block
	// instead of "tabular" — must fail loudly at load time, not as a
	// mysterious dial failure deep inside a live test.
	path := writeConfig(t, `{"services":[{"hostname":"db","type":"postgresql","kv":{"host":"db","port":"5432"}}]}`)
	if _, err := LoadLiveConfig(path); err == nil {
		t.Fatal("want error when the descriptor block doesn't match the classified family, got nil")
	}
}

func TestLoadLiveConfig_UnknownType_Errors(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, `{"services":[{"hostname":"mystery","type":"some-future-engine"}]}`)
	if _, err := LoadLiveConfig(path); err == nil {
		t.Fatal("want error for a type that classifies to FamilyUnknown, got nil")
	}
}

func TestLoadLiveConfig_RequiredFieldMissingInBlock_Errors(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"services":[{"hostname":"db","type":"postgresql","tabular":{"port":"5432"}}]}`,           // no host
		`{"services":[{"hostname":"cache","type":"valkey","kv":{"port":"6379"}}]}`,                 // no host
		`{"services":[{"hostname":"storage","type":"object-storage","object":{"bucket":"b"}}]}`,    // no endpoint
		`{"services":[{"hostname":"storage","type":"object-storage","object":{"endpoint":"s3"}}]}`, // no bucket
		`{"services":[{"hostname":"es","type":"elasticsearch","document":{"user":"elastic"}}]}`,    // no baseUrl
		`{"services":[{"hostname":"queue","type":"kafka","stream":{"port":"9092"}}]}`,              // no host
	}
	for _, body := range cases {
		if _, err := LoadLiveConfig(writeConfig(t, body)); err == nil {
			t.Errorf("body %s: want error for missing required field, got nil", body)
		}
	}
}

// ---- ServiceEntry.Descriptor: defaulting from Type ----

func TestServiceEntry_Descriptor_DefaultsDialectFromType(t *testing.T) {
	t.Parallel()
	e := ServiceEntry{
		Hostname: "db", Type: "mariadb",
		Tabular: &SQLDescriptor{Host: "db", Port: "3306"},
	}
	desc, err := e.Descriptor()
	if err != nil {
		t.Fatalf("Descriptor: %v", err)
	}
	sql := desc.(provider.SQLConn)
	if sql.Dialect != "mariadb" {
		t.Errorf("Dialect = %q, want %q (defaulted from Type)", sql.Dialect, "mariadb")
	}
}

func TestServiceEntry_Descriptor_ExplicitDialectWins(t *testing.T) {
	t.Parallel()
	e := ServiceEntry{
		Hostname: "db", Type: "mariadb",
		Tabular: &SQLDescriptor{Host: "db", Port: "3306", Dialect: "mysql"},
	}
	desc, err := e.Descriptor()
	if err != nil {
		t.Fatalf("Descriptor: %v", err)
	}
	if sql := desc.(provider.SQLConn); sql.Dialect != "mysql" {
		t.Errorf("Dialect = %q, want explicit %q to win over the Type default", sql.Dialect, "mysql")
	}
}

func TestServiceEntry_Descriptor_DefaultsEngineFromType(t *testing.T) {
	t.Parallel()
	doc := ServiceEntry{Hostname: "es", Type: "elasticsearch", Document: &DocumentDescriptor{BaseURL: "http://es:9200"}}
	desc, err := doc.Descriptor()
	if err != nil {
		t.Fatalf("Descriptor: %v", err)
	}
	if d := desc.(provider.DocumentConn); d.Engine != "elasticsearch" {
		t.Errorf("document Engine = %q, want %q (defaulted from Type)", d.Engine, "elasticsearch")
	}

	strm := ServiceEntry{Hostname: "queue", Type: "nats", Stream: &StreamDescriptor{Host: "queue", Port: "4222"}}
	desc, err = strm.Descriptor()
	if err != nil {
		t.Fatalf("Descriptor: %v", err)
	}
	if s := desc.(provider.StreamConn); s.Engine != "nats" {
		t.Errorf("stream Engine = %q, want %q (defaulted from Type)", s.Engine, "nats")
	}
}

func TestServiceEntry_Descriptor_UnclassifiedType_Errors(t *testing.T) {
	t.Parallel()
	e := ServiceEntry{Hostname: "x", Type: "shared-storage"} // FamilyFile — no console provider
	if _, err := e.Descriptor(); err == nil {
		t.Fatal("want error for a family with no supported descriptor, got nil")
	}
}

// ---- LiveConfig.Lookup / ByFamily ----

func TestLiveConfig_Lookup_HitAndMiss(t *testing.T) {
	t.Parallel()
	cfg := &LiveConfig{Services: []ServiceEntry{
		{Hostname: "db", Type: "postgresql", Tabular: &SQLDescriptor{Host: "db", Port: "5432"}},
	}}
	if _, ok := cfg.Lookup("db"); !ok {
		t.Error("Lookup(db): want hit")
	}
	if _, ok := cfg.Lookup("nope"); ok {
		t.Error("Lookup(nope): want miss")
	}
	var nilCfg *LiveConfig
	if _, ok := nilCfg.Lookup("db"); ok {
		t.Error("Lookup on a nil *LiveConfig: want miss, not a panic/hit")
	}
}

func TestLiveConfig_ByFamily_FiltersAndPreservesOrder(t *testing.T) {
	t.Parallel()
	cfg := &LiveConfig{Services: []ServiceEntry{
		{Hostname: "mysql1", Type: "mysql", Tabular: &SQLDescriptor{Host: "a", Port: "3306"}},
		{Hostname: "cache", Type: "valkey", KV: &KVDescriptor{Host: "b", Port: "6379"}},
		{Hostname: "pg1", Type: "postgresql", Tabular: &SQLDescriptor{Host: "c", Port: "5432"}},
	}}
	got := cfg.ByFamily(provider.FamilyTabular)
	if len(got) != 2 || got[0].Hostname != "mysql1" || got[1].Hostname != "pg1" {
		t.Fatalf("ByFamily(tabular) = %+v, want [mysql1, pg1] in config order", got)
	}
	if got := cfg.ByFamily(provider.FamilyStream); len(got) != 0 {
		t.Errorf("ByFamily(stream) = %+v, want empty", got)
	}
	var nilCfg *LiveConfig
	if got := nilCfg.ByFamily(provider.FamilyTabular); got != nil {
		t.Errorf("ByFamily on a nil *LiveConfig: want nil, got %+v", got)
	}
}

// ---- LoadFromEnv ----

func TestLoadFromEnv_Unset_ReturnsEmptyConfigNotError(t *testing.T) {
	t.Setenv(EnvConfig, "")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv with unset DC_LIVE_CONFIG: %v", err)
	}
	if len(cfg.Services) != 0 {
		t.Errorf("Services = %+v, want empty", cfg.Services)
	}
}

func TestLoadFromEnv_SetToMissingFile_Errors(t *testing.T) {
	t.Setenv(EnvConfig, filepath.Join(t.TempDir(), "nope.json"))
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("want error when DC_LIVE_CONFIG points at a missing file")
	}
}

// ---- Profile parsing ----

func TestParseProfile_ValidatesAndDefaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw     string
		want    Profile
		wantErr bool
	}{
		{"", ProfilePartial, false},
		{"partial", ProfilePartial, false},
		{"  partial  ", ProfilePartial, false},
		{"full", ProfileFull, false},
		{"FULL", ProfileFull, false},
		{"bogus", "", true},
	}
	for _, c := range cases {
		got, err := ParseProfile(c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseProfile(%q): want error, got nil", c.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseProfile(%q): %v", c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseProfile(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestProfileFromEnv_ReadsEnv(t *testing.T) {
	t.Setenv(EnvProfile, "full")
	got, err := ProfileFromEnv()
	if err != nil {
		t.Fatalf("ProfileFromEnv: %v", err)
	}
	if got != ProfileFull {
		t.Errorf("ProfileFromEnv = %q, want %q", got, ProfileFull)
	}
}

// ---- Manifest parsing (typed: hostname=baseType[@version]) ----

func TestParseManifest_TypedEntries_ValidAndMalformed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		raw     string
		want    []ManifestEntry
		wantErr bool
	}{
		{"empty is nil, no error", "", nil, false},
		{"whitespace-only is nil, no error", "   ", nil, false},
		{"bare type, no version", "db=postgresql", []ManifestEntry{{Hostname: "db", BaseType: "postgresql"}}, false},
		{"type with version", "db=postgresql@18", []ManifestEntry{{Hostname: "db", BaseType: "postgresql", Version: "18"}}, false},
		{
			"multiple entries, whitespace tolerated around = and ,",
			" db = postgresql@18 , cache=valkey ",
			[]ManifestEntry{{Hostname: "db", BaseType: "postgresql", Version: "18"}, {Hostname: "cache", BaseType: "valkey"}},
			false,
		},
		{"bare hostname (old format) errors", "db", nil, true},
		{"bare hostname among typed entries errors", "db=postgresql,cache", nil, true},
		{"unknown baseType errors", "db=no-such-engine", nil, true},
		{"empty segment (stray comma) errors", "db=postgresql,,cache=valkey", nil, true},
		{"empty hostname errors", "=postgresql", nil, true},
		{"empty baseType errors", "db=", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseManifest(c.raw)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ParseManifest(%q): want error, got nil (result %+v)", c.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseManifest(%q): %v", c.raw, err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("ParseManifest(%q) = %+v, want %+v", c.raw, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("ParseManifest(%q)[%d] = %+v, want %+v", c.raw, i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestManifestFromEnv_ReadsEnv(t *testing.T) {
	t.Setenv(EnvManifest, "db=postgresql, cache=valkey")
	got, err := ManifestFromEnv()
	if err != nil {
		t.Fatalf("ManifestFromEnv: %v", err)
	}
	want := []ManifestEntry{{Hostname: "db", BaseType: "postgresql"}, {Hostname: "cache", BaseType: "valkey"}}
	if len(got) != len(want) {
		t.Fatalf("ManifestFromEnv = %+v, want %+v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("ManifestFromEnv[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// ---- ValidateManifestAgainstConfig: manifest baseType must match the config's classification ----

func TestManifestValidate_MismatchAgainstConfig_Errors(t *testing.T) {
	t.Parallel()
	cfg := &LiveConfig{Services: []ServiceEntry{
		{Hostname: "db", Type: "postgresql", Tabular: &SQLDescriptor{Host: "db", Port: "5432"}},
	}}
	cases := []struct {
		name     string
		manifest []ManifestEntry
		wantErr  bool
	}{
		{"matching baseType passes", []ManifestEntry{{Hostname: "db", BaseType: "postgresql"}}, false},
		{"mismatched baseType errors naming both", []ManifestEntry{{Hostname: "db", BaseType: "valkey"}}, true},
		{"hostname absent from config errors", []ManifestEntry{{Hostname: "nope", BaseType: "postgresql"}}, true},
		{"empty manifest never errors", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateManifestAgainstConfig(c.manifest, cfg)
			if c.wantErr && err == nil {
				t.Fatalf("ValidateManifestAgainstConfig(%+v): want error, got nil", c.manifest)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("ValidateManifestAgainstConfig(%+v): %v", c.manifest, err)
			}
		})
	}

	// The mismatch case must name BOTH the manifest's claim and the config's
	// actual classification, so an operator can spot the typo immediately.
	err := ValidateManifestAgainstConfig([]ManifestEntry{{Hostname: "db", BaseType: "valkey"}}, cfg)
	if err == nil {
		t.Fatal("want error for a mismatched baseType, got nil")
	}
	if !strings.Contains(err.Error(), "valkey") || !strings.Contains(err.Error(), "postgresql") {
		t.Errorf("error %q must name both the manifest baseType (valkey) and the config's classification (postgresql)", err.Error())
	}
}

// ---- ValidateManifestAgainstConfig: hard version identity (spec-dataconsole-testing.md §8) ----
//
// A typed manifest entry carrying "@version" must match the config's declared
// version by PREFIX (requested "17" matches declared "17"/"17.7"; requested
// "8.16" matches declared "8.16" exactly) — a substituted version fails the
// run before any proof executes. Version-less manifest entries are exempt.

func TestManifestValidate_VersionIdentity_Enforced(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		manifest    ManifestEntry
		configType  string
		wantErr     bool
		errContains []string
	}{
		{
			name:       "requested 17 vs declared 17.7 prefix match passes",
			manifest:   ManifestEntry{Hostname: "db17", BaseType: "postgresql", Version: "17"},
			configType: "postgresql:single@17.7",
			wantErr:    false,
		},
		{
			name:        "requested 17 vs declared 18 errors naming hostname/requested/declared",
			manifest:    ManifestEntry{Hostname: "db17", BaseType: "postgresql", Version: "17"},
			configType:  "postgresql:single@18",
			wantErr:     true,
			errContains: []string{"db17", "17", "18"},
		},
		{
			name:       "requested 8.16 vs declared 8.16 exact match passes",
			manifest:   ManifestEntry{Hostname: "es816", BaseType: "elasticsearch", Version: "8.16"},
			configType: "elasticsearch@8.16",
			wantErr:    false,
		},
		{
			name:        "requested 8.1 vs declared 8.16 is NOT a component match — errors",
			manifest:    ManifestEntry{Hostname: "es816", BaseType: "elasticsearch", Version: "8.1"},
			configType:  "elasticsearch@8.16",
			wantErr:     true,
			errContains: []string{"es816", "8.1", "8.16"},
		},
		{
			name:        "requested 1 vs declared 17 is NOT a component match — errors",
			manifest:    ManifestEntry{Hostname: "db17", BaseType: "postgresql", Version: "1"},
			configType:  "postgresql:single@17",
			wantErr:     true,
			errContains: []string{"db17", "17"},
		},
		{
			name:        "versioned request vs version-less config type errors",
			manifest:    ManifestEntry{Hostname: "db17", BaseType: "postgresql", Version: "17"},
			configType:  "postgresql:single",
			wantErr:     true,
			errContains: []string{"db17", "17"},
		},
		{
			name:       "version-less request never checks version",
			manifest:   ManifestEntry{Hostname: "db17", BaseType: "postgresql"},
			configType: "postgresql:single@99",
			wantErr:    false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cfg := &LiveConfig{Services: []ServiceEntry{
				{Hostname: c.manifest.Hostname, Type: c.configType},
			}}
			err := ValidateManifestAgainstConfig([]ManifestEntry{c.manifest}, cfg)
			if c.wantErr && err == nil {
				t.Fatalf("ValidateManifestAgainstConfig(%+v): want error, got nil", c.manifest)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("ValidateManifestAgainstConfig(%+v): %v", c.manifest, err)
			}
			if err != nil {
				for _, want := range c.errContains {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q must contain %q", err.Error(), want)
					}
				}
			}
		})
	}
}

// ---- NamespaceFromEnv: the S10b fixture namespace (defaults, override) ----

func TestNamespaceFromEnv_Unset_ReturnsDefault(t *testing.T) {
	t.Setenv(EnvNamespace, "")
	if got := NamespaceFromEnv(); got != DefaultNamespace {
		t.Errorf("NamespaceFromEnv() = %q, want default %q", got, DefaultNamespace)
	}
}

func TestNamespaceFromEnv_ReadsOverride(t *testing.T) {
	t.Setenv(EnvNamespace, "dcconf_ci137")
	if got := NamespaceFromEnv(); got != "dcconf_ci137" {
		t.Errorf("NamespaceFromEnv() = %q, want %q", got, "dcconf_ci137")
	}
}

func TestNamespaceFromEnv_TrimsWhitespace(t *testing.T) {
	t.Setenv(EnvNamespace, "  dcconf_ci137  ")
	if got := NamespaceFromEnv(); got != "dcconf_ci137" {
		t.Errorf("NamespaceFromEnv() = %q, want %q", got, "dcconf_ci137")
	}
}

// ---- RequiredByManifest: the skip-vs-fail decision matrix ----

func TestRequiredByManifest_DecisionMatrix(t *testing.T) {
	t.Parallel()
	manifest := []ManifestEntry{{Hostname: "db", BaseType: "postgresql"}, {Hostname: "cache", BaseType: "valkey"}}
	cases := []struct {
		name     string
		profile  Profile
		manifest []ManifestEntry
		hostname string
		want     bool
	}{
		{"partial never gates, even if listed", ProfilePartial, manifest, "db", false},
		{"full + listed hostname gates", ProfileFull, manifest, "db", true},
		{"full + unlisted hostname does not gate", ProfileFull, manifest, "storage", false},
		{"full + empty manifest never gates", ProfileFull, nil, "db", false},
		{"partial + empty manifest never gates", ProfilePartial, nil, "db", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := RequiredByManifest(c.profile, c.manifest, c.hostname); got != c.want {
				t.Errorf("RequiredByManifest(%q, %v, %q) = %v, want %v", c.profile, c.manifest, c.hostname, got, c.want)
			}
		})
	}
}

// ---- sanity: config.go's JSON shape stays decodable with json.Unmarshal
// directly (guards against a stray struct tag typo breaking the documented
// shape in doc.go without any other test noticing). ----

func TestServiceEntry_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	in := ServiceEntry{
		Hostname: "db", Type: "postgresql",
		Tabular: &SQLDescriptor{Host: "db", Port: "5432", User: "u", Password: "p", Database: "d"},
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out ServiceEntry
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Hostname != in.Hostname || out.Type != in.Type || out.Tabular == nil || out.Tabular.Host != "db" {
		t.Errorf("round-trip mismatch: %+v", out)
	}
	if out.KV != nil || out.Object != nil || out.Document != nil || out.Stream != nil {
		t.Errorf("unset descriptor blocks must round-trip nil (omitempty), got %+v", out)
	}
}

// ---- version-matrix.import.yaml: structural sanity (docs/spec-dataconsole-testing.md
// §8's throwaway-compatibility-run fixture) — pure parse + shape assertion,
// no schema/network needed. Expectations are hand-written from the S7 brief's
// literals, independent of the YAML file's own content. ----

// versionMatrixService is one parsed "- hostname: ..." block from the fixture.
type versionMatrixService struct {
	Hostname, Type, Mode string
}

// parseVersionMatrixYAML extracts project.name + services[].{hostname,type,mode}
// from the fixture with a hand-rolled line scanner rather than a YAML library:
// this package's depguard allowlist (dataconsole-core-isolated, doc.go) is
// stdlib + provider + vendored engine drivers ONLY — gopkg.in/yaml.v3 is not on
// it, and pulling it in here would violate the package's "lifts to its own
// repo with a git mv" isolation contract for one test. The fixture's shape is
// fixed and simple enough that this scan is exact for it.
func parseVersionMatrixYAML(raw []byte) (projectName string, services []versionMatrixService) {
	nameRE := regexp.MustCompile(`^\s*name:\s*(\S+)\s*$`)
	hostRE := regexp.MustCompile(`^\s*-\s*hostname:\s*(\S+)\s*$`)
	typeRE := regexp.MustCompile(`^\s*type:\s*(\S+)\s*$`)
	modeRE := regexp.MustCompile(`^\s*mode:\s*(\S+)\s*$`)

	inServices := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "services:" {
			inServices = true
			continue
		}
		if !inServices {
			if m := nameRE.FindStringSubmatch(line); m != nil {
				projectName = m[1]
			}
			continue
		}
		if m := hostRE.FindStringSubmatch(line); m != nil {
			services = append(services, versionMatrixService{Hostname: m[1]})
			continue
		}
		if len(services) == 0 {
			continue
		}
		last := &services[len(services)-1]
		if m := typeRE.FindStringSubmatch(line); m != nil {
			last.Type = m[1]
		}
		if m := modeRE.FindStringSubmatch(line); m != nil {
			last.Mode = m[1]
		}
	}
	return projectName, services
}

func TestVersionMatrixImportYAML_StructurallySound(t *testing.T) {
	requireSourceTree(t)
	t.Parallel()
	path := filepath.Join("..", "..", "..", "..", "..", "e2e", "testdata", "dataconsole", "version-matrix.import.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	projectName, services := parseVersionMatrixYAML(raw)

	const wantProjectName = "zcp-dc-versions"
	if projectName != wantProjectName {
		t.Errorf("project.name = %q, want %q", projectName, wantProjectName)
	}

	// The alternate-version multi-version engines the live platform currently
	// offers a second version for. mariadb/valkey/typesense/clickhouse/
	// object-storage are deliberately absent (single-version types); kafka is
	// ALSO absent — a live catalog check (internal/schema/
	// dataconsole_version_matrix_test.go) proved the platform retired
	// kafka@3.8, leaving only the version the standing testbed already runs
	// — see the YAML's own header comment for both.
	wantTypes := map[string]string{
		"db17":       "postgresql@17",
		"es816":      "elasticsearch@8.16",
		"search110":  "meilisearch@1.10",
		"vectors110": "qdrant@1.10",
		"queue210":   "nats@2.10",
	}
	if len(services) != len(wantTypes) {
		t.Fatalf("services count = %d, want %d", len(services), len(wantTypes))
	}

	hostnameRE := regexp.MustCompile(`^[a-z0-9]{1,25}$`) // schema: max 25 chars, lowercase ASCII letters/digits only
	seen := make(map[string]bool, len(services))
	for _, svc := range services {
		if !hostnameRE.MatchString(svc.Hostname) {
			t.Errorf("hostname %q: must be 1-25 lowercase ASCII letters/digits", svc.Hostname)
		}
		seen[svc.Hostname] = true
		wantType, ok := wantTypes[svc.Hostname]
		if !ok {
			t.Errorf("unexpected hostname %q", svc.Hostname)
			continue
		}
		if svc.Type != wantType {
			t.Errorf("hostname %q: type = %q, want %q", svc.Hostname, svc.Type, wantType)
		}
		if svc.Mode != "NON_HA" {
			t.Errorf("hostname %q: mode = %q, want %q", svc.Hostname, svc.Mode, "NON_HA")
		}
	}
	for hostname := range wantTypes {
		if !seen[hostname] {
			t.Errorf("missing expected hostname %q", hostname)
		}
	}
}
