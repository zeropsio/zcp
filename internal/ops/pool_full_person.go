package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// LeasePoolFullError carries the pool reading behind a refused acquire.
// The platform error addresses an agent; PersonWords addresses the crew editor.
type LeasePoolFullError struct {
	*platform.PlatformError
	pool      string
	usage     poolUsage
	cause     string
	atMaximum bool
}

func (e *LeasePoolFullError) Unwrap() error { return e.PlatformError }

// PersonWords returns the two sentences or sentence pairs the Mate joins at Apply.
func (e *LeasePoolFullError) PersonWords() (message, suggestion string) {
	u := e.usage
	message = "The pool " + e.pool + " is full"
	if e.cause != "" && !e.atMaximum {
		message = "The pool " + e.pool + " has no free container"
	}
	if u.listed {
		switch {
		case u.max == 1 && u.leased == 1:
			message += ": its only container is taken"
		case u.max > 0 && u.leased == u.max:
			message += fmt.Sprintf(": all %d of its %d containers are taken", u.leased, u.max)
		case u.max > 0:
			message += fmt.Sprintf(": %s (maximum %d)", takenContainers(u.leased), u.max)
		default:
			message += ": " + takenContainers(u.leased)
		}
	}
	if places := u.personPlaces(e.pool); len(places) > 0 {
		if !u.listed {
			message += " — this Mate last recorded "
		} else {
			message += " — "
		}
		message += strings.Join(places, ", ")
	}
	message += "."
	if e.cause != "" && !e.atMaximum {
		message += fmt.Sprintf(" Zerops could not add a container: “%s”.", e.cause)
	}

	var actions []string
	if len(u.yours) > 0 {
		actions = append(actions, "archive a conversation that holds one")
	}
	if len(u.crew) > 0 {
		actions = append(actions, "remove a builder from the crew")
	}
	switch {
	case len(actions) > 0:
		suggestion = "Free one: " + strings.Join(actions, ", or ") + "."
	case u.otherHolders() > 0:
		suggestion = "Ask one of the holders to free a container."
	case len(u.stuck) > 0:
		suggestion = "Check the affected containers in Zerops."
	default:
		suggestion = "Check who holds the containers in Zerops and ask a holder to free one."
	}
	switch {
	case u.max >= poolContainerCeiling:
		suggestion += fmt.Sprintf(" Raising the pool's maximum will not help: %d is the most a pool holds.", poolContainerCeiling)
	case e.atMaximum && u.max > 0:
		suggestion += fmt.Sprintf(" Or raise the pool's maximum containers in Zerops (up to %d); each container it adds needs its preview port declared in the pool's run.ports first.", poolContainerCeiling)
	}
	return message, suggestion
}

func takenContainers(n int) string {
	if n == 1 {
		return "1 container is taken"
	}
	return fmt.Sprintf("%d containers are taken", n)
}

func (u poolUsage) personPlaces(pool string) []string {
	var places []string
	conversations := make([]string, 0, len(u.yours))
	for _, v := range u.yours {
		name := v.Alias
		if u.read {
			name += " (" + personActivity(v, u.now) + ")"
		}
		conversations = append(conversations, name)
	}
	if len(conversations) > 0 {
		places = append(places, joinAnd(conversations)+" by this Mate's conversations")
	}
	if len(u.crew) > 0 {
		crew := " by this Mate's crew"
		if len(conversations) > 0 {
			crew = " by its crew"
		}
		places = append(places, joinAnd(u.crewAliases())+crew)
	}
	if others := u.otherHolders(); others > 0 {
		places = append(places, fmt.Sprintf("%d by someone else", others))
	}
	for _, c := range u.stuck {
		state := "failed: it still takes a place"
		if c.LockState == swarmLockDeleting {
			state = "being removed: try again in about half a minute"
		}
		places = append(places, leaseAlias(pool, c.Number)+" ("+state+")")
	}
	return places
}

func personActivity(v LeaseView, now time.Time) string {
	l := livenessOf(v)
	var when string
	switch {
	case l.last.IsZero():
		when = "activity unknown"
	case l.inUse:
		when = "in use now"
	case !l.previewSeen:
		when = "last checkout change " + agoText(now.Sub(l.last)) + " ago, preview activity unknown"
	default:
		when = "idle " + agoText(now.Sub(l.last))
	}
	if v.Unsaved == nil {
		return when + ", unsaved work unknown"
	}
	var counts []string
	if n := v.Unsaved.FileCount; n > 0 {
		counts = append(counts, countedWork(n, "uncommitted or untracked file"))
	}
	if n := v.Unsaved.UnpushedCommits; n > 0 {
		counts = append(counts, countedWork(n, "unpushed commit"))
	}
	if len(counts) == 0 {
		return when + ", nothing unsaved"
	}
	return when + ", " + joinAnd(counts)
}

func countedWork(n int, what string) string {
	if n != 1 {
		what += "s"
	}
	return fmt.Sprintf("%d %s", n, what)
}
