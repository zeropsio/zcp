package schema

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestParseZeropsYmlSchema_BuildBases(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/zerops_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	s, err := ParseZeropsYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"php build base", "php@8.4", true},
		{"php latest", "php@latest", true},
		{"nodejs build base", "nodejs@22", true},
		{"go build base", "go@1", true},
		{"rust build base", "rust@stable", true},
		{"dotnet build base", "dotnet@8", true},
		{"nonexistent", "foobar@1.0", false},
		{"php-nginx not in build", "php-nginx@8.4", false},
	}

	set := s.BuildBaseVersionSet()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := set[tt.value]
			if got != tt.want {
				t.Errorf("BuildBaseVersionSet[%q] = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseZeropsYmlSchema_RunBases(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/zerops_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	s, err := ParseZeropsYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"php-nginx run base", "php-nginx@8.4", true},
		{"php-apache run base", "php-apache@8.5", true},
		{"nginx run base", "nginx@1.22", true},
		{"static run base", "static", true},
		{"nodejs run base", "nodejs@22", true},
		{"docker run base", "docker@26.1", true},
		{"zcp run base", "zcp@1", true},
		{"bare php not in run", "php@8.4", false},
	}

	set := s.RunBaseSet()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := set[tt.value]
			if got != tt.want {
				t.Errorf("RunBaseSet[%q] = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseImportYmlSchema_ServiceTypes(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/import_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	s, err := ParseImportYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"php-nginx", "php-nginx@8.4", true},
		{"postgresql", "postgresql@16", true},
		{"keydb", "keydb@6", true},
		{"object-storage", "object-storage", true},
		{"shared-storage", "shared-storage", true},
		{"mariadb", "mariadb@10.6", true},
		{"nonexistent", "foobar@1.0", false},
		{"bare php not a service type", "php@8.4", false},
	}

	set := s.ServiceTypeSet()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := set[tt.value]
			if got != tt.want {
				t.Errorf("ServiceTypeSet[%q] = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseImportYmlSchema_Enums(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/import_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	s, err := ParseImportYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !slices.Contains(s.Modes, "HA") || !slices.Contains(s.Modes, "NON_HA") {
		t.Errorf("expected modes [HA, NON_HA], got %v", s.Modes)
	}
	if !slices.Contains(s.CorePackages, "LIGHT") || !slices.Contains(s.CorePackages, "SERIOUS") {
		t.Errorf("expected core packages [LIGHT, SERIOUS], got %v", s.CorePackages)
	}
	if len(s.StoragePolicies) == 0 {
		t.Error("expected storage policies, got none")
	}
}

func TestBuildBaseSet(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/zerops_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	s, err := ParseZeropsYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	set := s.BuildBaseSet()

	tests := []struct {
		base string
		want bool
	}{
		{"php", true},
		{"nodejs", true},
		{"go", true},
		{"rust", true},
		{"python", true},
		{"bun", true},
		{"dotnet", true},
		{"ruby", true},
		{"php-nginx", false},
		{"postgresql", false},
	}

	for _, tt := range tests {
		t.Run(tt.base, func(t *testing.T) {
			t.Parallel()
			if got := set[tt.base]; got != tt.want {
				t.Errorf("BuildBaseSet[%q] = %v, want %v", tt.base, got, tt.want)
			}
		})
	}
}

func TestFormatZeropsYmlForLLM(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/zerops_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	s, err := ParseZeropsYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	out := FormatZeropsYmlForLLM(s)
	if out == "" {
		t.Fatal("expected non-empty output")
	}

	checks := []string{
		"## zerops.yaml Schema (live)",
		"### build",
		"### run",
		"base",
		"deployFiles",
	}
	for _, check := range checks {
		if !strings.Contains(out, check) {
			t.Errorf("output missing %q", check)
		}
	}
}

func TestFormatImportYmlForLLM(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/import_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	s, err := ParseImportYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	out := FormatImportYmlForLLM(s)
	if out == "" {
		t.Fatal("expected non-empty output")
	}

	checks := []string{
		"## import.yaml Schema (live)",
		"### project",
		"### services[]",
		"hostname",
		"verticalAutoscaling",
	}
	for _, check := range checks {
		if !strings.Contains(out, check) {
			t.Errorf("output missing %q", check)
		}
	}
}

func TestCompactEnumList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{"empty", nil, "(none)"},
		{"single", []string{"static"}, "static"},
		{"grouped", []string{"php@8.1", "php@8.3", "php@8.4"}, "php@{8.1,8.3,8.4}"},
		{"mixed", []string{"static", "nodejs@22"}, "static, nodejs@22"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := compactEnumList(tt.values)
			if got != tt.want {
				t.Errorf("compactEnumList(%v) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}

func TestParseInvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := ParseZeropsYmlSchema([]byte("{invalid"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}

	_, err = ParseImportYmlSchema([]byte("{invalid"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
