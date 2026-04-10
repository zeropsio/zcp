package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestFlexBool_UnmarshalJSON covers every input form we promise to
// accept and a few that we promise to reject. Table driven so adding
// a new form is a one-liner.
func TestFlexBool_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{"native true", `true`, true, false},
		{"native false", `false`, false, false},
		{"string true", `"true"`, true, false},
		{"string false", `"false"`, false, false},
		{"string TRUE uppercase", `"TRUE"`, true, false},
		{"string False mixed case", `"False"`, false, false},
		{"null", `null`, false, false},
		{"empty data", ``, false, false},
		{"string yes rejected", `"yes"`, false, true},
		{"string 1 rejected", `"1"`, false, true},
		{"integer 1 rejected", `1`, false, true},
		{"integer 0 rejected", `0`, false, true},
		{"object rejected", `{"v":true}`, false, true},
		{"array rejected", `[true]`, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var f FlexBool
			err := f.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got f=%v", tt.input, bool(f))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
			if bool(f) != tt.want {
				t.Errorf("input %q: got %v, want %v", tt.input, bool(f), tt.want)
			}
		})
	}
}

// TestFlexBool_MarshalJSON verifies the wire form stays canonical even
// when the value was unmarshaled from a stringified form.
func TestFlexBool_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   FlexBool
		want string
	}{
		{FlexBool(true), "true"},
		{FlexBool(false), "false"},
	}
	for _, tt := range tests {
		got, err := json.Marshal(tt.in)
		if err != nil {
			t.Fatalf("marshal %v: %v", tt.in, err)
		}
		if string(got) != tt.want {
			t.Errorf("marshal %v: got %s, want %s", tt.in, got, tt.want)
		}
	}
}

// TestFlexBool_InsideStruct is the concrete scenario: a tool input
// struct with FlexBool fields unmarshals successfully from both native
// and stringified forms.
func TestFlexBool_InsideStruct(t *testing.T) {
	t.Parallel()

	type sample struct {
		IncludeEnvs FlexBool `json:"includeEnvs,omitempty"`
		Project     FlexBool `json:"project,omitempty"`
	}

	tests := []struct {
		name         string
		payload      string
		wantEnvs     bool
		wantProject  bool
		wantDecodErr bool
	}{
		{
			name:        "both native",
			payload:     `{"includeEnvs": true, "project": false}`,
			wantEnvs:    true,
			wantProject: false,
		},
		{
			name:        "both stringified",
			payload:     `{"includeEnvs": "true", "project": "false"}`,
			wantEnvs:    true,
			wantProject: false,
		},
		{
			name:        "mixed",
			payload:     `{"includeEnvs": "true", "project": true}`,
			wantEnvs:    true,
			wantProject: true,
		},
		{
			name:         "string yes fails",
			payload:      `{"includeEnvs": "yes"}`,
			wantDecodErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var s sample
			err := json.Unmarshal([]byte(tt.payload), &s)
			if tt.wantDecodErr {
				if err == nil {
					t.Fatalf("expected unmarshal error for %q", tt.payload)
				}
				if !strings.Contains(err.Error(), "FlexBool") {
					t.Errorf("error should mention FlexBool so agents can recover, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bool(s.IncludeEnvs) != tt.wantEnvs {
				t.Errorf("includeEnvs: got %v, want %v", bool(s.IncludeEnvs), tt.wantEnvs)
			}
			if bool(s.Project) != tt.wantProject {
				t.Errorf("project: got %v, want %v", bool(s.Project), tt.wantProject)
			}
		})
	}
}
