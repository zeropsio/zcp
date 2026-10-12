package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// macStatusTimeout bounds one `mac status --json`: it reaches the Mac over
// ssh and asks macOS for the agent-device daemon's permissions, so a Mac
// that is asleep or off the VPN must not hold the call open.
const macStatusTimeout = 90 * time.Second

// macStatusWaitDelay bounds the wait for `mac`'s output pipes after it is
// killed, so a child it left running (its ssh) cannot hold the call.
const macStatusWaitDelay = 2 * time.Second

// The stages `mac status --json` reports (docs/spec-mate.md §2.10).
const (
	macStageNotPaired        = "not-paired"
	macStageUnreachable      = "unreachable"
	macStageNeedsPermissions = "needs-permissions"
	macStageReady            = "ready"
)

const macPairAgain = `When the user says they did it, call zerops_workflow action="mac-pair" again: it checks the Mac and says the next step.`

// macStatus is the object `mac status --json` prints. Mate owns the shape
// and the commands in it; zcp only reads it and repeats the commands.
type macStatus struct {
	Stage         string   `json:"stage"`
	PairCommands  []string `json:"pairCommands"`
	RemoveCommand string   `json:"removeCommand"`
	Mac           *macHost `json:"mac"`
	Missing       []string `json:"missing"`
}

type macHost struct {
	Name        string  `json:"name"`
	User        string  `json:"user"`
	Address     string  `json:"address"`
	Port        int     `json:"port"`
	MacOS       *string `json:"macos"`
	AgentDevice *string `json:"agentDevice"`
	Node        *string `json:"node"`
}

// macPairResult is mac-pair's answer: the Mate's status as read, and the
// next step in words the agent relays to the user.
type macPairResult struct {
	Stage         string   `json:"stage"`
	Mac           *macHost `json:"mac,omitempty"`
	Missing       []string `json:"missing,omitempty"`
	PairCommands  []string `json:"pairCommands"`
	RemoveCommand string   `json:"removeCommand"`
	Message       string   `json:"message"`
}

// handleMacPair is zerops_workflow action="mac-pair": zcp's guidance for
// pairing the user's Mac with this Mate, and for fixing a pairing that
// stopped working. It keeps no state — every call re-reads the Mate's own
// `mac status --json`, so the answer is always the next step from where the
// Mac is now.
//
// The guidance is rendered here rather than in atoms: atoms are selected by
// the lifecycle envelope's axes, and a Mac's pairing stage is not part of the
// envelope (it is probed per call), while every step names values only this
// call reads — the Mate's commands, the Mac's name, the missing permissions
// and node's path on the Mac.
func handleMacPair(ctx context.Context, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	return macPair(ctx, rt, macStatusTimeout)
}

func macPair(ctx context.Context, rt runtime.Info, timeout time.Duration) (*mcp.CallToolResult, any, error) {
	if !rt.MateEnabled {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"Pairing a Mac is a Zerops Mate's — this is not a Mate, so there is no Mac to pair with here.",
			"Nothing else here depends on it.",
		)), nil, nil
	}
	path, found := macOnPath()
	if !found {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"This Mate predates Mac pairing: it has no `mac` command yet.",
			"Mac pairing arrives with a Mate update — tell the user it is not available on this Mate until it is updated.",
		)), nil, nil
	}
	status, refusal := readMacStatus(ctx, path, timeout)
	if refusal != nil {
		return refusal, nil, nil
	}
	return jsonResult(macPairResult{
		Stage:         status.Stage,
		Mac:           status.Mac,
		Missing:       status.Missing,
		PairCommands:  status.PairCommands,
		RemoveCommand: status.RemoveCommand,
		Message:       macPairMessage(status),
	}), nil, nil
}

// macOnPath finds the Mate's `mac` command. Its presence is the capability:
// a Mate that ships `mac` answers `mac status --json`.
func macOnPath() (string, bool) {
	path, err := exec.LookPath("mac")
	return path, err == nil
}

// readMacStatus runs `mac status --json` and reads its object. Anything but
// a zero exit with one object of a known stage is `mac` failing, reported
// with what it printed — never guessed into a stage.
func readMacStatus(ctx context.Context, path string, timeout time.Duration) (macStatus, *mcp.CallToolResult) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, path, "status", "--json")
	cmd.WaitDelay = macStatusWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	failed := func(msg string) *mcp.CallToolResult {
		pe := platform.NewPlatformError(platform.ErrUnknown, msg,
			"This is the Mate's own `mac` command failing, not a step for the user: say so, with what it printed, and call zerops_workflow action=\"mac-pair\" again later.")
		pe.Diagnostic = strings.TrimSpace(stderr.String())
		return convertError(pe)
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return macStatus{}, failed(fmt.Sprintf("`mac status --json` did not answer within %s.", timeout))
	}
	if runErr != nil {
		return macStatus{}, failed(fmt.Sprintf("`mac status --json` failed (%v): %s", runErr, strings.TrimSpace(stderr.String())))
	}
	var status macStatus
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &status); err != nil {
		return macStatus{}, failed(fmt.Sprintf("`mac status --json` printed something that is not its status object (%v).", err))
	}
	if problem := macStatusProblem(status); problem != "" {
		return macStatus{}, failed(fmt.Sprintf("`mac status --json` printed a status zcp cannot read: %s.", problem))
	}
	return status, nil
}

// macStatusProblem names what makes a status object unusable for guidance.
func macStatusProblem(s macStatus) string {
	switch s.Stage {
	case macStageNotPaired:
	case macStageUnreachable, macStageNeedsPermissions, macStageReady:
		if s.Mac == nil {
			return fmt.Sprintf("stage %q without the paired Mac", s.Stage)
		}
	default:
		return fmt.Sprintf("unknown stage %q", s.Stage)
	}
	if len(s.PairCommands) == 0 || s.RemoveCommand == "" {
		return "no pairing commands"
	}
	return ""
}

// macPairMessage is the next step for the stage, written for the agent to
// relay to a user sitting at their Mac's Terminal, who may not be a
// developer.
func macPairMessage(s macStatus) string {
	pair := s.PairCommands[len(s.PairCommands)-1]
	switch s.Stage {
	case macStageNotPaired:
		return "No Mac is paired with this Mate yet. Pairing happens on the user's Mac, so walk them through it one step at a time. " +
			"The Mac needs zcli installed and signed in (`zcli login`) and Node.js (`brew install node`, or the installer from nodejs.org). " +
			"Then, in Terminal on the Mac, run in order:\n" + macCommandList(s.PairCommands) + "\n" +
			"`" + s.PairCommands[0] + "` connects the Mac to this project's VPN. It asks for the Mac's password, so only the user can run it — ZCP cannot start the VPN for them — and the VPN then stays connected on its own. " +
			"`" + pair + "` installs agent-device and starts, as the user and without admin rights, a private ssh server reachable only over the project VPN, plus the agent-device daemon. At the end macOS may ask to allow `node`: tell the user to allow it.\n" +
			macPairAgain
	case macStageUnreachable:
		return macName(s.Mac) + " is paired with this Mate but does not answer. Ask the user to make sure the Mac is awake and logged in, and that its VPN is connected — running `" + s.PairCommands[0] + "` in Terminal on the Mac connects it. " +
			"If it still does not answer, its VPN address may have changed: run the pairing again in Terminal on the Mac:\n" + macCommandList(s.PairCommands) + "\n" +
			macPairAgain
	case macStageNeedsPermissions:
		missing := s.Missing
		if len(missing) == 0 {
			missing = []string{"Accessibility", "Screen Recording"}
		}
		find := "If `node` is not listed, click +, press Cmd-Shift-G, paste the path `which node` prints in Terminal, and choose it."
		if s.Mac.Node != nil && *s.Mac.Node != "" {
			find = "If `node` is not listed, click +, press Cmd-Shift-G, paste `" + *s.Mac.Node + "` and choose it."
		}
		permissions := strings.Join(missing, " and ") + " permission"
		if len(missing) > 1 {
			permissions += "s"
		}
		return macName(s.Mac) + " is paired and answers, but macOS has not yet given the agent-device daemon (`node`) the " + permissions + ". " +
			"Ask the user to open System Settings > Privacy & Security and, under " + strings.Join(missing, " and under ") + ", turn on `node`. " + find + " " +
			"Or run `" + pair + "` in Terminal on the Mac again: it opens the settings and waits until they are allowed.\n" +
			macPairAgain
	default: // ready
		name := macName(s.Mac)
		if s.Mac.MacOS != nil && *s.Mac.MacOS != "" {
			name += " (macOS " + *s.Mac.MacOS + ")"
		}
		return name + " is paired with this Mate and ready. Work on it with `mac`: `mac sync` copies the working directory to the Mac, `mac run <command>` runs a command in that copy (build and launch the app), " +
			"`mac device <agent-device args>` drives the running app, `mac screenshot` brings a picture of it back, and `mac help` lists the rest. " +
			"To unpair, run `mac unpair` here, and `" + s.RemoveCommand + "` in Terminal on the Mac removes what pairing installed there."
	}
}

func macName(m *macHost) string {
	if m == nil || m.Name == "" {
		return "The Mac"
	}
	return m.Name
}

func macCommandList(cmds []string) string {
	lines := make([]string, len(cmds))
	for i, c := range cmds {
		lines[i] = fmt.Sprintf("%d. `%s`", i+1, c)
	}
	return strings.Join(lines, "\n")
}
