package analyze

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/analyze"
)

const generateChecklistUsage = `Usage: zcp analyze generate-checklist <machine-report.json> [flags]

Flags:
  --out <file>   Write the checklist Markdown here. Default: stdout.`

func runGenerateChecklist(args []string) {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprintln(os.Stderr, generateChecklistUsage)
		if len(args) == 0 {
			os.Exit(1)
		}
		return
	}
	known := map[string]bool{"--out": true}
	positional, flags, err := parseFlags(args, known)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, generateChecklistUsage)
		os.Exit(1)
	}
	reportPath, err := filepath.Abs(positional[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read %s: %v\n", reportPath, err)
		os.Exit(2)
	}
	var report analyze.MachineReport
	if err := json.Unmarshal(data, &report); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse %s: %v\n", reportPath, err)
		os.Exit(2)
	}
	sha, err := analyze.Sha256File(reportPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: hash %s: %v\n", reportPath, err)
		os.Exit(2)
	}
	var buf bytes.Buffer
	if err := analyze.ChecklistFromReport(&buf, &report, sha); err != nil {
		fmt.Fprintf(os.Stderr, "error: render checklist: %v\n", err)
		os.Exit(2)
	}
	out := flags["--out"]
	if out == "" {
		_, _ = os.Stdout.Write(buf.Bytes())
		return
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: mkdir: %v\n", err)
		os.Exit(2)
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", out, err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "checklist written: %s\n", out)
}
