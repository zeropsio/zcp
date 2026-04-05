package ops

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strconv"

	"github.com/zeropsio/zcp/internal/platform"
)

// preprocessorPattern matches full Zerops-preprocessor invocations embedded in
// an env-var value (e.g. "<@generateRandomString(<32>)>" or a prefix/suffix
// combination like "base64:<@generateRandomString(<32>)>"). Only the subset of
// functions that are safe and useful for workspace secret setup is supported;
// anything else returns a clear error so the agent fixes their call instead of
// getting a literal unexpanded string in the platform.
var preprocessorPattern = regexp.MustCompile(`<@([a-zA-Z][a-zA-Z0-9]*)\(([^)]*)\)>`)

// supportedPreprocessorFns lists the function names this expander understands.
// Exposed in error messages so the agent knows what's available.
var supportedPreprocessorFns = []string{"generateRandomString"}

const maxRandomStringLen = 4096

// expandPreprocessor expands Zerops-preprocessor functions embedded in an
// env-var value. Mirrors the subset of platform preprocessor syntax that
// workspace secret setup needs, so the workspace secret is produced by the
// same expression that appears in the recipe deliverable's project.envVariables.
//
// Supported:
//   - <@generateRandomString(<N>)> — N random alphanumeric characters
//
// Any other <@fn(...)> syntax returns an error listing supported functions —
// the agent is expected to fix the call (not fall back to a literal string).
func expandPreprocessor(value string) (string, error) {
	// Fast path: no preprocessor syntax at all.
	if !containsPreprocessor(value) {
		return value, nil
	}
	var firstErr error
	out := preprocessorPattern.ReplaceAllStringFunc(value, func(match string) string {
		if firstErr != nil {
			return match
		}
		sub := preprocessorPattern.FindStringSubmatch(match)
		if len(sub) < 3 {
			firstErr = unsupportedPreprocessor(match, "could not parse")
			return match
		}
		fn, args := sub[1], sub[2]
		switch fn {
		case "generateRandomString":
			n, err := parseLenArg(args)
			if err != nil {
				firstErr = preprocessorErr(match, fmt.Sprintf("invalid length argument: %v", err))
				return match
			}
			if n <= 0 || n > maxRandomStringLen {
				firstErr = preprocessorErr(match, fmt.Sprintf("length must be between 1 and %d", maxRandomStringLen))
				return match
			}
			s, err := randomAlphanumString(n)
			if err != nil {
				firstErr = preprocessorErr(match, fmt.Sprintf("rng failure: %v", err))
				return match
			}
			return s
		default:
			firstErr = unsupportedPreprocessor(match, "function not supported by zerops_env (set it via a deliverable-style import.yaml if you need the full preprocessor)")
			return match
		}
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// containsPreprocessor reports whether the value carries preprocessor syntax.
// Used as a fast path so plain values skip regex work entirely.
func containsPreprocessor(value string) bool {
	for i := 0; i+1 < len(value); i++ {
		if value[i] == '<' && value[i+1] == '@' {
			return true
		}
	}
	return false
}

// parseLenArg extracts the integer length from a "<N>" argument wrapper,
// matching the platform preprocessor's nested-bracket convention.
func parseLenArg(args string) (int, error) {
	if len(args) < 3 || args[0] != '<' || args[len(args)-1] != '>' {
		return 0, fmt.Errorf("expected <N>, got %q", args)
	}
	return strconv.Atoi(args[1 : len(args)-1])
}

// randomAlphanumString returns n cryptographically-random alphanumeric
// characters (matches the platform's generateRandomString output set).
func randomAlphanumString(n int) (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf), nil
}

func unsupportedPreprocessor(match, reason string) error {
	return preprocessorErr(match, fmt.Sprintf("%s — supported: %v", reason, supportedPreprocessorFns))
}

func preprocessorErr(match, detail string) error {
	return platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("preprocessor expansion failed for %q: %s", match, detail),
		"Use <@generateRandomString(<N>)> for random-string secrets, or omit preprocessor syntax for literal values",
	)
}
