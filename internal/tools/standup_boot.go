package tools

import "context"

// StandupAtBoot is the stand-up zcp's Mate service runs at its start, beside
// the server it supervises (internal/service mateLaunchSetup), where a pooled
// part has no build: adopt, ensure each pool's bearer, commit-deploy main to
// every pool and stage without a build, and write the verdict to the status
// file at statusPath. It writes rows for pooled parts only; with none it
// returns at once. env is the container's live env store as it is at each
// call (as KeepMateDevGit takes it); each line said goes to log.
func StandupAtBoot(ctx context.Context, env func() func(string) string, statusPath string, log func(string)) {
}
