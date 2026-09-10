package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/zeropsio/zcp/internal/eval"
)

// behavioralReportOptions is the parsed `eval behavioral report` flag set
// (docs/spec-capture-inspector.md §8.5, docs/spec-testing-architecture.md
// §10.5).
type behavioralReportOptions struct {
	Capture  string
	EvalID   string
	Scenario string
	Format   string
}

func parseBehavioralReportArgs(args []string) (behavioralReportOptions, error) {
	options := behavioralReportOptions{Format: "text"}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		readValue := func(name string) (string, error) {
			if index+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			index++
			return args[index], nil
		}
		var err error
		switch arg {
		case "--capture":
			options.Capture, err = readValue(arg)
		case "--eval":
			options.EvalID, err = readValue(arg)
		case "--scenario":
			options.Scenario, err = readValue(arg)
		case "--format":
			options.Format, err = readValue(arg)
		default:
			return behavioralReportOptions{}, fmt.Errorf("unknown flag %s", arg)
		}
		if err != nil {
			return behavioralReportOptions{}, err
		}
	}
	if options.Capture == "" {
		return behavioralReportOptions{}, errors.New("--capture <dir> is required")
	}
	if options.EvalID == "" {
		return behavioralReportOptions{}, errors.New("--eval <id> is required")
	}
	if options.Scenario == "" {
		return behavioralReportOptions{}, errors.New("--scenario <id> is required")
	}
	if options.Format != "text" && options.Format != captureInspectFormatJSON {
		return behavioralReportOptions{}, fmt.Errorf("unknown format %q", options.Format)
	}
	return options, nil
}

// runBehavioralReport implements `zcp eval behavioral report`
// (docs/spec-capture-inspector.md §8.5): a read-only projection of one
// finalized scenario run's frozen evidence. It never calls initEvalRunner,
// auth, platform, provider, or a capture manager — the window is the only
// input.
func runBehavioralReport(args []string, stdout, stderr io.Writer) int {
	options, err := parseBehavioralReportArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "eval behavioral report: %v\n", err)
		return 2
	}
	report, inspection, err := eval.BuildBehavioralReport(options.Capture, options.EvalID, options.Scenario)
	if err != nil {
		fmt.Fprintf(stderr, "eval behavioral report: %v\n", err)
		return 1
	}
	if options.Format == captureInspectFormatJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(stderr, "eval behavioral report: encode JSON: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprint(stdout, eval.RenderBehavioralReportText(report))
	}
	if !inspection.Integrity.Valid {
		return 1
	}
	if !inspection.Integrity.Complete {
		return 1
	}
	return 0
}
