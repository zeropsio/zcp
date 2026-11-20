package ops

import (
	"slices"
	"strconv"
	"strings"
)

// A command reaches agent-browser as its argv, and agent-browser takes its
// global flags out of it wherever they stand, a value flag with the argument
// after it, before it reads the command's name and arguments (agent-browser
// 0.38, cli/src/flags.rs clean_args; cli/src/commands.rs for eval and wait).
// zcp reads a command the same way to know which of its arguments the page
// runs as JavaScript (browserScripts).

// browserGlobalBoolFlags are agent-browser's global flags that take no
// value, save an optional "true" or "false" after them.
var browserGlobalBoolFlags = []string{
	"--json", "--headed", "--webgpu", "--no-webmcp", "--debug", "--ignore-https-errors",
	"--allow-file-access", "--hide-scrollbars", "--auto-connect", "--pin-tab", "--no-pin-tab",
	"--no-ca-cert", "--annotate", "--content-boundaries", "--confirm-interactive",
	"--no-auto-dialog", "-v", "--verbose", "-q", "--quiet", "--offline", "--quick", "--fix",
}

// browserBoolFlagValues are the values a bool flag takes after it.
var browserBoolFlagValues = []string{"true", "false"}

// browserGlobalValueFlags are agent-browser's global flags that take the
// argument after them as their value, whatever it is.
var browserGlobalValueFlags = []string{
	"--session", "--restore-save", "--restore-check-url", "--restore-check-text",
	browserFlagRestoreCheckFn, "--namespace", "--headers", "--executable-path", "--cdp",
	"--extension", "--init-script", "--enable", "--profile", "--state", "--proxy",
	"--proxy-bypass", "--args", "--user-agent", "-p", "--provider", "--device", "--session-name",
	"--color-scheme", "--download-path", "--max-output", "--allowed-domains", "--action-policy",
	"--confirm-actions", "--config", "--engine", "--input-mode", "--screenshot-dir",
	"--screenshot-quality", "--screenshot-format", "--idle-timeout", "--ca-cert", "--model",
}

// browserCmdWait waits for a page state; after --fn or -f, a JavaScript
// expression.
const browserCmdWait = "wait"

// browserFlagRestoreCheckFn is the global flag whose value is JavaScript the
// page runs when a saved session is restored.
const browserFlagRestoreCheckFn = "--restore-check-fn"

// browserFlagRestore is the global flag whose value is optional: before the
// command's name it takes the argument after it unless that is a flag or a
// command's name.
const browserFlagRestore = "--restore"

// browserTopLevelCommands are the names agent-browser takes for a command's
// (cli/src/commands.rs is_top_level_command).
var browserTopLevelCommands = strings.Fields(`open goto navigate back forward reload read click
	dblclick fill type hover focus check uncheck select drag upload download press key keydown
	keyup keyboard scroll scrollintoview scrollinto wait screenshot pdf snapshot eval close quit
	exit inspect auth confirm deny connect stream get is find mouse set network storage cookies
	tab window frame dialog trace profiler record console errors highlight clipboard state tap
	swipe device diff batch react vitals web-vitals a11y pushstate removeinitscript session mcp
	doctor install upgrade profiles skills dashboard plugin plugins chat webmcp`)

// browserCommandArgs reads cmd as agent-browser does: own are the places of
// the command's own arguments, its name first, and restoreFns those of the
// values of browserFlagRestoreCheckFn; every other argument is a global
// flag or a flag's value.
func browserCommandArgs(cmd []string) (own, restoreFns []int) {
	named := false
	for i := 0; i < len(cmd); i++ {
		arg := cmd[i]
		switch {
		case strings.HasPrefix(arg, browserFlagRestore+"="):
		case slices.Contains(browserGlobalValueFlags, arg):
			if arg == browserFlagRestoreCheckFn && i+1 < len(cmd) {
				restoreFns = append(restoreFns, i+1)
			}
			i++
		case arg == browserFlagRestore:
			if next := i + 1; !named && next < len(cmd) && !strings.HasPrefix(cmd[next], "-") && !slices.Contains(browserTopLevelCommands, cmd[next]) {
				i++
			}
		case slices.Contains(browserGlobalBoolFlags, arg):
			if next := i + 1; next < len(cmd) && slices.Contains(browserBoolFlagValues, cmd[next]) {
				i++
			}
		default:
			if !strings.HasPrefix(arg, "-") && slices.Contains(browserTopLevelCommands, arg) {
				named = true
			}
			own = append(own, i)
		}
	}
	return own, restoreFns
}

// browserWaitCapMs is the longest a caller's wait polls, in milliseconds. In
// November 6 of 161 timed-out agent waits asked more than it and lost at
// least 570 s; no clean walk with a longer wait ran past 98 s, while waits
// that succeeded after 60 s were measured.
const browserWaitCapMs = 120000

// browserWaitTimeout finds the value of a wait command's own --timeout: its
// place in cmd and the number it holds; ok is false for any other command,
// a wait without one, or a value that is no whole number.
func browserWaitTimeout(cmd []string) (at int, ms int64, ok bool) {
	own, _ := browserCommandArgs(cmd)
	if len(own) == 0 || cmd[own[0]] != browserCmdWait {
		return 0, 0, false
	}
	for j := 1; j+1 < len(own); j++ {
		if cmd[own[j]] != "--timeout" {
			continue
		}
		ms, err := strconv.ParseInt(cmd[own[j+1]], 10, 64)
		return own[j+1], ms, err == nil
	}
	return 0, 0, false
}

// capBrowserWait lowers a wait command's own --timeout to browserWaitCapMs
// when it asks more; any other command comes back as it came. cmd itself is
// never changed.
func capBrowserWait(cmd []string) []string {
	at, ms, ok := browserWaitTimeout(cmd)
	if !ok || ms <= browserWaitCapMs {
		return cmd
	}
	capped := slices.Clone(cmd)
	capped[at] = strconv.Itoa(browserWaitCapMs)
	return capped
}

// browserScripts are the JavaScript agent-browser hands the page from cmd,
// each by the places of the arguments it is made of, joined with a space: an
// eval's source, every argument of its own after the name; the argument
// after a wait's --fn or -f, where no --url or --load makes it another
// wait; and the value of browserFlagRestoreCheckFn. opaque are the places of
// JavaScript zcp cannot read: an eval's base64 source.
func browserScripts(cmd []string) (scripts [][]int, opaque []int) {
	own, restoreFns := browserCommandArgs(cmd)
	for _, at := range restoreFns {
		scripts = append(scripts, []int{at})
	}
	if len(own) == 0 {
		return scripts, nil
	}
	rest := own[1:]
	switch cmd[own[0]] {
	case browserCmdEval:
		switch {
		case len(rest) > 0 && (cmd[rest[0]] == "-b" || cmd[rest[0]] == "--base64"):
			opaque = rest[1:]
		case len(rest) > 0 && cmd[rest[0]] == "--stdin":
			// The source is read from stdin, which the walk does not give.
		case len(rest) > 0:
			scripts = append(scripts, rest)
		}
	case browserCmdWait:
		rest = slices.Clone(rest)
		if at := slices.IndexFunc(rest, func(i int) bool { return cmd[i] == "--timeout" }); at >= 0 {
			rest = slices.Delete(rest, at, min(at+2, len(rest)))
		}
		is := func(names ...string) func(int) bool {
			return func(i int) bool { return slices.Contains(names, cmd[i]) }
		}
		if slices.ContainsFunc(rest, is("--url", "-u")) || slices.ContainsFunc(rest, is("--load", "-l")) {
			break
		}
		if at := slices.IndexFunc(rest, is("--fn", "-f")); at >= 0 && at+1 < len(rest) {
			scripts = append(scripts, []int{rest[at+1]})
		}
	}
	return scripts, opaque
}
