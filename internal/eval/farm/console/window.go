package console

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// defaultWindow is the §8.4/§8.3 default when a window query is omitted
// ("default 24h, same rule as §8.4" — §8.3 FM-51's /findings page).
const defaultWindow = 24 * time.Hour

// ParseWindow parses a §8.4 window: a Go duration ("24h") or "<n>d" ("7d").
// "" parses as defaultWindow. A non-positive or unparsable value is an
// error — the caller renders it as 400.
func ParseWindow(s string) (time.Duration, error) {
	if s == "" {
		return defaultWindow, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid window %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid window %q", s)
	}
	return d, nil
}
