package platform

import (
	"regexp"
	"testing"
	"time"

	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
)

// rfc3339Re matches RFC3339/RFC3339Nano timestamps (contains "T", ends with "Z" or offset).
var rfc3339Re = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)

func TestMapEsProcessEvent_TimestampsRFC3339(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 23, 14, 30, 0, 0, time.UTC)
	started := now.Add(1 * time.Second)
	finished := now.Add(5 * time.Second)

	tests := []struct {
		name         string
		input        output.EsProcess
		wantCreated  string
		wantStarted  *string
		wantFinished *string
	}{
		{
			name: "all_timestamps_rfc3339",
			input: output.EsProcess{
				Created:  types.NewDateTime(now),
				Started:  types.NewDateTimeNull(started),
				Finished: types.NewDateTimeNull(finished),
				Status:   enum.ProcessStatusEnumRunning,
			},
			wantCreated:  now.Format(time.RFC3339Nano),
			wantStarted:  strPtr(started.Format(time.RFC3339Nano)),
			wantFinished: strPtr(finished.Format(time.RFC3339Nano)),
		},
		{
			name: "nil_optional_timestamps",
			input: output.EsProcess{
				Created: types.NewDateTime(now),
				Status:  enum.ProcessStatusEnumRunning,
			},
			wantCreated:  now.Format(time.RFC3339Nano),
			wantStarted:  nil,
			wantFinished: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := mapEsProcessEvent(tt.input)

			if !rfc3339Re.MatchString(result.Created) {
				t.Errorf("Created not RFC3339: %q", result.Created)
			}
			if result.Created != tt.wantCreated {
				t.Errorf("Created = %q, want %q", result.Created, tt.wantCreated)
			}

			if tt.wantStarted != nil {
				if result.Started == nil {
					t.Fatal("Started is nil, want non-nil")
				}
				if !rfc3339Re.MatchString(*result.Started) {
					t.Errorf("Started not RFC3339: %q", *result.Started)
				}
				if *result.Started != *tt.wantStarted {
					t.Errorf("Started = %q, want %q", *result.Started, *tt.wantStarted)
				}
			} else if result.Started != nil {
				t.Errorf("Started = %q, want nil", *result.Started)
			}

			if tt.wantFinished != nil {
				if result.Finished == nil {
					t.Fatal("Finished is nil, want non-nil")
				}
				if !rfc3339Re.MatchString(*result.Finished) {
					t.Errorf("Finished not RFC3339: %q", *result.Finished)
				}
				if *result.Finished != *tt.wantFinished {
					t.Errorf("Finished = %q, want %q", *result.Finished, *tt.wantFinished)
				}
			} else if result.Finished != nil {
				t.Errorf("Finished = %q, want nil", *result.Finished)
			}
		})
	}
}

func TestMapEsProcessEvent_FailReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		publicMeta     types.MapNull
		wantFailReason *string
	}{
		{
			name:           "no_public_meta",
			publicMeta:     types.MapNull{},
			wantFailReason: nil,
		},
		{
			name:           "public_meta_with_failReason",
			publicMeta:     types.NewMapNull(map[string]any{"failReason": "build timeout exceeded"}),
			wantFailReason: strPtr("build timeout exceeded"),
		},
		{
			name:           "public_meta_without_failReason",
			publicMeta:     types.NewMapNull(map[string]any{"otherKey": "value"}),
			wantFailReason: nil,
		},
		{
			name:           "public_meta_with_empty_failReason",
			publicMeta:     types.NewMapNull(map[string]any{"failReason": ""}),
			wantFailReason: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := output.EsProcess{
				Created:    types.NewDateTime(time.Now()),
				Status:     enum.ProcessStatusEnumFailed,
				PublicMeta: tt.publicMeta,
			}
			result := mapEsProcessEvent(input)

			if tt.wantFailReason == nil {
				if result.FailReason != nil {
					t.Errorf("FailReason = %q, want nil", *result.FailReason)
				}
			} else {
				if result.FailReason == nil {
					t.Fatal("FailReason is nil, want non-nil")
				}
				if *result.FailReason != *tt.wantFailReason {
					t.Errorf("FailReason = %q, want %q", *result.FailReason, *tt.wantFailReason)
				}
			}
		})
	}
}

func TestMapEsAppVersionEvent_TimestampsRFC3339(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 23, 14, 30, 0, 0, time.UTC)
	later := now.Add(10 * time.Second)

	input := output.EsAppVersion{
		Created:    types.NewDateTime(now),
		LastUpdate: types.NewDateTime(later),
		Status:     enum.AppVersionStatusEnumUploading,
		Source:     enum.AppVersionSourceEnumCli,
	}

	result := mapEsAppVersionEvent(input)

	if !rfc3339Re.MatchString(result.Created) {
		t.Errorf("Created not RFC3339: %q", result.Created)
	}
	if result.Created != now.Format(time.RFC3339Nano) {
		t.Errorf("Created = %q, want %q", result.Created, now.Format(time.RFC3339Nano))
	}
	if !rfc3339Re.MatchString(result.LastUpdate) {
		t.Errorf("LastUpdate not RFC3339: %q", result.LastUpdate)
	}
	if result.LastUpdate != later.Format(time.RFC3339Nano) {
		t.Errorf("LastUpdate = %q, want %q", result.LastUpdate, later.Format(time.RFC3339Nano))
	}
}

func TestMapEsAppVersionEvent_BuildTimestampsRFC3339(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 23, 14, 30, 0, 0, time.UTC)
	pipeStart := now.Add(1 * time.Second)
	pipeFinish := now.Add(30 * time.Second)

	input := output.EsAppVersion{
		Created:    types.NewDateTime(now),
		LastUpdate: types.NewDateTime(now),
		Status:     enum.AppVersionStatusEnumUploading,
		Source:     enum.AppVersionSourceEnumCli,
		Build: &output.AppVersionBuild{
			PipelineStart:  types.NewDateTimeNull(pipeStart),
			PipelineFinish: types.NewDateTimeNull(pipeFinish),
		},
	}

	result := mapEsAppVersionEvent(input)

	if result.Build == nil {
		t.Fatal("Build is nil, want non-nil")
	}
	if result.Build.PipelineStart == nil {
		t.Fatal("PipelineStart is nil")
	}
	if !rfc3339Re.MatchString(*result.Build.PipelineStart) {
		t.Errorf("PipelineStart not RFC3339: %q", *result.Build.PipelineStart)
	}
	if *result.Build.PipelineStart != pipeStart.Format(time.RFC3339Nano) {
		t.Errorf("PipelineStart = %q, want %q", *result.Build.PipelineStart, pipeStart.Format(time.RFC3339Nano))
	}
	if result.Build.PipelineFinish == nil {
		t.Fatal("PipelineFinish is nil")
	}
	if !rfc3339Re.MatchString(*result.Build.PipelineFinish) {
		t.Errorf("PipelineFinish not RFC3339: %q", *result.Build.PipelineFinish)
	}
}

func strPtr(s string) *string {
	return &s
}
