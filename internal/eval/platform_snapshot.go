package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

const PlatformSnapshotFormat1 = "zcp-eval-platform-snapshot-1"

type PlatformSnapshot struct {
	FormatVersion        string                       `json:"formatVersion"`
	ProjectID            string                       `json:"projectId"`
	ScenarioStartedAt    time.Time                    `json:"scenarioStartedAt"`
	ObservedAt           time.Time                    `json:"observedAt"`
	Services             []PlatformSnapshotService    `json:"services"`
	Processes            []PlatformSnapshotProcess    `json:"processes"`
	Diagnostics          []PlatformSnapshotDiagnostic `json:"diagnostics"`
	VerificationFindings []VerificationFinding        `json:"verificationFindings"`
}

type PlatformSnapshotService struct {
	ID               string `json:"id"`
	Hostname         string `json:"hostname"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	System           bool   `json:"system"`
	SubdomainEnabled bool   `json:"subdomainEnabled"`
}

type PlatformSnapshotProcess struct {
	ID               string                     `json:"id"`
	Action           string                     `json:"action"`
	Status           string                     `json:"status"`
	Services         []platform.ServiceStackRef `json:"services"`
	Created          string                     `json:"created"`
	Started          string                     `json:"started,omitempty"`
	Finished         string                     `json:"finished,omitempty"`
	AppVersionStatus string                     `json:"appVersionStatus,omitempty"`
	FailureReason    string                     `json:"failureReason,omitempty"`
}

type PlatformSnapshotDiagnostic struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

type platformObservation struct {
	observedAt   time.Time
	services     []platform.ServiceStack
	processes    []platform.Process
	servicesErr  error
	processesErr error
}

func collectPlatformObservation(ctx context.Context, client platform.Client, projectID string, readServices, readProcesses bool) platformObservation {
	observation := platformObservation{}
	if readServices {
		observation.services, observation.servicesErr = client.ListServicesDirect(ctx, projectID)
	}
	if readProcesses {
		observation.processes, observation.processesErr = client.GetProjectProcessesDirect(ctx, projectID)
	}
	observation.observedAt = time.Now().UTC()
	return observation
}

func CollectBehavioralPlatformEvidence(
	ctx context.Context,
	scenario *Scenario,
	projectID string,
	client platform.Client,
	httpDoer ops.HTTPDoer,
	retrospectiveText string,
	runStart time.Time,
) (PlatformSnapshot, []VerificationFinding) {
	observation := collectPlatformObservation(ctx, client, projectID, true, true)
	findings := runVerificationWithObservation(ctx, scenario, observation, httpDoer, retrospectiveText, runStart)
	if findings == nil {
		findings = []VerificationFinding{}
	}

	snapshot := PlatformSnapshot{
		FormatVersion:        PlatformSnapshotFormat1,
		ProjectID:            projectID,
		ScenarioStartedAt:    runStart.UTC(),
		ObservedAt:           observation.observedAt,
		Diagnostics:          []PlatformSnapshotDiagnostic{},
		VerificationFindings: findings,
	}
	if observation.servicesErr != nil {
		snapshot.Diagnostics = append(snapshot.Diagnostics, PlatformSnapshotDiagnostic{
			Source: "ListServicesDirect", Error: observation.servicesErr.Error(),
		})
	} else {
		snapshot.Services = make([]PlatformSnapshotService, 0, len(observation.services))
		for index := range observation.services {
			service := &observation.services[index]
			snapshot.Services = append(snapshot.Services, PlatformSnapshotService{
				ID:               service.ID,
				Hostname:         service.Name,
				Type:             service.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				Status:           service.Status,
				System:           service.IsSystem(),
				SubdomainEnabled: service.SubdomainAccess,
			})
		}
		sort.Slice(snapshot.Services, func(i, j int) bool {
			if snapshot.Services[i].Hostname != snapshot.Services[j].Hostname {
				return snapshot.Services[i].Hostname < snapshot.Services[j].Hostname
			}
			return snapshot.Services[i].ID < snapshot.Services[j].ID
		})
	}
	if observation.processesErr != nil {
		snapshot.Diagnostics = append(snapshot.Diagnostics, PlatformSnapshotDiagnostic{
			Source: "GetProjectProcessesDirect", Error: observation.processesErr.Error(),
		})
	} else {
		snapshot.Processes = make([]PlatformSnapshotProcess, 0, len(observation.processes))
		for _, process := range observation.processes {
			if !processCreatedAfter(process.Created, runStart) {
				continue
			}
			projected := PlatformSnapshotProcess{
				ID:       process.ID,
				Action:   process.ActionName,
				Status:   process.Status,
				Services: append([]platform.ServiceStackRef(nil), process.ServiceStacks...),
				Created:  process.Created,
			}
			if process.Started != nil {
				projected.Started = *process.Started
			}
			if process.Finished != nil {
				projected.Finished = *process.Finished
			}
			if process.AppVersion != nil {
				projected.AppVersionStatus = process.AppVersion.Status
			}
			if process.FailReason != nil {
				projected.FailureReason = *process.FailReason
			}
			sort.Slice(projected.Services, func(i, j int) bool {
				if projected.Services[i].Name != projected.Services[j].Name {
					return projected.Services[i].Name < projected.Services[j].Name
				}
				return projected.Services[i].ID < projected.Services[j].ID
			})
			snapshot.Processes = append(snapshot.Processes, projected)
		}
		sort.Slice(snapshot.Processes, func(i, j int) bool {
			if snapshot.Processes[i].Created != snapshot.Processes[j].Created {
				return snapshot.Processes[i].Created < snapshot.Processes[j].Created
			}
			return snapshot.Processes[i].ID < snapshot.Processes[j].ID
		})
	}
	return snapshot, findings
}

func WritePlatformSnapshot(outputDir string, snapshot PlatformSnapshot) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal platform snapshot: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(outputDir, "platform-snapshot.json"), data, 0o600); err != nil {
		return fmt.Errorf("write platform snapshot: %w", err)
	}
	return nil
}
