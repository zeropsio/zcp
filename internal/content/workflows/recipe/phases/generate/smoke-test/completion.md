# Smoke-test substep — completion predicate

The smoke-test substep counts as complete when, for every dev mount in the plan:

- Step 1 (package manager install) exited zero on that container.
- Step 2 (compile or type-check, where applicable) exited zero on that container.
- Step 3 (process binds to the expected port, or implicit webserver returns 200 on the HTTP port) exited zero on that container.
- Any fixes prompted by smoke-test failures live on the mount — not as deferred work to be re-tried by `zerops_deploy`.

Attest only when every predicate above holds across every codebase.
