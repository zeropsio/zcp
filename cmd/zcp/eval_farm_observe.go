package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// Defaults for `zcp eval farm observe` (docs/spec-eval-farm.md §7.7).
const (
	defaultClaudeBinary = "claude"
	observerTimeout     = 5 * time.Minute
)

// runFarmObserve implements the local verb `zcp eval farm observe <run-dir>
// [--model <m>] [--claude <path>]` (docs/spec-eval-farm.md §7.7): works over
// a pulled bundle, writes <run-dir>/observer/<obsId>.json, prints the
// rendering, and never touches the bucket. Exit 0 when the observation's
// own status is "ok", 1 otherwise (the file is written either way); 2 on
// bad flags.
func runFarmObserve(args []string) int {
	runDir, model, claudePath, err := parseObserveArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	obs := buildObservation(context.Background(), runDir, model, claudePath)

	if err := writeObservation(runDir, obs); err != nil {
		fmt.Fprintf(os.Stderr, "error: write observation: %v\n", err)
		return 1
	}
	fmt.Print(observer.Render(obs))

	if obs.Status == "ok" {
		return 0
	}
	return 1
}

func parseObserveArgs(args []string) (runDir, model, claudePath string, err error) {
	model = observer.DefaultModel
	claudePath = defaultClaudeBinary
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--model":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("%s requires a value", arg)
			}
			model = args[i+1]
			i++
		case "--claude":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("%s requires a value", arg)
			}
			claudePath = args[i+1]
			i++
		default:
			if strings.HasPrefix(arg, "--") {
				return "", "", "", fmt.Errorf("unknown flag %s", arg)
			}
			if runDir != "" {
				return "", "", "", fmt.Errorf("unexpected argument %q", arg)
			}
			runDir = arg
		}
	}
	if runDir == "" {
		return "", "", "", errors.New("a run-dir is required")
	}
	return runDir, model, claudePath, nil
}

// buildObservation is the local verb's thin wrapper over observer.Observe
// (§7.2-§7.5): reads <run-dir>/../summary.json when present (a `farm pull
// --batch` layout, §7.5), then delegates the whole pipeline to Observe.
func buildObservation(ctx context.Context, runDir, model, claudePath string) observer.Observation {
	runID := filepath.Base(runDir)
	bundle := observer.NewDirBundle(runDir)
	return observer.Observe(ctx, bundle, observer.ObserveConfig{
		RunID:       runID,
		Model:       model,
		ClaudePath:  claudePath,
		OAuthToken:  os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"),
		Timeout:     observerTimeout,
		Environ:     os.Environ,
		SummaryJSON: readSummaryJSON(runDir),
		Source:      observer.SourceLocal,
	})
}

// readSummaryJSON reads <run-dir>/../summary.json (a `farm pull --batch`
// layout, §7.5), returning nil when it is absent — the caller then falls
// back to meta.json.task.result.
func readSummaryJSON(runDir string) []byte {
	summaryPath := filepath.Join(filepath.Dir(runDir), "summary.json")
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		return nil
	}
	return data
}

func writeObservation(runDir string, obs observer.Observation) error {
	dir := filepath.Join(runDir, "observer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, obs.ObsID+".json")
	data, err := json.MarshalIndent(obs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
