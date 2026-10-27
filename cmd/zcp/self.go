package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
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
// writes its one answer to stdout: under --json `{"processId":"…"}` with exit
// 0, else `{"error":"…"}` with exit 1 — one line the Mate shows a person as
// the reason — plus `"refused":true` when the restart certainly did not
// happen (definiteRefusal), so the Mate need not wait out its grace.

const selfUsage = "usage: zcp self restart [--json]"

// selfRestartTimeout bounds the one API call the restart is.
const selfRestartTimeout = 30 * time.Second

// serviceRestarter is the one platform call `zcp self restart` makes.
type serviceRestarter interface {
	RestartService(ctx context.Context, serviceID string) (*platform.Process, error)
}

// selfRestartAnswer is `zcp self restart`'s answer: the process of the
// restart the platform accepted, or, when Error is set, why there is none —
// Refused when it certainly did not happen. Exactly one of ProcessID and
// Error is written (printSelfRestart).
type selfRestartAnswer struct {
	ProcessID string
	Error     string
	Refused   bool
}

// runSelfCmd implements `zcp self restart`. args is everything after "self".
// lookup reads the container's environment — its live env store, since the
// Mate server's own environment carries neither the service nor the key —
// and newClient builds the platform client from the project key and API
// host, as `zcp lease release` does (resolveLiveLeaseDeps).
func runSelfCmd(args []string, stdout io.Writer, lookup func(string) string, newClient func(token, apiHost string) (serviceRestarter, error)) int {
	asJSON := slices.Contains(args, "--json")
	fail := func(msg string, refused bool) int {
		printSelfRestart(stdout, selfRestartAnswer{Error: strings.Join(strings.Fields(msg), " "), Refused: refused}, "", asJSON)
		return 1
	}
	if len(args) == 0 || args[0] != "restart" {
		if slices.ContainsFunc(args, isHelpArg) {
			_, _ = fmt.Fprintln(stdout, selfUsage)
			return 0
		}
		return fail(selfUsage, true)
	}
	if _, code, done := commandFlags(selfUsage, args[1:], "--json"); done {
		return code
	}

	rt := runtime.DetectFrom(lookup)
	if !rt.InContainer || rt.ServiceID == "" {
		return fail("not running in a Zerops container, so there is no service to restart", true)
	}
	key := lookup(hq.EnvKey)
	if key == "" {
		return fail(fmt.Sprintf("this container has no %s, which the platform needs to restart it", hq.EnvKey), true)
	}
	client, err := newClient(key, mate.ResolveAPIHost(lookup("ZCP_API_HOST")))
	if err != nil {
		return fail(fmt.Sprintf("could not reach the platform: %v", err), true)
	}
	ctx, cancel := context.WithTimeout(context.Background(), selfRestartTimeout)
	defer cancel()
	proc, err := client.RestartService(ctx, rt.ServiceID)
	if err != nil {
		if definiteRefusal(err) {
			return fail(fmt.Sprintf("the platform refused the restart: %v", err), true)
		}
		return fail(fmt.Sprintf("could not confirm the restart: %v", err), false)
	}
	var answer selfRestartAnswer
	if proc != nil {
		answer.ProcessID = proc.ID
	}
	printSelfRestart(stdout, answer, rt.ServiceID, asJSON)
	return 0
}

// definiteRefusal reports whether the platform's error proves it did not
// take the restart: its own 4xx answer — the key (401, 403), the service
// (404), the rate limit (429), or a rejection carrying the API's error code.
// A timeout, a dropped connection, the platform's 5xx (platform.IsTransient)
// and an error zcp cannot classify are not: the restart may have happened.
func definiteRefusal(err error) bool {
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		return false
	}
	switch pe.Code {
	case platform.ErrAuthTokenExpired, platform.ErrPermissionDenied, platform.ErrServiceNotFound, platform.ErrAPIRateLimited:
		return true
	case platform.ErrAPIError:
		return pe.APICode != "" && !platform.IsTransient(err)
	}
	return false
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

// printSelfRestart writes answer to stdout: one JSON line under --json —
// `{"error":"…"}` or `{"processId":"…"}`, nothing else — else one line of
// text.
func printSelfRestart(stdout io.Writer, answer selfRestartAnswer, serviceID string, asJSON bool) {
	if asJSON {
		var data []byte
		var err error
		if answer.Error != "" {
			data, err = json.Marshal(struct {
				Error   string `json:"error"`
				Refused bool   `json:"refused,omitempty"`
			}{answer.Error, answer.Refused})
		} else {
			data, err = json.Marshal(struct {
				ProcessID string `json:"processId"`
			}{answer.ProcessID})
		}
		if err != nil {
			data = []byte(`{"error":"could not write the answer"}`)
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
