package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// ---- Manifest parsing ----

func TestParseManifest_TrimsAndDropsEmpty(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,, c ", []string{"a", "b", "c"}},
		{"solo", []string{"solo"}},
	}
	for _, c := range cases {
		got := ParseManifest(c.raw)
		if len(got) != len(c.want) {
			t.Errorf("ParseManifest(%q) = %v, want %v", c.raw, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseManifest(%q) = %v, want %v", c.raw, got, c.want)
				break
			}
		}
	}
}

func TestManifestFromEnv_ReadsEnv(t *testing.T) {
	t.Setenv(EnvManifest, "db, cache")
	got := ManifestFromEnv()
	if len(got) != 2 || got[0] != "db" || got[1] != "cache" {
		t.Errorf("ManifestFromEnv = %v, want [db cache]", got)
	}
}

// ---- RequiredByManifest: the skip-vs-fail decision matrix ----

func TestRequiredByManifest_DecisionMatrix(t *testing.T) {
	t.Parallel()
	manifest := []string{"db", "cache"}
	cases := []struct {
		name     string
		profile  Profile
		manifest []string
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
