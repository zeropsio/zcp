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
	"unicode/utf16"
)

// A zerops_browser result goes back without the values its commands were
// given (docs/spec-mate.md §5.8a): browserRedactor finds each one in the
// text agent-browser and the page gave back, however that text spells it,
// and puts its placeholder there.
//
// A value is its characters in order, and each character may be spelled
// (runeSpellings): as itself; as JSON text escapes it, applied up to
// browserRedactJSONDepth times; as the JavaScript escape an eval was given
// (jsRuneEscape), JSON-escaped on top. Each byte of a spelling may in turn
// stand as itself, as its %HH escape (a space also as `+`), or as its HTML
// entity (byteEnds). So every percent-encoding of the value is found,
// whichever characters the encoder chose, without listing encoders. What is
// not found is any other transformation — base64, a hash, a listed encoding
// applied once more than listed — and a value under browserRedactMinLen.

// browserRedactMinLen is the shortest substituted value text from the
// browser side is searched for. A shorter one is a common fragment of
// ordinary output — a short word, a number — so replacing it would shred the
// result, and it protects nothing a guess does not find. The command a value
// was written in is never searched: it goes back as written whatever the
// value's length.
const browserRedactMinLen = 6

// browserRedactJSONDepth is how many times over a value may be JSON-escaped
// and still be found: JSON inside a JSON string inside a JSON string.
const browserRedactJSONDepth = 3

// htmlEntities are the ways HTML writes the five characters it escapes, as a
// snapshot or a `get html` returns a value typed into a page.
var htmlEntities = map[byte][]string{
	'&':  {"&amp;"},
	'<':  {"&lt;"},
	'>':  {"&gt;"},
	'"':  {"&quot;", "&#34;"},
	'\'': {"&#39;", "&#039;", "&#x27;", "&apos;"},
}

// valuePattern finds one substituted value: units holds, per character of
// the value, the spellings it may have; first is every byte a match can
// start with.
type valuePattern struct {
	placeholder string
	units       [][]string
	first       [256]bool
}

// browserRedactor replaces the values of one call by their placeholders.
type browserRedactor struct {
	patterns []valuePattern
}

// newBrowserRedactor is the redactor of the values a call substituted, by
// key. Two keys holding one value are both that value: the first key, in
// order, names it.
func newBrowserRedactor(values map[string]string) *browserRedactor {
	redactor := &browserRedactor{}
	var seen []string
	for _, key := range slices.Sorted(maps.Keys(values)) {
		value := values[key]
		if len(value) < browserRedactMinLen || slices.Contains(seen, value) {
			continue
		}
		seen = append(seen, value)
		pattern := valuePattern{placeholder: browserEnvPlaceholder(key)}
		for _, r := range value {
			pattern.units = append(pattern.units, runeSpellings(r))
		}
		for _, spelling := range pattern.units[0] {
			b := spelling[0]
			pattern.first[b], pattern.first['%'] = true, true
			if b == ' ' {
				pattern.first['+'] = true
			}
			if len(htmlEntities[b]) > 0 {
				pattern.first['&'] = true
			}
		}
		redactor.patterns = append(redactor.patterns, pattern)
	}
	return redactor
}

// runeSpellings are the ways one character of a value may be spelled in
// text from the browser side, before any byte of it is percent-encoded or
// written as an HTML entity.
func runeSpellings(r rune) []string {
	raw := string(r)
	all := []string{raw}
	level := jsonSpellings(raw)
	if escaped := jsRuneEscape(r); escaped != raw {
		level = append(level, escaped)
	}
	for depth := 1; depth <= browserRedactJSONDepth; depth++ {
		deeper := make([]string, 0, len(level))
		for _, spelling := range level {
			if !slices.Contains(all, spelling) {
				all = append(all, spelling)
			}
			deeper = append(deeper, jsonSpellings(spelling)...)
		}
		level = deeper
	}
	return all
}

// jsonSpellings are the ways a JSON encoder may write text inside a string:
// each character one of jsonRuneSpellings.
func jsonSpellings(text string) []string {
	spellings := []string{""}
	for _, r := range text {
		options := jsonRuneSpellings(r)
		next := make([]string, 0, len(spellings)*len(options))
		for _, head := range spellings {
			for _, option := range options {
				next = append(next, head+option)
			}
		}
		spellings = next
	}
	return spellings
}

// jsonRuneSpellings are the ways JSON encoders write one character inside a
// string: the escape JSON demands for a quote, a backslash and a control
// character; `\/`, which some write; and `\uXXXX`, in either hex case, for
// the characters some encoders keep out of HTML (`<`, `>`, `&`) or out of
// ASCII output (everything past it, as a surrogate pair where it takes one).
func jsonRuneSpellings(r rune) []string {
	unicodeEscapes := func(r rune) []string {
		units := []rune{r}
		if r > 0xFFFF {
			high, low := utf16.EncodeRune(r)
			units = []rune{high, low}
		}
		var lower, upper string
		for _, unit := range units {
			lower += fmt.Sprintf(`\u%04x`, unit)
			upper += fmt.Sprintf(`\u%04X`, unit)
		}
		if lower == upper {
			return []string{lower}
		}
		return []string{lower, upper}
	}
	switch {
	case r == '"':
		return []string{`\"`}
	case r == '\\':
		return []string{`\\`}
	case r == '/':
		return []string{`/`, `\/`}
	case r < 0x20:
		short := map[rune]string{'\b': `\b`, '\f': `\f`, '\n': `\n`, '\r': `\r`, '\t': `\t`}
		if escape, ok := short[r]; ok {
			return append([]string{escape}, unicodeEscapes(r)...)
		}
		return unicodeEscapes(r)
	case r == '<', r == '>', r == '&', r >= 0x7f:
		return append([]string{string(r)}, unicodeEscapes(r)...)
	}
	return []string{string(r)}
}

// percentByte is the byte a %HH escape at text[pos] stands for, -1 when
// there is none there.
func percentByte(text string, pos int) int {
	if pos+2 >= len(text) || text[pos] != '%' {
		return -1
	}
	digit := func(c byte) int {
		switch {
		case c >= '0' && c <= '9':
			return int(c - '0')
		case c >= 'a' && c <= 'f':
			return int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			return int(c-'A') + 10
		}
		return -1
	}
	high, low := digit(text[pos+1]), digit(text[pos+2])
	if high < 0 || low < 0 {
		return -1
	}
	return high<<4 | low
}

// byteEnds appends where text goes on after one byte of a spelling read at
// pos: the byte itself, its %HH escape in either hex case, `+` for a space,
// or its HTML entity. A `%` or an `&` in the text may be either the byte or
// the start of its escape, so there can be more than one.
func byteEnds(text string, pos int, b byte, ends []int) []int {
	if pos >= len(text) {
		return ends
	}
	c := text[pos]
	if c == b {
		ends = append(ends, pos+1)
	}
	if percentByte(text, pos) == int(b) {
		ends = append(ends, pos+3)
	}
	if b == ' ' && c == '+' {
		ends = append(ends, pos+1)
	}
	if c == '&' {
		for _, entity := range htmlEntities[b] {
			if strings.HasPrefix(text[pos:], entity) {
				ends = append(ends, pos+len(entity))
			}
		}
	}
	return ends
}

// matchAt reports where the value ends when text spells it from start. The
// places text may have reached are followed together, character by
// character, so a text that can be read two ways costs no more than one.
func (p *valuePattern) matchAt(text string, start int) (end int, ok bool) {
	reached := []int{start}
	for _, spellings := range p.units {
		next := make([]int, 0, len(reached))
		for _, from := range reached {
			for _, spelling := range spellings {
				at := []int{from}
				for i := 0; i < len(spelling) && len(at) > 0; i++ {
					var after []int
					for _, pos := range at {
						after = byteEnds(text, pos, spelling[i], after)
					}
					at = after
				}
				next = append(next, at...)
			}
		}
		if len(next) == 0 {
			return 0, false
		}
		slices.Sort(next)
		reached = slices.Compact(next)
	}
	return reached[len(reached)-1], true
}

// text is a text from the browser side with each value it spells replaced
// by its placeholder. Values that overlap are replaced together, by the
// placeholders of each, so no part of one is left beside the other's.
func (r *browserRedactor) text(text string) string {
	if r == nil || len(r.patterns) == 0 {
		return text
	}
	type span struct {
		start, end   int
		placeholders []string
	}
	var spans []span
	for i := 0; i < len(text); i++ {
		for p := range r.patterns {
			pattern := &r.patterns[p]
			if !pattern.first[text[i]] {
				continue
			}
			end, ok := pattern.matchAt(text, i)
			if !ok {
				continue
			}
			if n := len(spans); n > 0 && i < spans[n-1].end {
				last := &spans[n-1]
				last.end = max(last.end, end)
				if !slices.Contains(last.placeholders, pattern.placeholder) {
					last.placeholders = append(last.placeholders, pattern.placeholder)
				}
				continue
			}
			spans = append(spans, span{start: i, end: end, placeholders: []string{pattern.placeholder}})
		}
	}
	if len(spans) == 0 {
		return text
	}
	var redacted strings.Builder
	from := 0
	for _, found := range spans {
		redacted.WriteString(text[from:found.start])
		redacted.WriteString(strings.Join(found.placeholders, ""))
		from = found.end
	}
	redacted.WriteString(text[from:])
	return redacted.String()
}

// json is a step's result with the values taken out of what the page put in
// it: every string and every number, and — for keysFrom levels down and
// deeper — every object key. The keys above that level are agent-browser's
// own and are left, so the result reads as before. Strings are searched
// decoded, so a value is found at the level of JSON it stands in. A result
// that holds no value goes back as it came; one that is no JSON goes back as
// its redacted text.
func (r *browserRedactor) json(raw json.RawMessage, keysFrom int) json.RawMessage {
	if r == nil || len(r.patterns) == 0 || len(raw) == 0 {
		return raw
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out bytes.Buffer
	changed := false
	err := r.rewriteJSON(decoder, &out, 0, keysFrom, &changed)
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
func (r *browserRedactor) rewriteJSON(decoder *json.Decoder, out *bytes.Buffer, depth, keysFrom int, changed *bool) error {
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
				if depth >= keysFrom {
					key = leaf(key)
				}
				out.Write(browserJSONString(key))
				out.WriteByte(':')
			}
			if err := r.rewriteJSON(decoder, out, depth+1, keysFrom, changed); err != nil {
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
