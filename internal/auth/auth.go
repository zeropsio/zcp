// Package auth resolves Zerops authentication from env vars or zcli's cli.data file.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	defaultAPIHost = "api.app-prg1.zerops.io"
	defaultRegion  = "prg1"
	cliDataFile    = "cli.data"
)

// Info holds resolved authentication and project context.
type Info struct {
	Token       string
	APIHost     string
	Region      string
	ClientID    string
	ProjectID   string
	ProjectName string
}

// cliData is the JSON schema for zcli's cli.data file.
type cliData struct {
	Token          string    `json:"Token"`          //nolint:tagliatelle // matches zcli API response format
	RegionData     cliRegion `json:"RegionData"`     //nolint:tagliatelle // matches zcli API response format
	ScopeProjectID *string   `json:"ScopeProjectId"` //nolint:tagliatelle // matches zcli API response format
}

// cliRegion is the region section of cli.data.
type cliRegion struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// Resolve authenticates against Zerops and discovers the active project.
//
// Resolution order:
//  1. ZCP_API_KEY env var (primary)
//  2. zcli fallback — read cli.data from OS config directory
//
// Both paths validate via client.GetUserInfo and discover the project.
func Resolve(ctx context.Context, client platform.Client) (*Info, error) {
	token, apiHost, region, scopeProjectID, err := resolveCredentials()
	if err != nil {
		return nil, err
	}

	// Validate token by fetching user info.
	userInfo, err := client.GetUserInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Discover project.
	projectID, projectName, err := discoverProject(ctx, client, userInfo.ID, scopeProjectID)
	if err != nil {
		return nil, err
	}

	return &Info{
		Token:       token,
		APIHost:     apiHost,
		Region:      region,
		ClientID:    userInfo.ID,
		ProjectID:   projectID,
		ProjectName: projectName,
	}, nil
}

// Credentials holds raw connection info needed to create a platform client.
type Credentials struct {
	Token   string
	APIHost string
	Region  string
}

// ResolveCredentials reads token and connection info from env vars or cli.data
// without contacting the API. Use this to bootstrap a platform.Client before
// calling Resolve for full validation.
func ResolveCredentials() (*Credentials, error) {
	token, apiHost, region, _, err := resolveCredentials()
	if err != nil {
		return nil, err
	}
	return &Credentials{Token: token, APIHost: apiHost, Region: region}, nil
}

// resolveCredentials reads token and connection info from env vars or cli.data.
func resolveCredentials() (token, apiHost, region string, scopeProjectID *string, err error) {
	// Primary path: ZCP_API_KEY env var.
	token = os.Getenv("ZCP_API_KEY")
	if token != "" {
		apiHost = envOrDefault("ZCP_API_HOST", defaultAPIHost)
		region = envOrDefault("ZCP_REGION", defaultRegion)
		return token, apiHost, region, nil, nil
	}

	// Fallback: read zcli's cli.data.
	data, readErr := readCliData()
	if readErr != nil {
		return "", "", "", nil, platform.NewPlatformError(
			platform.ErrAuthRequired,
			"No authentication found: set ZCP_API_KEY or log in with zcli",
			"Export ZCP_API_KEY=<your-token> or run: zcli login <token>",
		)
	}

	if data.Token == "" {
		return "", "", "", nil, platform.NewPlatformError(
			platform.ErrAuthRequired,
			"cli.data found but token is empty",
			"Run: zcli login <token>",
		)
	}

	// cli.data values, with env var overrides.
	apiHost = envOrDefault("ZCP_API_HOST", data.RegionData.Address)
	if apiHost == "" {
		apiHost = defaultAPIHost
	}
	region = envOrDefault("ZCP_REGION", data.RegionData.Name)
	if region == "" {
		region = defaultRegion
	}

	return data.Token, apiHost, region, data.ScopeProjectID, nil
}

// discoverProject finds the active project for the authenticated user.
func discoverProject(ctx context.Context, client platform.Client, clientID string, scopeProjectID *string) (string, string, error) {
	// If zcli has a scoped project, use it directly.
	if scopeProjectID != nil && *scopeProjectID != "" {
		proj, err := client.GetProject(ctx, *scopeProjectID)
		if err != nil {
			return "", "", fmt.Errorf("get scoped project: %w", err)
		}
		return proj.ID, proj.Name, nil
	}

	// Otherwise list projects and require exactly one.
	projects, err := client.ListProjects(ctx, clientID)
	if err != nil {
		return "", "", fmt.Errorf("list projects: %w", err)
	}

	switch len(projects) {
	case 0:
		return "", "", platform.NewPlatformError(
			platform.ErrTokenNoProject,
			"Token has no project access",
			"Use a project-scoped token or grant project access",
		)
	case 1:
		return projects[0].ID, projects[0].Name, nil
	default:
		return "", "", platform.NewPlatformError(
			platform.ErrTokenMultiProject,
			fmt.Sprintf("Token accesses %d projects; use project-scoped token", len(projects)),
			"Create a project-scoped token in Zerops GUI or set project via zcli scope",
		)
	}
}

// readCliData reads and parses zcli's cli.data file.
func readCliData() (*cliData, error) {
	path, err := cliDataPath()
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var data cliData
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("parse cli.data: %w", err)
	}
	return &data, nil
}

// cliDataPath returns the OS-specific path to zcli's cli.data file.
// Respects ZCP_ZCLI_DATA_DIR env var for testing.
func cliDataPath() (string, error) {
	// Test override.
	if dir := os.Getenv("ZCP_ZCLI_DATA_DIR"); dir != "" {
		return filepath.Join(dir, "zerops", cliDataFile), nil
	}

	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "zerops", cliDataFile), nil
	default:
		// Linux / other: use XDG_CONFIG_HOME or ~/.config
		configDir := os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve home dir: %w", err)
			}
			configDir = filepath.Join(home, ".config")
		}
		return filepath.Join(configDir, "zerops", cliDataFile), nil
	}
}

// envOrDefault returns the env var value or the fallback if empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
