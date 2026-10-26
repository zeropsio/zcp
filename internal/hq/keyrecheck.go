package hq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// KeyVerdict is HQ's answer to a key recheck.
type KeyVerdict string

const (
	// KeyHeld: HQ asked Zerops and the key still resolves to this project.
	KeyHeld KeyVerdict = "held"
	// KeyRefused: HQ asked Zerops and Zerops refuses the key; HQ has cut the
	// Mate off.
	KeyRefused KeyVerdict = "refused"
)

// keyRefusedCode is the code of HQ's 401 that says Zerops refused the key,
// as against a 401 that says the call carried none.
const keyRefusedCode = "key_refused"

// RecheckKey asks HQ to verify the key at once: `POST /api/mate/key-recheck`.
// HQ runs its own `/user/info` with the key and trusts nothing zcp says, so
// the verdict is HQ's and changes nothing in zcp. An HQ that could not tell
// (503 zerops_unanswered), one that does not answer, and any other refusal
// are errors with no verdict.
func (c Client) RecheckKey(ctx context.Context) (KeyVerdict, error) {
	err := c.call.json(ctx, http.MethodPost, "/api/mate/key-recheck", c.authorization(), nil, nil)
	switch {
	case err == nil:
		return KeyHeld, nil
	case refusedAs(err, keyRefusedCode):
		return KeyRefused, nil
	}
	return "", fmt.Errorf("key recheck: %w", err)
}

// recheckPeriod is the least time between two asks of HQ: HQ answers the
// same inside the minute, so asking sooner spends a call to learn nothing.
const recheckPeriod = time.Minute

// KeyRechecker tells HQ of each refusal Zerops gives this container's key,
// at most once a minute (spec-mate §3.3, TC-21). It decides nothing locally:
// HQ's answer is only reported.
type KeyRechecker struct {
	// Open is a client of HQ under the key the container holds now.
	Open func() (Client, error)
	Now  func() time.Time
	// Tell receives one line per ask, HQ's answer in words.
	Tell func(string)

	mu    sync.Mutex
	asked time.Time
}

// Refused is the container's key refused by Zerops: HQ is asked to recheck it
// unless it was asked inside the last minute.
func (r *KeyRechecker) Refused(ctx context.Context) {
	if !r.admit() {
		return
	}
	client, err := r.Open()
	if err != nil {
		r.Tell(fmt.Sprintf("hq key recheck not asked: %v", err))
		return
	}
	verdict, err := client.RecheckKey(ctx)
	var unavailable *UnavailableError
	switch {
	case errors.As(err, &unavailable) && unavailable.Code == "zerops_unanswered":
		r.Tell("hq key recheck: HQ could not tell, Zerops did not answer it")
	case err != nil:
		r.Tell(fmt.Sprintf("hq key recheck: %v", err))
	case verdict == KeyRefused:
		r.Tell("hq key recheck: Zerops refuses the key, HQ has cut this Mate off")
	default:
		r.Tell("hq key recheck: the key still resolves at HQ")
	}
}

// admit takes the minute's one ask, or says it was taken.
func (r *KeyRechecker) admit() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.Now()
	if !r.asked.IsZero() && now.Sub(r.asked) < recheckPeriod {
		return false
	}
	r.asked = now
	return true
}
