package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// EnvGetResult contains the result of an env get operation.
type EnvGetResult struct {
	Scope    string           `json:"scope"`
	Hostname string           `json:"serviceHostname,omitempty"`
	Vars     []map[string]any `json:"vars"`
}

// EnvSetResult contains the result of an env set operation.
type EnvSetResult struct {
	Process *platform.Process `json:"process,omitempty"`
}

// EnvDeleteResult contains the result of an env delete operation.
type EnvDeleteResult struct {
	Process *platform.Process `json:"process,omitempty"`
}

// EnvGet retrieves environment variables for a service or project.
func EnvGet(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	isProject bool,
) (*EnvGetResult, error) {
	if hostname == "" && !isProject {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"Provide serviceHostname or set project=true",
			"Specify which scope to read env vars from")
	}

	if isProject {
		envs, err := client.GetProjectEnv(ctx, projectID)
		if err != nil {
			return nil, err
		}
		return &EnvGetResult{
			Scope: "project",
			Vars:  envVarsToMaps(envs),
		}, nil
	}

	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}

	envs, err := client.GetServiceEnv(ctx, svc.ID)
	if err != nil {
		return nil, err
	}

	return &EnvGetResult{
		Scope:    "service",
		Hostname: hostname,
		Vars:     envVarsToMaps(envs),
	}, nil
}

// EnvSet sets environment variables for a service or project.
func EnvSet(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	isProject bool,
	variables []string,
) (*EnvSetResult, error) {
	if hostname == "" && !isProject {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"Provide serviceHostname or set project=true", "")
	}

	pairs, err := parseEnvPairs(variables)
	if err != nil {
		return nil, err
	}

	if isProject {
		var lastProc *platform.Process
		for _, p := range pairs {
			proc, setErr := client.CreateProjectEnv(ctx, projectID, p.Key, p.Value, false)
			if setErr != nil {
				return nil, setErr
			}
			lastProc = proc
		}
		return &EnvSetResult{Process: lastProc}, nil
	}

	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}

	content := buildEnvFileContent(pairs)
	proc, err := client.SetServiceEnvFile(ctx, svc.ID, content)
	if err != nil {
		return nil, err
	}

	return &EnvSetResult{Process: proc}, nil
}

// EnvDelete deletes environment variables from a service or project.
func EnvDelete(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	isProject bool,
	variables []string,
) (*EnvDeleteResult, error) {
	if hostname == "" && !isProject {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"Provide serviceHostname or set project=true", "")
	}

	if isProject {
		envs, err := client.GetProjectEnv(ctx, projectID)
		if err != nil {
			return nil, err
		}
		var lastProc *platform.Process
		for _, key := range variables {
			envID := findEnvIDByKey(envs, key)
			if envID == "" {
				return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
					fmt.Sprintf("Environment variable '%s' not found", key), "")
			}
			proc, delErr := client.DeleteProjectEnv(ctx, envID)
			if delErr != nil {
				return nil, delErr
			}
			lastProc = proc
		}
		return &EnvDeleteResult{Process: lastProc}, nil
	}

	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}

	envs, err := client.GetServiceEnv(ctx, svc.ID)
	if err != nil {
		return nil, err
	}

	var lastProc *platform.Process
	for _, key := range variables {
		envID := findEnvIDByKey(envs, key)
		if envID == "" {
			return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Environment variable '%s' not found", key), "")
		}
		proc, delErr := client.DeleteUserData(ctx, envID)
		if delErr != nil {
			return nil, delErr
		}
		lastProc = proc
	}

	return &EnvDeleteResult{Process: lastProc}, nil
}

func envVarsToMaps(envs []platform.EnvVar) []map[string]any {
	result := make([]map[string]any, len(envs))
	for i, e := range envs {
		result[i] = map[string]any{
			"key":   e.Key,
			"value": e.Content,
		}
	}
	return result
}

func buildEnvFileContent(pairs []envPair) string {
	var b strings.Builder
	for _, p := range pairs {
		b.WriteString(p.Key)
		b.WriteByte('=')
		b.WriteString(p.Value)
		b.WriteByte('\n')
	}
	return b.String()
}
