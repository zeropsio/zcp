package telemetry

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// envMap builds a getenv func from a map, so every test controls its own env
// explicitly (spec §3.1 Resolve inputs are all passed, never read from real
// process env).
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolve_Precedence(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		seedDisabled bool // pre-seed install.json with disabled=true
		seedFresh    bool // pre-seed install.json already disclosed, enabled
		wantEnabled  bool
		wantPreDiscl bool
		wantChannel  string
	}{
		{
			name:        "rule1 ZCP_TELEMETRY=0 disables regardless of install state",
			env:         map[string]string{"ZCP_TELEMETRY": "0"},
			seedFresh:   true,
			wantEnabled: false,
			wantChannel: wire.ChannelExternal,
		},
		{
			name:        "rule1 ZCP_TELEMETRY=off disables",
			env:         map[string]string{"ZCP_TELEMETRY": "off"},
			seedFresh:   true,
			wantEnabled: false,
		},
		{
			name:        "rule1 ZCP_TELEMETRY=false disables",
			env:         map[string]string{"ZCP_TELEMETRY": "false"},
			seedFresh:   true,
			wantEnabled: false,
		},
		{
			name:        "rule1 ZCP_TELEMETRY=no disables",
			env:         map[string]string{"ZCP_TELEMETRY": "no"},
			seedFresh:   true,
			wantEnabled: false,
		},
		{
			name:        "rule2 DO_NOT_TRACK=1 disables",
			env:         map[string]string{"DO_NOT_TRACK": "1"},
			seedFresh:   true,
			wantEnabled: false,
		},
		{
			name:        "rule2 DO_NOT_TRACK=true disables",
			env:         map[string]string{"DO_NOT_TRACK": "true"},
			seedFresh:   true,
			wantEnabled: false,
		},
		{
			name:         "rule3 install.json disabled=true wins over no env",
			seedDisabled: true,
			wantEnabled:  false,
		},
		{
			name:         "rule4 no install file → pre-disclosure, disabled this process",
			wantEnabled:  false,
			wantPreDiscl: true,
		},
		{
			name:        "rule5 disclosed + not disabled → enabled",
			seedFresh:   true,
			wantEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.seedDisabled {
				path := installFilePath(home, false)
				if err := writeInstallFileAtomic(path, installFile{InstallID: "seed", DisclosedAt: "2026-01-01T00:00:00Z", Disabled: true}); err != nil {
					t.Fatalf("seed disabled install file: %v", err)
				}
			}
			if tt.seedFresh {
				path := installFilePath(home, false)
				if err := writeInstallFileAtomic(path, installFile{InstallID: "seed", DisclosedAt: "2026-01-01T00:00:00Z"}); err != nil {
					t.Fatalf("seed fresh install file: %v", err)
				}
			}

			var buf bytes.Buffer
			cfg := Resolve(envMap(tt.env), home, "v1.2.3", wire.RuntimeLocal, &buf)

			if cfg.Enabled != tt.wantEnabled {
				t.Errorf("Enabled = %v, want %v", cfg.Enabled, tt.wantEnabled)
			}
			if cfg.PreDisclosure != tt.wantPreDiscl {
				t.Errorf("PreDisclosure = %v, want %v", cfg.PreDisclosure, tt.wantPreDiscl)
			}
			if tt.wantChannel != "" && cfg.Channel != tt.wantChannel {
				t.Errorf("Channel = %q, want %q", cfg.Channel, tt.wantChannel)
			}
			if cfg.SessionID == "" {
				t.Error("SessionID must always be minted, even when disabled")
			}
		})
	}
}

func TestResolve_Reason(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		seedDisabled bool
		seedFresh    bool
		want         string
	}{
		{"rule1 env telemetry off", map[string]string{"ZCP_TELEMETRY": "0"}, false, true, ReasonOptedOutEnvTelemetry},
		{"rule2 do not track", map[string]string{"DO_NOT_TRACK": "1"}, false, true, ReasonOptedOutDoNotTrack},
		{"rule3 install file disabled", nil, true, false, ReasonDisabledInstallFile},
		{"rule4 pre-disclosure", nil, false, false, ReasonPreDisclosure},
		{"rule5 enabled", nil, false, true, ReasonEnabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.seedDisabled {
				path := installFilePath(home, false)
				if err := writeInstallFileAtomic(path, installFile{InstallID: "seed", DisclosedAt: "2026-01-01T00:00:00Z", Disabled: true}); err != nil {
					t.Fatalf("seed disabled install file: %v", err)
				}
			}
			if tt.seedFresh {
				path := installFilePath(home, false)
				if err := writeInstallFileAtomic(path, installFile{InstallID: "seed", DisclosedAt: "2026-01-01T00:00:00Z"}); err != nil {
					t.Fatalf("seed fresh install file: %v", err)
				}
			}
			var buf bytes.Buffer
			cfg := Resolve(envMap(tt.env), home, "v1.2.3", wire.RuntimeLocal, &buf)
			if cfg.Reason != tt.want {
				t.Errorf("Reason = %q, want %q", cfg.Reason, tt.want)
			}
		})
	}
}

func TestResolve_InstallFileCorrupt_ResultsDisabled(t *testing.T) {
	home := t.TempDir()
	path := installFilePath(home, false)
	if err := writeCorruptFile(path); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}

	var buf bytes.Buffer
	cfg := Resolve(envMap(nil), home, "v1.0.0", wire.RuntimeLocal, &buf)

	if cfg.Enabled {
		t.Error("Enabled = true, want false when install file is corrupt")
	}
	if cfg.PreDisclosure {
		t.Error("PreDisclosure = true, want false — a corrupt existing file is not the same as no file")
	}
	if cfg.Reason != ReasonInstallFileError {
		t.Errorf("Reason = %q, want %q", cfg.Reason, ReasonInstallFileError)
	}
}

func TestIsInternalChannel(t *testing.T) {
	tests := []struct {
		channel string
		want    bool
	}{
		{wire.ChannelExternal, false},
		{wire.ChannelCI, false},
		{wire.ChannelInternalDev, true},
		{wire.ChannelInternalEval, true},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsInternalChannel(tt.channel); got != tt.want {
			t.Errorf("IsInternalChannel(%q) = %v, want %v", tt.channel, got, tt.want)
		}
	}
}

func TestPrintDisclosureNotice_WritesSameTextAsResolve(t *testing.T) {
	var direct bytes.Buffer
	PrintDisclosureNotice(&direct)
	if direct.Len() == 0 {
		t.Fatal("PrintDisclosureNotice wrote nothing")
	}

	home := t.TempDir()
	var viaResolve bytes.Buffer
	Resolve(envMap(nil), home, "v1.0.0", wire.RuntimeLocal, &viaResolve)

	if direct.String() != viaResolve.String() {
		t.Fatalf("PrintDisclosureNotice text diverges from Resolve's pre-disclosure print:\n%q\nvs\n%q", direct.String(), viaResolve.String())
	}
}

func TestResolve_PreDisclosure_StampsInstallFileAndPrintsNoticeExactlyOnce(t *testing.T) {
	home := t.TempDir()
	var buf bytes.Buffer

	cfg := Resolve(envMap(nil), home, "v1.0.0", wire.RuntimeLocal, &buf)

	if !cfg.PreDisclosure {
		t.Fatal("want PreDisclosure = true on first-ever resolve")
	}
	if cfg.Enabled {
		t.Fatal("want Enabled = false during the disclosure process itself (spec §3.3)")
	}
	if buf.Len() == 0 {
		t.Fatal("disclosure notice was not printed")
	}
	if strings.Count(buf.String(), "ZCP_TELEMETRY=0") != 1 {
		t.Fatalf("disclosure notice printed unexpected number of times: %q", buf.String())
	}

	f, exists, err := loadInstallFile(installFilePath(home, false))
	if err != nil || !exists {
		t.Fatalf("expected install file to be stamped: exists=%v err=%v", exists, err)
	}
	if f.DisclosedAt == "" {
		t.Fatal("DisclosedAt not stamped")
	}
	if f.InstallID != cfg.InstallID {
		t.Fatalf("stamped InstallID %q != returned Config.InstallID %q", f.InstallID, cfg.InstallID)
	}

	// Second resolve on the same home dir: file now exists+disclosed → enabled.
	var buf2 bytes.Buffer
	cfg2 := Resolve(envMap(nil), home, "v1.0.0", wire.RuntimeLocal, &buf2)
	if cfg2.PreDisclosure {
		t.Fatal("second resolve should not be pre-disclosure")
	}
	if !cfg2.Enabled {
		t.Fatal("second resolve should be enabled")
	}
	if cfg2.InstallID != cfg.InstallID {
		t.Fatalf("InstallID changed across processes: %q vs %q", cfg2.InstallID, cfg.InstallID)
	}
	if buf2.Len() != 0 {
		t.Fatal("disclosure notice must not be printed again after the first process")
	}
}

func TestResolve_Channel(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"unset defaults external", nil, wire.ChannelExternal},
		{"unset with CI=true maps to ci", map[string]string{"CI": "true"}, wire.ChannelCI},
		{"explicit internal_dev", map[string]string{"ZCP_TELEMETRY_CHANNEL": "internal_dev"}, wire.ChannelInternalDev},
		{"explicit internal_eval", map[string]string{"ZCP_TELEMETRY_CHANNEL": "internal_eval"}, wire.ChannelInternalEval},
		{"explicit ci", map[string]string{"ZCP_TELEMETRY_CHANNEL": "ci"}, wire.ChannelCI},
		{"invalid value falls back to external", map[string]string{"ZCP_TELEMETRY_CHANNEL": "bogus"}, wire.ChannelExternal},
		{"explicit channel wins over CI env", map[string]string{"ZCP_TELEMETRY_CHANNEL": "external", "CI": "true"}, wire.ChannelExternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			var buf bytes.Buffer
			cfg := Resolve(envMap(tt.env), home, "v1.0.0", wire.RuntimeLocal, &buf)
			if cfg.Channel != tt.want {
				t.Errorf("Channel = %q, want %q", cfg.Channel, tt.want)
			}
		})
	}
}

func TestResolve_InternalChannelUsesSeparateInstallFile(t *testing.T) {
	home := t.TempDir()
	var buf bytes.Buffer

	cfg := Resolve(envMap(map[string]string{"ZCP_TELEMETRY_CHANNEL": "internal_dev"}), home, "v1.0.0", wire.RuntimeLocal, &buf)
	if !cfg.PreDisclosure {
		t.Fatal("want pre-disclosure on first internal_dev resolve")
	}

	if _, exists, _ := loadInstallFile(installFilePath(home, false)); exists {
		t.Fatal("external install.json must not be written by an internal channel resolve")
	}
	if _, exists, _ := loadInstallFile(installFilePath(home, true)); !exists {
		t.Fatal("install-internal.json must be written by an internal channel resolve")
	}
}

func TestResolve_Endpoint(t *testing.T) {
	home := t.TempDir()
	var buf bytes.Buffer

	cfg := Resolve(envMap(nil), home, "v1.0.0", wire.RuntimeLocal, &buf)
	if cfg.Endpoint != DefaultEndpoint {
		t.Errorf("Endpoint = %q, want default %q", cfg.Endpoint, DefaultEndpoint)
	}

	cfg2 := Resolve(envMap(map[string]string{"ZCP_TELEMETRY_ENDPOINT": "http://localhost:9999/v1/events"}), home, "v1.0.0", wire.RuntimeLocal, &buf)
	if cfg2.Endpoint != "http://localhost:9999/v1/events" {
		t.Errorf("Endpoint override not applied: %q", cfg2.Endpoint)
	}
}

func TestResolve_Debug(t *testing.T) {
	home := t.TempDir()
	var buf bytes.Buffer

	cfg := Resolve(envMap(map[string]string{"ZCP_TELEMETRY_DEBUG": "1"}), home, "v1.0.0", wire.RuntimeLocal, &buf)
	if !cfg.Debug {
		t.Error("Debug = false, want true for ZCP_TELEMETRY_DEBUG=1")
	}

	cfg2 := Resolve(envMap(nil), home, "v1.0.0", wire.RuntimeLocal, &buf)
	if cfg2.Debug {
		t.Error("Debug = true, want false when unset")
	}
}

func TestResolve_VersionCoercion(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"v6.31.0", "v6.31.0"},
		{"6.31.0", "6.31.0"},
		{"dev", "dev"},
		{"not-a-version", "dev"},
		{"", "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			home := t.TempDir()
			var buf bytes.Buffer
			cfg := Resolve(envMap(nil), home, tt.in, wire.RuntimeLocal, &buf)
			if cfg.ZcpVersion != tt.want {
				t.Errorf("ZcpVersion = %q, want %q", cfg.ZcpVersion, tt.want)
			}
		})
	}
}

func TestResolve_RuntimeEnvPassthrough(t *testing.T) {
	home := t.TempDir()
	var buf bytes.Buffer
	cfg := Resolve(envMap(nil), home, "v1.0.0", wire.RuntimeContainer, &buf)
	if cfg.RuntimeEnv != wire.RuntimeContainer {
		t.Errorf("RuntimeEnv = %q, want %q", cfg.RuntimeEnv, wire.RuntimeContainer)
	}
}

func TestResolve_OSArchAlwaysPopulated(t *testing.T) {
	home := t.TempDir()
	var buf bytes.Buffer
	cfg := Resolve(envMap(nil), home, "v1.0.0", wire.RuntimeLocal, &buf)
	if cfg.OS == "" || cfg.Arch == "" {
		t.Errorf("OS/Arch not populated: OS=%q Arch=%q", cfg.OS, cfg.Arch)
	}
}

// writeCorruptFile seeds an unparseable install file at path.
func writeCorruptFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("{not-json"), 0o600)
}
