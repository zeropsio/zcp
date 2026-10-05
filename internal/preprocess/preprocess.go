// Package preprocess runs Zerops preprocessor expressions through the official
// zParser library, producing values byte-for-byte identical to what the
// platform produces when importing a recipe at https://app.zerops.io/recipes.
//
// The wrapper exists to give zcp a narrow, context-aware Go API over zParser,
// with sane limits applied (max function count, context timeout) so an
// expansion can't loop or hang.
package preprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zParser/v2/src/parser"
)

// MaxFunctionCount caps how many preprocessor function calls a single
// expansion may execute. High enough for realistic use (generate a random
// string + hash/encode it a few times), low enough to bound runtime cost.
const MaxFunctionCount = 200

// DefaultTimeout bounds a single expansion's wall-clock cost. bcrypt + argon2id
// modifiers are intentionally slow, so the limit has to absorb a handful of
// chained KDF calls — 15s is well above realistic recipe needs.
const DefaultTimeout = 15 * time.Second

// Expand runs a single preprocessor expression through zParser and returns
// the expanded string. The input may be pure preprocessor syntax
// (`<@generateRandomString(<32>)>`), plain text (returned unchanged), or a
// mix (`base64:<@generateRandomString(<32>)>` — though plain base64 prefixes
// are a known footgun, see recipe.md).
//
// Values with no zParser syntax short-circuit (no parser spin-up), so this
// is safe to call on every value regardless of shape.
func Expand(ctx context.Context, input string) (string, error) {
	if !containsSyntax(input) {
		return input, nil
	}
	return expand(ctx, input, nil)
}

// Batch expands a map of values through a single zParser instance, so
// setVar/getVar calls in later entries can reference variables set by
// earlier entries. Iteration follows the provided key order (pass an
// ordered slice if setVar/getVar correlations matter).
//
// If any entry fails, the error names the failing key and the batch is
// aborted — a partial result is never returned.
func Batch(ctx context.Context, keys []string, inputs map[string]string) (map[string]string, error) {
	if len(inputs) == 0 {
		return map[string]string{}, nil
	}
	if len(keys) == 0 {
		// If the caller didn't specify an order, derive one from map keys —
		// setVar/getVar correlations won't be deterministic, but at least
		// the function signature doesn't silently drop values.
		keys = make([]string, 0, len(inputs))
		for k := range inputs {
			keys = append(keys, k)
		}
	}

	// One shared parser keeps the variable store alive across all entries.
	// zParser's NewParser takes in/out streams and parses them in one go, so
	// we pipe each value through a fresh stream pair while holding the
	// variable store via option injection. Since the current zParser API
	// does not let us inject a pre-populated store, we instead inline all
	// values into one input stream separated by a unique delimiter the
	// parser will pass through untouched, and split the output by the same
	// delimiter.
	const delim = "\x00ZCP_PREPROCESS_DELIM\x00"
	var combined strings.Builder
	for i, key := range keys {
		if _, ok := inputs[key]; !ok {
			return nil, fmt.Errorf("batch preprocess: key %q has no input", key)
		}
		if i > 0 {
			combined.WriteString(delim)
		}
		combined.WriteString(inputs[key])
	}

	expanded, err := expand(ctx, combined.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("batch preprocess: %w", err)
	}

	parts := strings.Split(expanded, delim)
	if len(parts) != len(keys) {
		return nil, fmt.Errorf("batch preprocess: expected %d output parts, got %d (delimiter leaked through a function output?)", len(keys), len(parts))
	}
	out := make(map[string]string, len(keys))
	for i, key := range keys {
		out[key] = parts[i]
	}
	return out, nil
}

// containsSyntax is a fast pre-check: any zParser expression starts with
// either `<@` (function call) or a plain `<` that would open a static
// string. We match the function-call form only — plain `<` appears in too
// much legitimate text (SQL, shell, HTML snippets) to gate on reliably,
// and the deliverable's recipes only ever use `<@...>` forms.
func containsSyntax(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '<' && s[i+1] == '@' {
			return true
		}
	}
	return false
}

// expand runs the parser once with zcp's standard limits. Optional variable
// store parameter is reserved for future batch use — currently always nil.
func expand(ctx context.Context, input string, _ map[string]string) (string, error) {
	if _, deadline := ctx.Deadline(); !deadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}

	in := strings.NewReader(input)
	var out bytes.Buffer

	p := parser.NewParser(
		in, &out,
		parser.WithMaxFunctionCount(MaxFunctionCount),
		parser.WithMultilineOutputHandling(parser.MultilinePreserved),
	)
	if err := parse(ctx, p); err != nil {
		return "", wrapParseError(err)
	}
	return out.String(), nil
}

// errParserPanic is zParser failing by a panic rather than an error. v2.1.2
// panics on a call whose next argument follows its comma directly —
// <@generateRandomInt(<10>,<50>)> — where <@generateRandomInt(<10>, <50>)>
// expands. The form is not rewritten: the platform's import runs the same
// zParser, so a value zcp accepted could still fail there.
var errParserPanic = errors.New("the preprocessor could not read this expression; write a space after each comma between arguments, as in <@generateRandomInt(<10>, <50>)>")

// parse runs p, answering a panic inside zParser as errParserPanic: one bad
// value must never take the whole zcp process — and the agent's MCP server —
// down with it. The panic's own words stay out: they say nothing the agent
// can act on.
func parse(ctx context.Context, p *parser.Parser) (err error) {
	defer func() {
		if recover() != nil {
			err = errParserPanic
		}
	}()
	return p.Parse(ctx)
}

// wrapParseError converts zParser's errors into something the MCP layer can
// surface cleanly: zParser's reason, never the input — a value is often a
// secret, and the error reaches the agent and the person's screen. The
// caller names the entry that failed.
func wrapParseError(err error) error {
	// Deadline exceeded surfaces as a wrapped context error — preserve that
	// so callers can distinguish timeout from syntax error.
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("preprocess: timeout (%s)", DefaultTimeout)
	}
	return fmt.Errorf("preprocess: %w", err)
}
