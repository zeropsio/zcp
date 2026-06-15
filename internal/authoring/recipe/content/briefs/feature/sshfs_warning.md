# SSHFS warning — never local-build under the mount

Local `npm install` / `npx build` / `vite build` against
`/var/www/<hostname>dev/` (the SSHFS mount) tunnels every file IO
through FUSE — 10-100× slower than native, and the artifacts miss the
container's platform-injected env vars. Run framework CLIs via
`ssh <hostname>dev "cd /var/www && <cmd>"` instead.

When debugging an unfamiliar build failure, check the build site
FIRST. Run-21 evidence: features-2nd burned 8 minutes in a
Vite-on-SSHFS ESM-import rabbit hole because this warning was buried
in the middle of a different brief.

The full mount-vs-container execution-split rule lives in
`principles/mount-vs-container.md` (injected below).
