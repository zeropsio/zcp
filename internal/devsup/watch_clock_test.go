//go:build linux || darwin

package devsup

import "time"

// manualWatchClock completes only timers whose supplied deadline was reached.
// It also retains cancelled callbacks, so obsolete burst generations are tested.
type manualWatchClock struct {
	at     time.Time
	timers []*manualWatchTimer
}

type manualWatchTimer struct {
	at      time.Time
	fire    func()
	stopped bool
}

func (c *manualWatchClock) now() time.Time { return c.at }
func (c *manualWatchClock) afterFunc(wait time.Duration, f func()) watchTimer {
	t := &manualWatchTimer{at: c.at.Add(wait), fire: f}
	c.timers = append(c.timers, t)
	return t
}
func (t *manualWatchTimer) Stop() bool {
	was := t.stopped
	t.stopped = true
	return !was
}
func (c *manualWatchClock) advance(by time.Duration) {
	c.at = c.at.Add(by)
	pending := c.timers
	c.timers = nil
	for _, t := range pending {
		if !t.at.After(c.at) {
			t.fire()
		} else {
			c.timers = append(c.timers, t)
		}
	}
}
