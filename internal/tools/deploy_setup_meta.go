package tools

import (
	"errors"
	"fmt"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func recordResolvedDeploySetupMeta(stateDir, targetHostname, setupName string, servesHTTP bool) error {
	if stateDir == "" || targetHostname == "" {
		return nil
	}
	err := workflow.UpdateServiceMeta(stateDir, targetHostname, func(m *workflow.ServiceMeta) error {
		changed := false
		_, isStage := m.StageFor(targetHostname)
		switch {
		case isStage:
			if setupName != "" && m.SetStageSetup(targetHostname, setupName) {
				changed = true
			}
		case targetHostname == m.Hostname:
			if setupName != "" && m.PrimarySetupName != setupName {
				m.PrimarySetupName = setupName
				changed = true
			}
		default:
			return workflow.ErrSkipWrite
		}
		if m.SetServesHTTP(targetHostname, servesHTTP) {
			changed = true
		}
		if !changed {
			return workflow.ErrSkipWrite
		}
		return nil
	})
	if errors.Is(err, workflow.ErrServiceMetaNotFound) {
		return nil
	}
	return err
}

func resolveDeploySetupEntryFromContent(stateDir, targetHostname, setup, yamlContent string) (*ops.ZeropsYmlEntry, string, error) {
	if yamlContent == "" {
		return nil, setup, nil
	}
	doc, err := ops.ParseZeropsYmlContent([]byte(yamlContent), "zerops.yaml")
	if err != nil {
		return nil, setup, err
	}
	role := topology.DeployRoleDev
	if meta, metaErr := workflow.FindServiceMeta(stateDir, targetHostname); metaErr == nil && meta != nil {
		if setup == "" {
			setup = meta.SetupNameFor(targetHostname)
		}
		if resolvedRole := meta.RoleFor(targetHostname); resolvedRole != "" {
			role = resolvedRole
		} else {
			role = meta.PrimaryRole()
		}
	}
	entry := resolveSetupEntry(doc, setup, role, targetHostname)
	if entry == nil {
		return nil, setup, nil
	}
	return entry, entry.Setup, nil
}

func recordDeploySetupMetaFromContent(stateDir, targetHostname, setup, yamlContent string) (string, error) {
	entry, resolvedSetup, err := resolveDeploySetupEntryFromContent(stateDir, targetHostname, setup, yamlContent)
	if err != nil || entry == nil {
		return resolvedSetup, err
	}
	if err := recordResolvedDeploySetupMeta(stateDir, targetHostname, entry.Setup, entry.HasPorts()); err != nil {
		return resolvedSetup, fmt.Errorf("record deployed setup metadata: %w", err)
	}
	return resolvedSetup, nil
}
