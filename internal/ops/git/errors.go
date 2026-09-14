package git

import (
	"fmt"
	"strings"
)

// wrapErr formats a Runner failure with its stderr, mirroring the
// fmt.Errorf("op: %w", err) discipline while surfacing the diagnostic text
// a bare exec.ExitError would otherwise drop.
func wrapErr(op string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", op, err)
	}
	return fmt.Errorf("%s: %w (%s)", op, err, stderr)
}
