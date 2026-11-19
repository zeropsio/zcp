package ops

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A `{{env:KEY}}` in JavaScript — an eval's source, what follows a wait's
// --fn — is replaced with the value escaped for a string literal
// (jsStringEscape), so it must stand inside one (jsStringContent): anywhere
// else the escaped text would be code, and the page's own syntax error would
// quote a piece of the value back.

// jsRuneEscape writes one character of a value so that, inside a JavaScript
// string literal quoted with ', " or `, it is exactly that character: the
// three quotes and the backslash escaped, `$` so that a template literal
// starts no `${`, and line ends and control characters, which a quoted
// literal cannot hold.
func jsRuneEscape(r rune) string {
	switch r {
	case '\\':
		return `\\`
	case '\'':
		return `\'`
	case '"':
		return `\"`
	case '`':
		return "\\`"
	case '$':
		return `\$`
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\t':
		return `\t`
	case ' ', ' ':
		return fmt.Sprintf(`\u%04x`, r)
	}
	if r < 0x20 || r == 0x7f {
		return fmt.Sprintf(`\u%04x`, r)
	}
	return string(r)
}

// jsStringEscape is a value as it is substituted inside a quoted JavaScript
// string (jsRuneEscape).
func jsStringEscape(value string) string {
	var escaped strings.Builder
	for _, r := range value {
		escaped.WriteString(jsRuneEscape(r))
	}
	return escaped.String()
}

// jsStringContent marks the bytes of a JavaScript source that are the plain
// content of a string literal quoted with ', " or `: not its quotes, not a
// backslash or the character it escapes, not a template's `${…}`. The source
// is read as a page reads a classic script, which a CDP evaluation is: a
// `#!` at its very start, a `<!--` in code and a `-->` that begins a line
// (after nothing but whitespace and comments) open a comment to the line's
// end, as `//` does, and U+2028 and U+2029 end a line as LF and CR do. It
// reads only what it can be sure of and marks nothing from where it cannot
// tell: a `/` in code that opens no comment may open a regular expression,
// whose body could hold any quote, and a ' or " string that runs into a line
// end is no string.
func jsStringContent(source string) []bool {
	const (
		code = iota
		single
		double
		template
	)
	content := make([]bool, len(source))
	mode := code
	// braces counts the open `{` of each level of code: the source's own,
	// then one per `${` a template is in.
	braces := []int{0}
	// lineStart says that nothing but whitespace and comments stands before
	// i on its line, in code: a `-->` there opens a comment.
	lineStart := true
	i := 0
	if strings.HasPrefix(source, "#!") {
		i = jsLineEnd(source, 0)
	}
	for i < len(source) {
		c := source[i]
		switch mode {
		case code:
			if n := jsLineTerminatorLen(source, i); n > 0 {
				lineStart = true
				i += n
				continue
			}
			if n := jsWhiteSpaceLen(source, i); n > 0 {
				i += n
				continue
			}
			switch {
			case strings.HasPrefix(source[i:], "<!--"), strings.HasPrefix(source[i:], "//"),
				lineStart && strings.HasPrefix(source[i:], "-->"):
				i = jsLineEnd(source, i)
				continue
			case strings.HasPrefix(source[i:], "/*"):
				end := strings.Index(source[i+2:], "*/")
				if end < 0 {
					return content
				}
				for j := i + 2; j < i+2+end; j++ {
					if jsLineTerminatorLen(source, j) > 0 {
						lineStart = true
					}
				}
				i += end + 4
				continue
			}
			lineStart = false
			switch c {
			case '\'':
				mode = single
			case '"':
				mode = double
			case '`':
				mode = template
			case '{':
				braces[len(braces)-1]++
			case '}':
				switch top := len(braces) - 1; {
				case braces[top] > 0:
					braces[top]--
				case top > 0:
					braces, mode = braces[:top], template
				}
			case '/':
				// No comment, so a division or a regular expression.
				return content
			}
		case single, double:
			switch {
			case c == '\\':
				i++
			case (mode == single && c == '\'') || (mode == double && c == '"'):
				mode = code
			case c == '\n' || c == '\r':
				return content
			default:
				content[i] = true
			}
		case template:
			switch {
			case c == '\\':
				i++
			case c == '`':
				mode = code
			case strings.HasPrefix(source[i:], "${"):
				braces, mode = append(braces, 0), code
				i++
			default:
				content[i] = true
			}
		}
		i++
	}
	return content
}

// jsLineEnd is where the line that holds source[i] ends: its terminator, or
// the end of the source.
func jsLineEnd(source string, i int) int {
	for ; i < len(source); i++ {
		if jsLineTerminatorLen(source, i) > 0 {
			return i
		}
	}
	return i
}

// jsLineTerminatorLen is the length of the line terminator at source[i]
// (LF, CR, U+2028, U+2029), 0 for none.
func jsLineTerminatorLen(source string, i int) int {
	switch {
	case source[i] == '\n' || source[i] == '\r':
		return 1
	case strings.HasPrefix(source[i:], "\u2028"), strings.HasPrefix(source[i:], "\u2029"):
		return 3
	}
	return 0
}

// jsWhiteSpaceLen is the length of the JavaScript white space at source[i]
// (tab, vertical tab, form feed, space, U+FEFF and any space separator), 0
// for none.
func jsWhiteSpaceLen(source string, i int) int {
	switch source[i] {
	case '\t', '\v', '\f', ' ':
		return 1
	}
	if source[i] < utf8.RuneSelf {
		return 0
	}
	r, n := utf8.DecodeRuneInString(source[i:])
	if r == '\uFEFF' || unicode.Is(unicode.Zs, r) {
		return n
	}
	return 0
}

// jsPlaceholdersQuoted reports whether every `{{env:KEY}}` of a JavaScript
// source stands whole inside a string literal's content.
func jsPlaceholdersQuoted(source string) bool {
	content := jsStringContent(source)
	for _, at := range browserEnvRef.FindAllStringIndex(source, -1) {
		for i := at[0]; i < at[1]; i++ {
			if !content[i] {
				return false
			}
		}
	}
	return true
}
