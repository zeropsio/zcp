# Provision — 3b. Git config + init for every dev mount

Multi-codebase plans repeat the full container-side config + init + initial commit SSH call for **every** provisioned dev mount. One codebase = one mount = one SSH invocation.

The number of mounts is the same number `mount-dev-filesystem` produced — one per `sharesCodebaseWith` group. The authoritative enumeration of dev mounts per plan shape lives under the generate step's "Write ALL setups at once" section; match that row to the plan and run one SSH call per mount listed there.

Different mounts are independent — their `.git/` trees do not share an index.lock, so the SSH calls across different hostnames may run in parallel. Sequential execution is always safe; parallel execution is safe across different hostnames.
