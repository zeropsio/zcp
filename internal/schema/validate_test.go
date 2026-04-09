package schema

import (
	"os"
	"testing"
)

func TestExtractValidFields(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/zerops_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	s, err := ParseZeropsYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	vf := ExtractValidFields(s)
	if vf == nil {
		t.Fatal("expected non-nil ValidFields")
	}

	tests := []struct {
		name    string
		section string
		field   string
		want    bool
	}{
		// Setup-level fields.
		{"setup at root", "setup", "setup", true},
		{"build at root", "setup", "build", true},
		{"deploy at root", "setup", "deploy", true},
		{"run at root", "setup", "run", true},
		{"extends at root", "setup", "extends", true},
		{"verticalAutoscaling at root", "setup", "verticalAutoscaling", false},

		// Build fields.
		{"build base", "build", "base", true},
		{"build buildCommands", "build", "buildCommands", true},
		{"build deployFiles", "build", "deployFiles", true},
		{"build cache", "build", "cache", true},
		{"build envVariables", "build", "envVariables", true},
		{"build os", "build", "os", true},
		{"build prepareCommands", "build", "prepareCommands", true},
		{"build verticalAutoscaling", "build", "verticalAutoscaling", false},
		{"build healthCheck", "build", "healthCheck", false},

		// Deploy fields.
		{"deploy readinessCheck", "deploy", "readinessCheck", true},
		{"deploy temporaryShutdown", "deploy", "temporaryShutdown", true},
		{"deploy healthCheck", "deploy", "healthCheck", false},

		// Run fields.
		{"run base", "run", "base", true},
		{"run start", "run", "start", true},
		{"run ports", "run", "ports", true},
		{"run healthCheck", "run", "healthCheck", true},
		{"run envVariables", "run", "envVariables", true},
		{"run initCommands", "run", "initCommands", true},
		{"run os", "run", "os", true},
		{"run prepareCommands", "run", "prepareCommands", true},
		{"run verticalAutoscaling", "run", "verticalAutoscaling", false},
		{"run deployFiles", "run", "deployFiles", false},
		{"run buildCommands", "run", "buildCommands", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var set map[string]bool
			switch tt.section {
			case "setup":
				set = vf.Setup
			case "build":
				set = vf.Build
			case "deploy":
				set = vf.Deploy
			case "run":
				set = vf.Run
			}
			if got := set[tt.field]; got != tt.want {
				t.Errorf("ValidFields.%s[%q] = %v, want %v", tt.section, tt.field, got, tt.want)
			}
		})
	}
}

func TestExtractValidFields_Nil(t *testing.T) {
	t.Parallel()
	if vf := ExtractValidFields(nil); vf != nil {
		t.Errorf("expected nil for nil schema, got %v", vf)
	}
	if vf := ExtractValidFields(&ZeropsYmlSchema{}); vf != nil {
		t.Errorf("expected nil for empty schema, got %v", vf)
	}
}

func TestValidateZeropsYmlRaw_ValidContent(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/zerops_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}
	s, err := ParseZeropsYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	vf := ExtractValidFields(s)

	validYAML := []byte(`zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - ./dist
        - ./node_modules
      cache:
        - node_modules
    deploy:
      readinessCheck:
        httpGet:
          port: 3000
          path: /api/health
    run:
      base: nodejs@22
      start: node dist/main.js
      ports:
        - port: 3000
          httpSupport: true
      healthCheck:
        httpGet:
          port: 3000
          path: /api/health
      envVariables:
        NODE_ENV: production
      initCommands:
        - node dist/migrate.js
`)

	errs := ValidateZeropsYmlRaw(validYAML, vf)
	if len(errs) > 0 {
		t.Errorf("expected no errors for valid YAML, got %v", errs)
	}
}

func TestValidateZeropsYmlRaw_UnknownFields(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/zerops_yml_schema.json")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}
	s, err := ParseZeropsYmlSchema(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	vf := ExtractValidFields(s)

	tests := []struct {
		name      string
		yaml      string
		wantCount int
		wantField string
		wantSetup string
		wantSect  string
	}{
		{
			name: "verticalAutoscaling under run",
			yaml: `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: ./dist
    run:
      base: nodejs@22
      start: node dist/main.js
      verticalAutoscaling:
        minRam: 0.25
`,
			wantCount: 1,
			wantField: "verticalAutoscaling",
			wantSetup: "prod",
			wantSect:  "run",
		},
		{
			name: "unknown field at setup level",
			yaml: `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
      deployFiles: ./
    scaling:
      min: 1
`,
			wantCount: 1,
			wantField: "scaling",
			wantSetup: "dev",
			wantSect:  "",
		},
		{
			name: "unknown field under build",
			yaml: `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: ./dist
      runtime: nodejs
`,
			wantCount: 1,
			wantField: "runtime",
			wantSetup: "prod",
			wantSect:  "build",
		},
		{
			name: "multiple unknown fields",
			yaml: `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: ./dist
    run:
      base: nodejs@22
      start: node dist/main.js
      verticalAutoscaling:
        minRam: 0.25
      horizontalAutoscaling:
        minContainers: 2
`,
			wantCount: 2,
			wantSetup: "prod",
			wantSect:  "run",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := ValidateZeropsYmlRaw([]byte(tt.yaml), vf)
			if len(errs) != tt.wantCount {
				t.Fatalf("got %d errors, want %d: %v", len(errs), tt.wantCount, errs)
			}
			if tt.wantCount == 1 {
				if errs[0].Field != tt.wantField {
					t.Errorf("field = %q, want %q", errs[0].Field, tt.wantField)
				}
				if errs[0].Setup != tt.wantSetup {
					t.Errorf("setup = %q, want %q", errs[0].Setup, tt.wantSetup)
				}
				if errs[0].Section != tt.wantSect {
					t.Errorf("section = %q, want %q", errs[0].Section, tt.wantSect)
				}
			}
		})
	}
}

func TestValidateZeropsYmlRaw_NilInputs(t *testing.T) {
	t.Parallel()

	// Nil valid fields.
	errs := ValidateZeropsYmlRaw([]byte(`zerops: []`), nil)
	if len(errs) != 0 {
		t.Errorf("expected no errors with nil ValidFields, got %v", errs)
	}

	// Invalid YAML.
	vf := &ValidFields{Setup: map[string]bool{"setup": true}}
	errs = ValidateZeropsYmlRaw([]byte(`{invalid`), vf)
	if len(errs) != 0 {
		t.Errorf("expected no errors for invalid YAML, got %v", errs)
	}

	// Empty YAML.
	errs = ValidateZeropsYmlRaw([]byte(``), vf)
	if len(errs) != 0 {
		t.Errorf("expected no errors for empty YAML, got %v", errs)
	}
}
