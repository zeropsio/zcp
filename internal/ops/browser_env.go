package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"

	"github.com/zeropsio/zcp/internal/platform"
)

// A zerops_browser command reaches a page with a value the agent never
// sees (docs/spec-mate.md §5.8a): in any argument, `{{env:KEY}}` is replaced
// with the value the container's live env store holds under KEY
// (LiveEnvValues: the reader and the refusals of `zcp with`), and the result
// goes back with the placeholder where the value was (browser_redact.go).

// browserEnvRef is a `{{env:KEY}}` placeholder; what stands for KEY is
// checked by the store's reader.
var browserEnvRef = regexp.MustCompile(`\{\{env:(.*?)\}\}`)

// browserFlagFn is the wait flag whose next argument is JavaScript.
const browserFlagFn = "--fn"

// browserEnvUse is what one call substituted: the batch as it was written
// and as it ran, step for step, and the redactor of the values behind its
// placeholders. The zero value is a call that held no placeholder.
type browserEnvUse struct {
	written, run [][]string
	redactor     *browserRedactor
}

func browserEnvPlaceholder(key string) string { return "{{env:" + key + "}}" }

// substituteBrowserEnv answers the batch to run for the batch as written:
// every `{{env:KEY}}` in a command's argument replaced, once, with the
// store's value, read once for the call — escaped where the argument is
// JavaScript (isBrowserScript), where the placeholder must stand inside a
// quoted string and is then exactly the value. The step that opens the
// call's url is zcp's own and is not searched. The call is refused, and
// nothing has run, for a placeholder in JavaScript outside a quoted string,
// for a refusal of the store's reader, and for a value longer than
// browserValueMaxLen. A batch without a placeholder never reads the store.
func substituteBrowserEnv(written [][]string, storePath string, inMate bool) ([][]string, browserEnvUse, error) {
	var keys []string
	for _, cmd := range written {
		if isCommand(cmd, browserCmdOpen) {
			continue
		}
		for at, arg := range cmd {
			refs := browserEnvRef.FindAllStringSubmatch(arg, -1)
			if len(refs) > 0 && isBrowserScript(cmd, at) && !jsPlaceholdersQuoted(arg) {
				return nil, browserEnvUse{}, platform.NewPlatformError(platform.ErrInvalidParameter,
					"A {{env:…}} placeholder in JavaScript does not stand inside a quoted string, or zcp cannot tell that it does; the browser was not opened",
					"In an eval's source and a wait's --fn write it between quotes — '{{env:KEY}}', \"{{env:KEY}}\" or `{{env:KEY}}` — not after a backslash and not inside ${…}: "+
						"the value is escaped to be exactly that string. Where a / outside a string stands before it (a division, a regular expression), put it first: const v = '{{env:KEY}}'; …")
			}
			for _, ref := range refs {
				if !slices.Contains(keys, ref[1]) {
					keys = append(keys, ref[1])
				}
			}
		}
	}
	if len(keys) == 0 {
		return written, browserEnvUse{}, nil
	}
	values, refused := LiveEnvValues(storePath, keys)
	if refused != nil {
		return nil, browserEnvUse{}, browserEnvRefusal(refused, inMate)
	}
	for _, key := range keys {
		if len(values[key]) > browserValueMaxLen {
			return nil, browserEnvUse{}, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("%s: the value is longer than %d bytes, more than zerops_browser takes back out of a result; the browser was not opened", browserEnvPlaceholder(key), browserValueMaxLen),
				fmt.Sprintf("Hand a value this long to a command instead: zcp with %s -- <command>.", key))
		}
	}
	run := make([][]string, len(written))
	for i, cmd := range written {
		run[i] = cmd
		if isCommand(cmd, browserCmdOpen) {
			continue
		}
		run[i] = make([]string, len(cmd))
		for j, arg := range cmd {
			script := isBrowserScript(cmd, j)
			run[i][j] = browserEnvRef.ReplaceAllStringFunc(arg, func(ref string) string {
				value := values[browserEnvRef.FindStringSubmatch(ref)[1]]
				if script {
					return jsStringEscape(value)
				}
				return value
			})
		}
	}
	return run, browserEnvUse{written: written, run: run, redactor: newBrowserRedactor(values)}, nil
}

// isBrowserScript reports whether argument at of cmd is JavaScript the page
// runs: an eval's source, or what follows a wait's --fn.
func isBrowserScript(cmd []string, at int) bool {
	if isCommand(cmd, browserCmdEval) {
		return at > 0
	}
	return at > 0 && cmd[at-1] == browserFlagFn
}

// browserEnvRefusal words a refusal of the store's reader for the agent:
// which key, and what to do — by where zcp runs, since only a Mate has a
// vault of its own and the ask that fills it. Never a value: what stood
// where a name belongs is not echoed, and of a store that could not be read
// only the system's own error on the file is quoted.
func browserEnvRefusal(refused *LiveEnvRefusal, inMate bool) error {
	const notOpened = "; the browser was not opened"
	switch refused.Kind {
	case LiveEnvNotAName:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"A {{env:…}} placeholder in commands does not name a key"+notOpened,
			"Write {{env:KEY}} with the key's name alone (letters, digits and `_`), as in {{env:TEST_PASSWORD}}; never a value.")
	case LiveEnvHidden:
		other := "Use a value under another name, one this service's variables hold."
		if inMate {
			other = "Use a value of your own under another name; zerops_env action=\"request\" mate=true asks the person for one."
		}
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%s names a key Mate and zcp keep for themselves, which zerops_browser hands to no page%s", browserEnvPlaceholder(refused.Key), notOpened),
			other)
	case LiveEnvNotHeld:
		return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("%s: this container's env does not hold %s%s", browserEnvPlaceholder(refused.Key), refused.Key, notOpened),
			LiveEnvNotHeldWords(refused.Key, inMate, "call again"))
	case LiveEnvStoreUnread:
		var onFile *fs.PathError
		if errors.As(refused.Err, &onFile) {
			return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				fmt.Sprintf("{{env:…}} reads this container's env store, which could not be read (%v)%s", onFile, notOpened),
				"The platform keeps the store in a Zerops container; without it, run the commands without {{env:…}}.")
		}
	}
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		"{{env:…}} reads this container's env store, which does not read as the platform writes it"+notOpened,
		"Run the commands without {{env:…}}, or try again once the store is whole.")
}

// browserWithheld is what a result says when the values could not be taken
// out of it within the search's bound (browserRedactBudget): the text from
// the browser side is not returned, since it was not searched.
const browserWithheld = "Text from the browser side is withheld: taking the {{env:…}} values out of this call's answer did not finish within its bound, " +
	"so no step result or error, no errors, console or network output and nothing of agent-browser's own is returned — only each step's command as written and whether it succeeded. " +
	"The page ran what it was given. To read the text, call again with fewer or shorter {{env:…}} values, or with less output."

// takeBack takes the substituted values out of what a walk answered, before
// zcp reads or composes anything from it. A step names its command as it was
// written — put back by its place in the batch, byte for byte, never
// searched — and so does the error of a step that got no answer. What came
// from the browser side is searched for the values (browserRedactor): each
// step's result and error, and agent-browser's stderr — which, cut at its
// cap (truncated), loses its end from where a form of a value may have
// begun. When the search does not finish within its bound, withheld is true
// and the answer carries no text of the browser side at all.
func (u browserEnvUse) takeBack(stdout, stderr string, truncated bool, runErr error) (out, errOut string, withheld bool, err error) {
	if u.redactor == nil {
		return stdout, stderr, false, runErr
	}
	var unanswered *browserStepUnansweredError
	if errors.As(runErr, &unanswered) && unanswered.step < len(u.written) {
		unanswered.command = u.written[unanswered.step]
	}
	if truncated {
		stderr = u.redactor.cutText(stderr)
	} else {
		stderr = u.redactor.text(stderr)
	}
	var steps []BrowserStepResult
	parsed := json.Unmarshal([]byte(stdout), &steps) == nil
	if !parsed {
		// Not the walk's own JSON: nothing in it is known to be a command.
		stdout = u.redactor.text(stdout)
	}
	for i := range steps {
		step := &steps[i]
		if i < len(u.run) && slices.Equal(step.Command, u.run[i]) {
			step.Command = u.written[i]
		} else {
			// No step of this batch: its words are the browser side's.
			for j, arg := range step.Command {
				step.Command[j] = u.redactor.text(arg)
			}
		}
		step.Result = u.redactor.json(step.Result)
		if step.Error != nil {
			text := u.redactor.text(*step.Error)
			step.Error = &text
		}
	}
	if u.redactor.spent {
		return u.withheldSteps(steps), "", true, runErr
	}
	if !parsed {
		return stdout, stderr, false, runErr
	}
	redacted, marshalErr := json.Marshal(steps)
	if marshalErr != nil {
		return u.withheldSteps(steps), "", true, runErr
	}
	return string(redacted), stderr, false, runErr
}

// withheldSteps are a walk's steps with nothing of the browser side left in
// them: each step of this batch by its command as written and whether it
// succeeded, a failed one saying its error is withheld.
func (u browserEnvUse) withheldSteps(steps []BrowserStepResult) string {
	const note = "withheld: see message"
	kept := make([]BrowserStepResult, 0, len(steps))
	for i, step := range steps {
		if i >= len(u.written) {
			break
		}
		bare := BrowserStepResult{Command: u.written[i], Success: step.Success}
		if step.Error != nil {
			text := note
			bare.Error = &text
		}
		kept = append(kept, bare)
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return "[]"
	}
	return string(out)
}
