package schema

import (
	"strings"
	"testing"
)

// TestValidateImportYAMLStructure_AcceptsUnknownType pins the Phase-1 contract:
// the structure-only schema MUST accept a service type that is newer than the
// embedded enum (export types come from a live Discover → already valid), while
// STILL rejecting genuine structural defects (field typos, bad stable enums).
func TestValidateImportYAMLStructure_AcceptsUnknownType(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantErr     bool
		errContains string
	}{
		{
			name: "unknown service type accepted",
			yaml: `services:
  - hostname: app
    type: ubuntu/nodejs@99
    enableSubdomainAccess: true
`,
			wantErr: false,
		},
		{
			name: "typo'd top-level field still rejected",
			yaml: `services:
  - hostname: app
    type: ubuntu/nodejs@22
    bogusField: x
`,
			wantErr:     true,
			errContains: "bogusField",
		},
		{
			name: "bad objectStoragePolicy still rejected",
			yaml: `services:
  - hostname: app
    type: ubuntu/nodejs@22
    objectStoragePolicy: not-a-real-policy
`,
			wantErr:     true,
			errContains: "objectStoragePolicy",
		},
		{
			name: "missing required hostname still rejected",
			yaml: `services:
  - type: ubuntu/nodejs@22
`,
			wantErr:     true,
			errContains: "hostname",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateImportYAMLStructure(tt.yaml)
			got := len(errs) > 0
			if got != tt.wantErr {
				t.Fatalf("ValidateImportYAMLStructure wantErr=%v got %d errors: %v", tt.wantErr, len(errs), errs)
			}
			if tt.errContains != "" {
				found := false
				for _, e := range errs {
					if strings.Contains(e.Error(), tt.errContains) {
						found = true
					}
				}
				if !found {
					t.Fatalf("expected an error containing %q, got %v", tt.errContains, errs)
				}
			}
		})
	}
}

// TestValidateZeropsYAMLStructure_AcceptsUnknownBase pins that build.base
// (oneOf string-or-array) and run.base accept a brand-new base in BOTH forms,
// while a non-string base and field typos still reject.
func TestValidateZeropsYAMLStructure_AcceptsUnknownBase(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantErr     bool
		errContains string
	}{
		{
			name: "unknown run.base accepted",
			yaml: `zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands: ["x"]
      deployFiles: ["./"]
    run:
      base: ubuntu/nodejs@99
`,
			wantErr: false,
		},
		{
			name: "unknown build.base string accepted",
			yaml: `zerops:
  - setup: app
    build:
      base: zigzag@99
      buildCommands: ["x"]
      deployFiles: ["./"]
    run:
      base: nodejs@22
`,
			wantErr: false,
		},
		{
			name: "unknown build.base array form accepted",
			yaml: `zerops:
  - setup: app
    build:
      base: [zigzag@99, nodejs@22]
      buildCommands: ["x"]
      deployFiles: ["./"]
    run:
      base: nodejs@22
`,
			wantErr: false,
		},
		{
			name: "non-string build.base still rejected",
			yaml: `zerops:
  - setup: app
    build:
      base: 12345
      buildCommands: ["x"]
      deployFiles: ["./"]
    run:
      base: nodejs@22
`,
			wantErr: true,
		},
		{
			name: "typo'd field under run still rejected",
			yaml: `zerops:
  - setup: app
    run:
      base: nodejs@22
      bogusRunField: x
`,
			wantErr:     true,
			errContains: "bogusRunField",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateZeropsYAMLStructure(tt.yaml, "")
			got := len(errs) > 0
			if got != tt.wantErr {
				t.Fatalf("ValidateZeropsYAMLStructure wantErr=%v got %d errors: %v", tt.wantErr, len(errs), errs)
			}
			if tt.errContains != "" {
				found := false
				for _, e := range errs {
					if strings.Contains(e.Error(), tt.errContains) {
						found = true
					}
				}
				if !found {
					t.Fatalf("expected an error containing %q, got %v", tt.errContains, errs)
				}
			}
		})
	}
}
