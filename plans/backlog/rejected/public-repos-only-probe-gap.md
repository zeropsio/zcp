# REJECTED: "Public repositories only" PAT trap as a PROBE gap

**Why rejected** (2026-06-17): the finding's central premise is factually false vs
HEAD. It claimed the CHECK is a read-only `ls-remote` probe, so a read-only "Public
repositories" PAT sails through and 403s later. Both modes already run a WRITE-class
probe — container `BuildGitWritePushProbeCommand`, local `RunGitAuthProbeLocal`, both
`git push --dry-run` against git-receive-pack — which 403s a read-only PAT AT setup
(shipped 11de178f). The investigator misread a STALE comment ("read-only auth check")
as behavior; that comment was itself residue and is fixed under R0 (commit
6e9bffa4). The probe already closes the gap.

**What DID ship from the same finding** (the legitimate, preventive part): the
credential recommendation now steers the user to "Only select repositories → your
repo, NOT Public repositories" up front, and the auth-rejection diagnostic NAMES the
trap (commit 6e9bffa4, F7/#2). So the trap is handled both reactively (probe rejects
+ diagnostic names) and preventively (recommendation steers) — just not as the
"probe doesn't catch it" framing this finding proposed.

**Refs**: plans/minor-findings-rootcause-2026-06-17.md (F8).
