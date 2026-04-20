# pre-ship-assertions

Before returning, you run a pre-ship assertion set against your mount. Every assertion below states a positive-form invariant and the shell command that proves it holds. Aggregate exit must be 0. When any assertion fails, repair the code and re-run that specific assertion until it passes, then re-run the full set.

## Primary source: the contract

Run every `FixRecurrenceRules[].preAttestCmd` whose `appliesTo` list contains your role or `any`. Each rule states its positive form; non-zero exit = repair the code until the rule holds.

## Codebase-level assertions

Every assertion below is a positive-form declaration paired with the command that proves it. Substitute your mount's hostname for `$HOST` at the top of the block.

```bash
HOST={{.Hostname}}
MOUNT=/var/www/$HOST

# zerops.yaml carries no self-shadow (KEY: ${KEY}) lines, if the file exists.
# zerops.yaml is written later at a separate substep; at this substep no
# file is expected.
test ! -f $MOUNT/zerops.yaml \
    || { echo "FAIL: zerops.yaml is present at scaffold-return time"; exit 1; }

# README.md is not authored at this substep. READMEs are written later at
# deploy.readmes. If the framework scaffolder created one, you deleted it.
test ! -f $MOUNT/README.md \
    || { echo "FAIL: README.md is present at scaffold-return time"; exit 1; }

# .env is absent; .env.example is present (with a non-empty body if the
# scaffolder produced one). Runtime reads env from the platform, not from .env.
test ! -f $MOUNT/.env \
    || { echo "FAIL: .env is committed to the codebase"; exit 1; }
test -f $MOUNT/.env.example \
    || { echo "FAIL: .env.example is missing"; exit 1; }

# .gitignore is present and excludes node_modules, dist, .env, .DS_Store
# plus framework-specific cache directories.
grep -qE '^(node_modules|/node_modules)' $MOUNT/.gitignore \
    || { echo "FAIL: .gitignore missing node_modules"; exit 1; }
grep -q 'dist' $MOUNT/.gitignore \
    || { echo "FAIL: .gitignore missing dist"; exit 1; }
grep -q '\.env' $MOUNT/.gitignore \
    || { echo "FAIL: .gitignore missing .env"; exit 1; }

# The framework scaffolder's .git/ is gone. The canonical container-side
# git init runs later once your scaffold returns; a residual .git/ would
# collide with .git/index.lock.
! ssh $HOST "test -d /var/www/.git" \
    || { echo "FAIL: /var/www/.git is present on $HOST"; exit 1; }

# No scaffold-phase test artifacts committed. Pre-ship scripts run ephemerally
# from /tmp or via bash -c; they do not ship with the codebase. Runtime shell
# scripts referenced from zerops.yaml (e.g. a healthcheck.sh referenced from
# run.start) are legitimate — the `no-scaffold-test-artifacts` contract rule
# filters those out.
! find $MOUNT -maxdepth 3 \( -name 'preship.sh' -o -name '*.assert.sh' \) \
    | grep -q . \
    || { echo "FAIL: scaffold test artifact committed to the codebase"; exit 1; }

exit 0
```

## After assertions pass

Run the framework's build or compile step via SSH and inspect the tail for errors. Fix every compile or type error before returning.

```bash
ssh {{.Hostname}} "cd /var/www && <framework-build-command> 2>&1 | tail -40"
```

Examples: `npm run build` (Nest / Svelte / Vite), `go build ./...` (Go), `composer install --no-dev && php artisan config:cache` (Laravel), `cargo build --release` (Rust). Your role addendum below names the exact command for your framework.

Do NOT start a dev server. Smoke testing is owned at a later substep.

## Record a fact every time you repaired a failing assertion

Each time an assertion fails and you fix it, call `mcp__zerops__zerops_record_fact type=fix_applied` so the writer classifies the event correctly as a scaffold decision rather than a platform gotcha. A self-inflicted bug you caught before return is not a porter-facing trap.
