package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// A zerops_browser command reaches a page with a value the agent never
// sees (docs/spec-mate.md §5.8a): in any argument, `{{env:KEY}}` is replaced
// with the value the container's live env store holds under KEY
// (LiveEnvValues: the reader and the refusals of `zcp with`), and the result
// goes back with the placeholder where the value was.

// browserEnvRef is a `{{env:KEY}}` placeholder; what stands for KEY is
// checked by the store's reader.
var browserEnvRef = regexp.MustCompile(`\{\{env:(.*?)\}\}`)

// browserRedactMinLen is the shortest substituted value the result's text is
// searched for. A shorter one is a common fragment of ordinary output — a
// short word, a number — so replacing it would shred the result, and it
// protects nothing a guess does not find. The command a value was written
// in comes back as written whatever its length.
const browserRedactMinLen = 6

// browserEnvUse is what one call substituted: each command that held a
// placeholder, as written and as run, and the value behind each placeholder.
type browserEnvUse struct {
	written, run [][]string
	values       map[string]string
}

func browserEnvPlaceholder(key string) string { return "{{env:" + key + "}}" }

// substituteBrowserEnv replaces every `{{env:KEY}}` in commands with the
// store's value, read once for the call. A refusal of the reader refuses the
// call: nothing has run. Commands without a placeholder never read the store.
func substituteBrowserEnv(commands [][]string, storePath string) ([][]string, browserEnvUse, error) {
	var keys []string
	for _, cmd := range commands {
		for _, arg := range cmd {
			for _, ref := range browserEnvRef.FindAllStringSubmatch(arg, -1) {
				if !slices.Contains(keys, ref[1]) {
					keys = append(keys, ref[1])
				}
			}
		}
	}
	if len(keys) == 0 {
		return commands, browserEnvUse{}, nil
	}
	values, refused := LiveEnvValues(storePath, keys)
	if refused != nil {
		return nil, browserEnvUse{}, browserEnvRefusal(refused)
	}
	use := browserEnvUse{values: values}
	run := make([][]string, len(commands))
	for i, cmd := range commands {
		run[i] = make([]string, len(cmd))
		for j, arg := range cmd {
			run[i][j] = browserEnvRef.ReplaceAllStringFunc(arg, func(ref string) string {
				return values[browserEnvRef.FindStringSubmatch(ref)[1]]
			})
		}
		if !slices.Equal(run[i], cmd) {
			use.written, use.run = append(use.written, cmd), append(use.run, run[i])
		}
	}
	return run, use, nil
}

// browserEnvRefusal words a refusal of the store's reader for the agent:
// which key, and what to do. Never a value: what stood where a name belongs
// is not echoed, and of a store that could not be read only the system's own
// error on the file is quoted.
func browserEnvRefusal(refused *LiveEnvRefusal) error {
	const notOpened = "; the browser was not opened"
	switch refused.Kind {
	case LiveEnvNotAName:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"A {{env:…}} placeholder in commands does not name a key"+notOpened,
			"Write {{env:KEY}} with the key's name alone (letters, digits and `_`), as in {{env:TEST_PASSWORD}}; never a value.")
	case LiveEnvHidden:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%s names a key Mate and zcp keep for themselves, which zerops_browser hands to no page%s", browserEnvPlaceholder(refused.Key), notOpened),
			"Use a value of your own under another name; zerops_env action=\"request\" mate=true asks the person for one.")
	case LiveEnvNotHeld:
		return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("%s: this container's env does not hold %s%s", browserEnvPlaceholder(refused.Key), refused.Key, notOpened),
			fmt.Sprintf("It is not saved yet (an open ask waits for the person), was saved moments ago (it arrives a few seconds after the write: call again), "+
				"or was never asked for (zerops_env action=\"request\" mate=true key=%q).", refused.Key))
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

// redact takes the substituted values back out of what a call answers: each
// step that ran a substituted command names it as it was written, and every
// text — a step's command, result and error, the errors, console and network
// outputs, the message — carries the placeholder where a value of
// browserRedactMinLen bytes or more stood, in the forms zcp knows it by
// (valueForms). It runs before an eval's value is turned into text, so a
// value is found at one level of JSON escaping. A screenshot is pixels and
// is not touched.
func (u browserEnvUse) redact(result *BrowserBatchResult, refused []BrowserStepResult) {
	if len(u.values) == 0 {
		return
	}
	var forms []string
	placeholderOf := map[string]string{}
	for _, key := range slices.Sorted(maps.Keys(u.values)) {
		value := u.values[key]
		if len(value) < browserRedactMinLen {
			continue
		}
		for _, form := range valueForms(value) {
			if _, known := placeholderOf[form]; !known {
				placeholderOf[form] = browserEnvPlaceholder(key)
				forms = append(forms, form)
			}
		}
	}
	// The longest form first, so a value that holds another is taken whole;
	// equal lengths in a fixed order, so a result is the same every time.
	slices.SortFunc(forms, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	pairs := make([]string, 0, 2*len(forms))
	for _, form := range forms {
		pairs = append(pairs, form, placeholderOf[form])
	}
	replacer := strings.NewReplacer(pairs...)
	raw := func(text json.RawMessage) json.RawMessage {
		replaced := replacer.Replace(string(text))
		if replaced == string(text) {
			return text
		}
		if json.Valid([]byte(replaced)) {
			return json.RawMessage(replaced)
		}
		// A value that stood outside a JSON string (a number): the output
		// goes back as its redacted text.
		quoted, err := json.Marshal(replaced)
		if err != nil {
			return nil
		}
		return quoted
	}
	steps := func(steps []BrowserStepResult) {
		for i := range steps {
			step := &steps[i]
			if at := slices.IndexFunc(u.run, func(run []string) bool { return slices.Equal(run, step.Command) }); at >= 0 {
				step.Command = u.written[at]
			}
			step.Command = slices.Clone(step.Command)
			for j, arg := range step.Command {
				step.Command[j] = replacer.Replace(arg)
			}
			step.Result = raw(step.Result)
			if step.Error != nil {
				text := replacer.Replace(*step.Error)
				step.Error = &text
			}
		}
	}
	steps(result.Steps)
	steps(refused)
	result.ErrorsOutput = raw(result.ErrorsOutput)
	result.ConsoleOutput = raw(result.ConsoleOutput)
	for i := range result.NetworkOutput {
		request := &result.NetworkOutput[i]
		request.URL = replacer.Replace(request.URL)
		request.Method = replacer.Replace(request.Method)
		request.ResourceType = replacer.Replace(request.ResourceType)
		request.StatusText = replacer.Replace(request.StatusText)
	}
	result.Message = replacer.Replace(result.Message)
}

// valueForms are the ways a value stands in a result's text: as it is, as
// JSON text escapes it (with and without the escapes that keep it safe in
// HTML), and percent-encoded as a URL's query and path carry it.
func valueForms(value string) []string {
	forms := []string{value, url.QueryEscape(value), url.PathEscape(value)}
	if quoted, err := json.Marshal(value); err == nil {
		forms = append(forms, string(quoted[1:len(quoted)-1]))
	}
	var plain bytes.Buffer
	encoder := json.NewEncoder(&plain)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err == nil {
		quoted := bytes.TrimSpace(plain.Bytes())
		forms = append(forms, string(quoted[1:len(quoted)-1]))
	}
	return forms
}
