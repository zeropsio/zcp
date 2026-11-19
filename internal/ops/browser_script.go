package ops

import (
	"fmt"
	"strings"
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
// backslash or the character it escapes, not a template's `${…}`. It reads
// only what it can be sure of and marks nothing from where it cannot tell: a
// `/` in code that opens no comment may open a regular expression, whose
// body could hold any quote, and a ' or " string that runs into a line end
// is no string.
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
	for i := 0; i < len(source); i++ {
		c := source[i]
		switch mode {
		case code:
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
				switch {
				case strings.HasPrefix(source[i:], "//"):
					end := strings.IndexAny(source[i:], "\r\n")
					if end < 0 {
						return content
					}
					i += end
				case strings.HasPrefix(source[i:], "/*"):
					end := strings.Index(source[i+2:], "*/")
					if end < 0 {
						return content
					}
					i += end + 3
				default:
					return content
				}
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
	}
	return content
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
