package workflow

import (
	"strings"
	"testing"
)

func TestNewCICDState_Initialization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		hostnames []string
		wantSteps int
	}{
		{
			name:      "single hostname",
			hostnames: []string{"appdev"},
			wantSteps: 3,
		},
		{
			name:      "multiple hostnames",
			hostnames: []string{"appdev", "apidev"},
			wantSteps: 3,
		},
		{
			name:      "nil hostnames",
			hostnames: nil,
			wantSteps: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := NewCICDState(tt.hostnames)
			if !cs.Active {
				t.Error("expected Active")
			}
			if cs.CurrentStep != 0 {
				t.Errorf("CurrentStep: want 0, got %d", cs.CurrentStep)
			}
			if len(cs.Steps) != tt.wantSteps {
				t.Errorf("Steps: want %d, got %d", tt.wantSteps, len(cs.Steps))
			}
			if cs.Provider != "" {
				t.Errorf("Provider: want empty, got %q", cs.Provider)
			}
			// First step should be choose.
			if cs.Steps[0].Name != CICDStepChoose {
				t.Errorf("first step: want %q, got %q", CICDStepChoose, cs.Steps[0].Name)
			}
		})
	}
}

func TestCICDState_CompleteStep_Sequence(t *testing.T) {
	t.Parallel()
	cs := NewCICDState([]string{"appdev"})
	cs.Steps[0].Status = stepInProgress

	// Complete choose.
	if err := cs.CompleteStep(CICDStepChoose, "User chose GitHub Actions for CI/CD"); err != nil {
		t.Fatalf("complete choose: %v", err)
	}
	if cs.CurrentStepName() != CICDStepConfigure {
		t.Errorf("after choose: want configure, got %s", cs.CurrentStepName())
	}

	// Complete configure.
	if err := cs.CompleteStep(CICDStepConfigure, "GitHub Actions workflow file created and pushed"); err != nil {
		t.Fatalf("complete configure: %v", err)
	}
	if cs.CurrentStepName() != CICDStepVerify {
		t.Errorf("after configure: want verify, got %s", cs.CurrentStepName())
	}

	// Complete verify.
	if err := cs.CompleteStep(CICDStepVerify, "Pipeline triggered successfully, deployment verified"); err != nil {
		t.Fatalf("complete verify: %v", err)
	}
	if cs.Active {
		t.Error("should be inactive after all steps")
	}
	if cs.CurrentStepName() != "" {
		t.Errorf("after all steps: want empty, got %q", cs.CurrentStepName())
	}
}

func TestCICDState_CompleteStep_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		setup       func() *CICDState
		step        string
		attestation string
		wantErr     string
	}{
		{
			name: "wrong step name",
			setup: func() *CICDState {
				cs := NewCICDState(nil)
				cs.Steps[0].Status = stepInProgress
				return cs
			},
			step:        CICDStepVerify,
			attestation: "some attestation text here for test",
			wantErr:     "choose",
		},
		{
			name: "attestation too short",
			setup: func() *CICDState {
				cs := NewCICDState(nil)
				cs.Steps[0].Status = stepInProgress
				return cs
			},
			step:        CICDStepChoose,
			attestation: "short",
			wantErr:     "too short",
		},
		{
			name: "not active",
			setup: func() *CICDState {
				cs := NewCICDState(nil)
				cs.Active = false
				return cs
			},
			step:        CICDStepChoose,
			attestation: "some attestation text here for test",
			wantErr:     "not active",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := tt.setup()
			err := cs.CompleteStep(tt.step, tt.attestation)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestCICDState_SetProvider(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{"github", CICDProviderGitHub, false},
		{"gitlab", CICDProviderGitLab, false},
		{"webhook", CICDProviderWebhook, false},
		{"generic", CICDProviderGeneric, false},
		{"invalid", "jenkins", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := NewCICDState([]string{"appdev"})
			err := cs.SetProvider(tt.provider)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetProvider(%q): err = %v, wantErr = %v", tt.provider, err, tt.wantErr)
			}
			if !tt.wantErr && cs.Provider != tt.provider {
				t.Errorf("Provider: want %q, got %q", tt.provider, cs.Provider)
			}
		})
	}
}

func TestCICDState_BuildResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		setup       func() *CICDState
		wantStep    string
		wantMsg     string
		wantNilCurr bool
	}{
		{
			name: "at choose step",
			setup: func() *CICDState {
				cs := NewCICDState([]string{"appdev"})
				cs.Steps[0].Status = stepInProgress
				return cs
			},
			wantStep: CICDStepChoose,
			wantMsg:  "CI/CD step 1/3",
		},
		{
			name: "at configure step with provider",
			setup: func() *CICDState {
				cs := NewCICDState([]string{"appdev"})
				cs.Steps[0].Status = stepComplete
				cs.CurrentStep = 1
				cs.Steps[1].Status = stepInProgress
				cs.Provider = CICDProviderGitHub
				return cs
			},
			wantStep: CICDStepConfigure,
			wantMsg:  "CI/CD step 2/3",
		},
		{
			name: "completed",
			setup: func() *CICDState {
				cs := NewCICDState([]string{"appdev"})
				cs.Active = false
				cs.CurrentStep = 3
				for i := range cs.Steps {
					cs.Steps[i].Status = stepComplete
				}
				return cs
			},
			wantNilCurr: true,
			wantMsg:     "CI/CD setup complete",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cs := tt.setup()
			resp := cs.BuildResponse("sess-1", "set up cicd", EnvContainer, nil)

			if resp.SessionID != "sess-1" {
				t.Errorf("SessionID: want sess-1, got %s", resp.SessionID)
			}
			if resp.Progress.Total != 3 {
				t.Errorf("Progress.Total: want 3, got %d", resp.Progress.Total)
			}
			if !strings.Contains(resp.Message, tt.wantMsg) {
				t.Errorf("Message = %q, want to contain %q", resp.Message, tt.wantMsg)
			}
			if tt.wantNilCurr {
				if resp.Current != nil {
					t.Error("expected nil Current")
				}
			} else {
				if resp.Current == nil {
					t.Fatal("expected non-nil Current")
				}
				if resp.Current.Name != tt.wantStep {
					t.Errorf("Current.Name: want %q, got %q", tt.wantStep, resp.Current.Name)
				}
			}
		})
	}
}
