package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/platform"
)

// watchKeyRefusal makes client ask the Mate's HQ to recheck the project key
// whenever Zerops refuses it (spec-mate §3.3, TC-21): at most once a minute,
// off the refused call's path, the key and HQ's address read from identity
// at the ask so a regenerated key is the one presented. HQ's answer is
// logged to stderr and decides nothing here.
func watchKeyRefusal(client *platform.ZeropsClient, identity func() hq.Identity, tell func(string)) {
	rechecker := &hq.KeyRechecker{
		Open: func() (hq.Client, error) {
			opened, err := hq.Open(&http.Client{Timeout: hqCallTimeout}, identity())
			return opened.Once(), err
		},
		Now:  time.Now,
		Tell: tell,
	}
	client.OnKeyRefused(func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), hqCallTimeout)
			defer cancel()
			rechecker.Refused(ctx)
		}()
	})
}

// watchLiveKeyRefusal is watchKeyRefusal for a container: the identity from
// the live env store, HQ's answer to the log.
func watchLiveKeyRefusal(client *platform.ZeropsClient) {
	watchKeyRefusal(client, func() hq.Identity {
		return hq.IdentityFrom(mate.LiveLookup(mate.LiveEnvStorePath))
	}, func(line string) { log.Print(line) })
}
