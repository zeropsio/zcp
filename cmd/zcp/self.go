package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// `zcp self restart [--json]` asks the platform to restart the service this
// container is — the zcp service. The restart is the whole update of a Mate:
// run.initCommands install the latest zcp release, then `zcp init` installs
// the stable Mate (docs/spec-mate.md §2.9, MA-6). The Mate server runs it
// while idle when `zcp mate status` says restartNeeded. It answers as soon as
// the platform accepted the restart and never waits for its process: the
// restart stops this container, this process with it. It reads no stdin and
// writes its one answer to stdout; exit 0 accepted, 1 anything else.

const selfUsage = "usage: zcp self restart [--json]"

// selfRestartTimeout bounds the one API call the restart is.
const selfRestartTimeout = 30 * time.Second

// serviceRestarter is the one platform call `zcp self restart` makes.
type serviceRestarter interface {
	RestartService(ctx context.Context, serviceID string) (*platform.Process, error)
}

// selfRestartAnswer is `zcp self restart --json`'s answer: the restart the
// platform accepted and its process, or why there is none.
type selfRestartAnswer struct {
	Restarting bool   `json:"restarting"`
	ProcessID  string `json:"processId,omitempty"`
	Error      string `json:"error,omitempty"`
}

// runSelfCmd implements `zcp self restart`. args is everything after "self".
// lookup reads the container's environment — its live env store, since the
// Mate server's own environment carries neither the service nor the key —
// and newClient builds the platform client from the project key and API
// host, as `zcp lease release` does (resolveLiveLeaseDeps).
func runSelfCmd(args []string, stdout io.Writer, lookup func(string) string, newClient func(token, apiHost string) (serviceRestarter, error)) int {
	asJSON := slices.Contains(args, "--json")
	fail := func(msg string) int {
		printSelfRestart(stdout, selfRestartAnswer{Error: msg}, "", asJSON)
		return 1
	}
	if len(args) == 0 || args[0] != "restart" {
		if slices.ContainsFunc(args, isHelpArg) {
			_, _ = fmt.Fprintln(stdout, selfUsage)
			return 0
		}
		return fail(selfUsage)
	}
	if _, code, done := commandFlags(selfUsage, args[1:], "--json"); done {
		return code
	}

	rt := runtime.DetectFrom(lookup)
	if !rt.InContainer || rt.ServiceID == "" {
		return fail("zcp self restart runs inside a Zerops container: it restarts the service it runs in, and no serviceId names one here")
	}
	key := lookup(hq.EnvKey)
	if key == "" {
		return fail(fmt.Sprintf("no %s in this container's environment: the platform takes the restart only with the project's key", hq.EnvKey))
	}
	client, err := newClient(key, mate.ResolveAPIHost(lookup("ZCP_API_HOST")))
	if err != nil {
		return fail(fmt.Sprintf("build the platform client: %v", err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), selfRestartTimeout)
	defer cancel()
	proc, err := client.RestartService(ctx, rt.ServiceID)
	if err != nil {
		return fail(fmt.Sprintf("restart service %s: %v", rt.ServiceID, err))
	}
	answer := selfRestartAnswer{Restarting: true}
	if proc != nil {
		answer.ProcessID = proc.ID
	}
	printSelfRestart(stdout, answer, rt.ServiceID, asJSON)
	return 0
}

// newSelfRestartClient builds the platform client the way `zcp lease
// release` does, without the project read a restart does not need.
func newSelfRestartClient(token, apiHost string) (serviceRestarter, error) {
	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		return nil, fmt.Errorf("platform client: %w", err)
	}
	return client, nil
}

// printSelfRestart writes answer to stdout: JSON under --json, else one line.
func printSelfRestart(stdout io.Writer, answer selfRestartAnswer, serviceID string, asJSON bool) {
	if asJSON {
		data, err := json.Marshal(answer)
		if err != nil {
			data = []byte(`{"restarting":false,"error":"marshal the answer"}`)
		}
		_, _ = fmt.Fprintln(stdout, string(data))
		return
	}
	if answer.Error != "" {
		_, _ = fmt.Fprintln(stdout, "self restart: "+answer.Error)
		return
	}
	_, _ = fmt.Fprintf(stdout, "self restart: the platform is restarting service %s (process %s); this container stops in a moment\n", serviceID, answer.ProcessID)
}
