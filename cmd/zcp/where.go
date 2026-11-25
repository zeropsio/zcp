package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
)

const whereBudget = 3 * time.Second

func runWhereCmd(args []string) int {
	started := time.Now()
	ctx, cancel := context.WithDeadline(context.Background(), started.Add(whereBudget))
	defer cancel()
	jsonOutput := false
	// A malformed flag still returns JSON when the caller asked for JSON.
	for _, arg := range args {
		if arg == "--json" || arg == "-json" || strings.HasPrefix(arg, "--json=") || strings.HasPrefix(arg, "-json=") {
			jsonOutput = true
		}
	}
	flags := flag.NewFlagSet("where", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	alias := flags.String("lease", "", "")
	asJSON := flags.Bool("json", false, "")
	if err := flags.Parse(args); err != nil {
		return whereError(jsonOutput, fmt.Errorf("where: invalid flags"))
	}
	jsonOutput = *asJSON
	if flags.NArg() != 0 {
		return whereError(jsonOutput, fmt.Errorf("where: unexpected argument"))
	}
	rt := runtime.DetectFrom(mate.LiveLookup(mate.LiveEnvStorePath))
	if !rt.InContainer {
		return whereError(jsonOutput, fmt.Errorf("where runs inside the project's zcp container"))
	}
	if rt.ProjectID == "" {
		return whereError(jsonOutput, fmt.Errorf("where: no project in this container"))
	}
	if mate.LiveLookup(mate.LiveEnvStorePath)(hq.EnvKey) == "" {
		return whereError(jsonOutput, fmt.Errorf("where: no project key in this container"))
	}
	deps, err := resolveLiveLeaseDeps(ctx)
	if err != nil {
		return whereError(jsonOutput, fmt.Errorf("where: platform did not provide the project"))
	}
	client, ok := deps.Client.(*platform.ZeropsClient)
	if !ok {
		return whereError(jsonOutput, fmt.Errorf("where: no platform client"))
	}
	answer, err := tools.WhereAnswer(ctx, deps, rt, *alias, started, client.ActiveAppVersionName)
	if err != nil {
		return whereError(jsonOutput, err)
	}
	if jsonOutput {
		whereJSON(struct {
			Where  string `json:"where"`
			ReadAt string `json:"readAt"`
			TookMs int64  `json:"tookMs"`
		}{answer, started.UTC().Format(time.RFC3339), time.Since(started).Milliseconds()})
	} else {
		fmt.Fprintln(os.Stdout, answer)
	}
	return 0
}

func whereError(asJSON bool, err error) int {
	if asJSON {
		whereJSON(struct {
			Error string `json:"error"`
		}{err.Error()})
	} else {
		fmt.Fprintln(os.Stderr, err)
	}
	return 1
}

func whereJSON(answer any) {
	data, err := json.Marshal(answer)
	if err != nil {
		fmt.Fprintln(os.Stdout, `{"error":"could not encode answer"}`)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}
