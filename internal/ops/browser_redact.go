package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// A zerops_browser result goes back without the values its commands were
// given (docs/spec-mate.md §5.8a): browserRedactor finds each one in the
// text agent-browser and the page gave back, however that text spells it
// (browser_match.go), and puts its placeholder there.
//
// A value is searched for as it is stored and as a browser hands it back
// (browserReturnForms). Each of those is its characters in order, and each
// character may be spelled as itself, as a JSON or JavaScript string writes
// it — a short escape, or \uXXXX for any character — and so again inside up
// to browserRedactJSONDepth levels of JSON; each byte of a spelling may
// stand as itself, as its %HH escape (a space also as `+`), or as the HTML
// entity of its character. What is not found is any other transformation —
// base64, a hash, a listed encoding applied once more than listed — and a
// value under browserRedactMinLen.
//
// The search is bounded (browserRedactBudget). A redactor that ran out
// answers no text at all from then on: what was not searched is withheld,
// never returned.

// browserRedactMinLen is the shortest form of a substituted value text from
// the browser side is searched for. A shorter one is a common fragment of
// ordinary output — a short word, a number — so replacing it would shred the
// result, and it protects nothing a guess does not find. The command a value
// was written in is never searched: it goes back as written whatever the
// value's length.
const browserRedactMinLen = 6

// browserRedactJSONDepth is how many times over a value may be JSON-escaped
// and still be found: JSON inside a JSON string inside a JSON string.
const browserRedactJSONDepth = 3

// browserValueMaxLen is the longest value a command may be given, in bytes:
// long enough for a signed token or a private key, short enough that the
// search for it stays within its budget on ordinary output.
const browserValueMaxLen = 4096

// browserRedactBudget bounds the search of one call's answer, in steps: one
// per character read, one per reading of a value under way that the
// character moves and one per reading it makes — a second or two of
// work. An ordinary megabyte takes a few million; only a text that keeps
// hundreds of readings alive at once — a value of one repeated character
// against a text of that character — comes near it.
const browserRedactBudget int64 = 100_000_000

// browserRedactor replaces the values of one call by their placeholders.
type browserRedactor struct {
	matcher      *matcher
	placeholders []string
	budget       int64
	// spent is set when the budget ran out: every text answered since is
	// empty, and the call's answer is withheld (browserEnvUse.takeBack).
	spent bool
}

// newBrowserRedactor is the redactor of the values a call substituted, by
// key. Two keys holding one value are both that value: the first key, in
// order, names it.
func newBrowserRedactor(values map[string]string) *browserRedactor {
	redactor := &browserRedactor{
		matcher: &matcher{tries: map[rune]*runeTrie{}, entityStarts: map[rune][]int32{}},
		budget:  browserRedactBudget,
	}
	var seen []string
	for _, key := range slices.Sorted(maps.Keys(values)) {
		placeholder := int32(len(redactor.placeholders))
		redactor.placeholders = append(redactor.placeholders, browserEnvPlaceholder(key))
		for _, form := range browserReturnForms(values[key]) {
			if len(form) < browserRedactMinLen || slices.Contains(seen, form) {
				continue
			}
			seen = append(seen, form)
			redactor.matcher.add(form, placeholder)
		}
	}
	return redactor
}

// browserReturnForms are a value as it is stored and as a browser hands it
// back once it was typed into a page: trimmed of the whitespace around it
// (an e-mail field), with its line breaks removed (a one-line field), with
// each line break as LF (a textarea) and as CRLF (a submitted form), and
// each of its lines on its own (a snapshot gives a text per line).
func browserReturnForms(value string) []string {
	const space = " \t\n\f\r"
	lf := strings.NewReplacer("\r\n", "\n", "\r", "\n")
	forms := []string{value}
	add := func(form string) {
		if !slices.Contains(forms, form) {
			forms = append(forms, form)
		}
	}
	for _, kept := range []string{value, strings.Trim(value, space)} {
		add(kept)
		unix := lf.Replace(kept)
		add(unix)
		add(strings.ReplaceAll(unix, "\n", "\r\n"))
		add(strings.ReplaceAll(unix, "\n", ""))
	}
	for line := range strings.SplitSeq(lf.Replace(value), "\n") {
		add(line)
		add(strings.Trim(line, space))
	}
	return forms
}

// text is a text from the browser side with each value it spells replaced
// by its placeholder. Values that overlap are replaced together, by the
// placeholders of each, so no part of one is left beside the other's. Once
// the redactor's budget is spent it answers "" and says so in spent.
func (r *browserRedactor) text(text string) string {
	return r.search(text, false)
}

// cutText is text for a capture that was cut at its cap: redacted, and
// without its end from where a form of a value may have begun — the cut may
// have gone through one, and its beginning must not stay.
func (r *browserRedactor) cutText(text string) string {
	return r.search(text, true)
}

// search redacts text; cut also drops its end from where the earliest
// reading of a value still under way at the text's end began, or from an
// escape the cut left unfinished (unfinishedEscape), whichever is earlier.
func (r *browserRedactor) search(text string, cut bool) string {
	if r == nil || len(r.matcher.patterns) == 0 {
		return text
	}
	if r.spent {
		return ""
	}
	spans, pending, ok := r.matcher.find(text, &r.budget)
	if !ok {
		r.spent = true
		return ""
	}
	limit := int32(len(text))
	if cut {
		limit = int32(min(pending, unfinishedEscape(text)))
	}
	var out strings.Builder
	from := int32(0)
	for _, span := range spans {
		if span.start >= limit {
			break
		}
		out.WriteString(text[from:span.start])
		for _, placeholder := range span.placeholders {
			out.WriteString(r.placeholders[placeholder])
		}
		from = span.end
	}
	if from < limit {
		out.WriteString(text[from:limit])
	}
	return out.String()
}

// json is a step's result with the values taken out of what the page put in
// it: every string and every number, and every object key below the first
// level. The first level's keys are the names agent-browser wraps a result
// in and are left, so the result reads as before; what stands under them —
// an eval's value, a storage's keys — is the page's. Strings are searched
// decoded, so a value is found at the level of JSON it stands in. A result
// that holds no value goes back as it came; one that is no JSON goes back as
// its redacted text.
func (r *browserRedactor) json(raw json.RawMessage) json.RawMessage {
	if r == nil || len(r.matcher.patterns) == 0 || len(raw) == 0 {
		return raw
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out bytes.Buffer
	changed := false
	err := r.rewriteJSON(decoder, &out, 0, &changed)
	if err == nil {
		if _, trailing := decoder.Token(); !errors.Is(trailing, io.EOF) {
			err = errors.New("more than one JSON value")
		}
	}
	if err != nil {
		return browserJSONString(r.text(string(raw)))
	}
	if !changed {
		return raw
	}
	return out.Bytes()
}

// rewriteJSON copies the next value of decoder to out, redacted.
func (r *browserRedactor) rewriteJSON(decoder *json.Decoder, out *bytes.Buffer, depth int, changed *bool) error {
	leaf := func(text string) string {
		redacted := r.text(text)
		if redacted != text {
			*changed = true
		}
		return redacted
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("read a JSON value: %w", err)
	}
	switch value := token.(type) {
	case json.Delim:
		out.WriteString(value.String())
		for first := true; decoder.More(); first = false {
			if !first {
				out.WriteByte(',')
			}
			if value == '{' {
				keyToken, err := decoder.Token()
				if err != nil {
					return fmt.Errorf("read a JSON key: %w", err)
				}
				key, _ := keyToken.(string)
				if depth >= 1 {
					key = leaf(key)
				}
				out.Write(browserJSONString(key))
				out.WriteByte(':')
			}
			if err := r.rewriteJSON(decoder, out, depth+1, changed); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("read a JSON value's end: %w", err)
		}
		fmt.Fprint(out, closing)
	case string:
		out.Write(browserJSONString(leaf(value)))
	case json.Number:
		if redacted := leaf(value.String()); redacted != value.String() {
			out.Write(browserJSONString(redacted))
		} else {
			out.WriteString(value.String())
		}
	case bool:
		fmt.Fprint(out, value)
	case nil:
		out.WriteString("null")
	}
	return nil
}

// browserJSONString is text as a JSON string, written as agent-browser writes one:
// `<`, `>` and `&` as they are.
func browserJSONString(text string) json.RawMessage {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(text); err != nil {
		return json.RawMessage(`""`)
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
}

// unfinishedEscape is where an escape a cut may have left unfinished begins
// at the end of text — a `%` or a `%` and one hex digit, or a `&` followed
// by nothing but the beginning of an entity, without its `;` — and len(text)
// when there is none. Its rest could have spelled a value's first character,
// which no reading has begun with yet.
func unfinishedEscape(text string) int {
	n := len(text)
	switch {
	case n >= 1 && text[n-1] == '%':
		return n - 1
	case n >= 2 && text[n-2] == '%' && hexNibble(text[n-1]) >= 0:
		return n - 2
	}
	amp := strings.LastIndexByte(text, '&')
	if amp < 0 {
		return n
	}
	tail := text[amp+1:]
	var digits string
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	switch {
	case strings.HasPrefix(tail, "#x"), strings.HasPrefix(tail, "#X"):
		digits, isDigit = tail[2:], func(c byte) bool { return hexNibble(c) >= 0 }
	case strings.HasPrefix(tail, "#"):
		digits = tail[1:]
	default:
		for _, name := range entityNames {
			if strings.HasPrefix(name, tail) {
				return amp
			}
		}
		return n
	}
	for i := range len(digits) {
		if !isDigit(digits[i]) {
			return n
		}
	}
	return amp
}
