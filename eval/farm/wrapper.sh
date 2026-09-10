#!/bin/sh
# eval/farm/wrapper.sh — run-project supervisor + child
# (docs/spec-eval-farm.md §2.3, FM-13). Started detached by the run
# project's last init command (§2.1 FM-10); reads everything from its own
# environment (§2.2 FM-12), never from a file drop or a second RPC.
#
# Supervisor: forks itself as a child ("$0" --child), waits on it, and — via
# an EXIT/INT/TERM/HUP trap that fires regardless of how the child ended —
# redacts, uploads runs/<runId>/results/ then runs/<runId>/capture/, and
# writes runs/<runId>/done.json last (FM-3/FM-4). A SIGKILL of the
# supervisor itself is the one case nothing runs after (FM-13): no trap, no
# upload, no done.json — the explicit "blocked: no bundle" outcome.
#
# Child: PUTs started.json, downloads the pinned evaluator/candidate
# binaries and the scenario tree, verifies both binary digests before
# running either, creates a private HOME, and execs the evaluator's
# unchanged spec-testing-architecture.md §10.4 binding — replacing its own
# process image so the pidfile below always names whichever process is
# actually doing the work.
#
# Env contract: docs/spec-eval-farm.md §2.2 (FM-12) plus one addition not
# yet in that table — ZCP_FARM_SCENARIOS_DIGEST, the scenario tree digest
# under which `scenarios/<digest>/**` is fetched (flagged in the S1a build
# report; FM-12's table needs this row). ZCP_FARM_RUNDIR is a wrapper-local
# test seam, not part of the run descriptor: production leaves it unset (a
# fresh `mktemp -d` is used each run); the offline test sets it so it can
# find the pid files and local state without scraping stdout.

# shellcheck disable=SC2329
# ^ every helper below is reached only via the `trap finish_and_upload ...`
# handler, or transitively from it; shellcheck's static reachability check
# does not follow a trap's function-name argument as a call.
# shellcheck disable=SC2154
# ^ projectId is the platform-injected run-descriptor env (lowercase by
# platform convention, docs/spec-eval-farm.md §2.2; internal/runtime/runtime.go
# reads the same var the same way).

set -eu

AWS_SIGV4="aws:amz:us-east-1:s3"

# ---- S3 helpers (path-style, curl --aws-sigv4; docs/spec-eval-farm.md §1.3 FM-8) ----

s3_put() {
	# $1 = local file, $2 = bucket key
	curl -sS -f --aws-sigv4 "$AWS_SIGV4" \
		--user "$ZCP_FARM_S3_KEY:$ZCP_FARM_S3_SECRET" \
		-X PUT --data-binary "@$1" \
		"$ZCP_FARM_S3_URL/$ZCP_FARM_S3_BUCKET/$2" >/dev/null
}

s3_get() {
	# $1 = bucket key, $2 = local dest file
	curl -sS -f --aws-sigv4 "$AWS_SIGV4" \
		--user "$ZCP_FARM_S3_KEY:$ZCP_FARM_S3_SECRET" \
		-o "$2" \
		"$ZCP_FARM_S3_URL/$ZCP_FARM_S3_BUCKET/$1"
}

# s3_list prints one key per line under prefix ($1), following continuation
# tokens (docs/spec-eval-farm.md §1: "?list-type=2&prefix=").
s3_list() {
	prefix="$1"
	token=""
	while :; do
		url="$ZCP_FARM_S3_URL/$ZCP_FARM_S3_BUCKET?list-type=2&prefix=$prefix"
		if [ -n "$token" ]; then
			url="$url&continuation-token=$token"
		fi
		body=$(curl -sS -f --aws-sigv4 "$AWS_SIGV4" \
			--user "$ZCP_FARM_S3_KEY:$ZCP_FARM_S3_SECRET" "$url")
		printf '%s\n' "$body" | grep -o '<Key>[^<]*</Key>' | sed -e 's#<Key>##' -e 's#</Key>##'
		case "$body" in
		*'<IsTruncated>true</IsTruncated>'*) ;;
		*) break ;;
		esac
		token=$(printf '%s\n' "$body" | grep -o '<NextContinuationToken>[^<]*</NextContinuationToken>' | sed -e 's#<NextContinuationToken>##' -e 's#</NextContinuationToken>##')
		[ -z "$token" ] && break
	done
}

# download_prefix fetches every object under prefix ($1, must end in "/")
# into dest ($2), preserving the relative layout.
download_prefix() {
	prefix="$1"
	dest="$2"
	mkdir -p "$dest"
	s3_list "$prefix" | while IFS= read -r key; do
		[ -z "$key" ] && continue
		rel=${key#"$prefix"}
		mkdir -p "$dest/$(dirname "$rel")"
		s3_get "$key" "$dest/$rel"
	done
}

# ---- content digests ----------------------------------------------------

# tree_digest reproduces internal/eval/farm/digest.go's TreeDigest: sha256
# over the sorted "<relpath>\n<file-sha256>\n" lines of every regular file
# under dir ($1), symlinks skipped, paths relative and slash-separated.
tree_digest() {
	dir="$1"
	if [ ! -d "$dir" ]; then
		printf '' | sha256sum | awk '{print $1}'
		return
	fi
	(
		cd "$dir" || exit 1
		find . -type f | sed 's#^\./##' | LC_ALL=C sort
	) | while IFS= read -r rel; do
		filehash=$(sha256sum "$dir/$rel" | awk '{print $1}')
		printf '%s\n%s\n' "$rel" "$filehash"
	done | sha256sum | awk '{print $1}'
}

# ---- redaction (docs/spec-eval-farm.md §1.3 FM-7) ------------------------

# redact_dir replaces every literal occurrence of value ($2) with
# "<redacted>" in every regular file under dir ($1). No-op for an empty
# value or a missing dir. Escapes the three BRE-special characters an
# opaque token value could contain: backslash, forward slash (the sed
# delimiter), and ampersand (special in a sed replacement). Every file it
# actually rewrote (checked with a plain-text grep first, so a file the
# value never appeared in is never touched or logged) is appended to
# $RUNDIR/redacted.log — D8 needs that list to patch capture/manifest.json's
# per-file size/sha256 afterward and to name what changed in done.json.
redact_dir() {
	dir="$1"
	value="$2"
	[ -z "$value" ] && return 0
	[ -d "$dir" ] || return 0
	escaped=$(printf '%s' "$value" | sed -e 's/[\\/&]/\\&/g')
	find "$dir" -type f | while IFS= read -r f; do
		grep -qF -- "$value" "$f" 2>/dev/null || continue
		sed -i.bak "s/$escaped/<redacted>/g" "$f"
		rm -f "$f.bak"
		printf '%s\n' "$f" >>"$RUNDIR/redacted.log"
	done
}

redact_known_secrets() {
	dir="$1"
	redact_dir "$dir" "${ZCP_FARM_S3_SECRET:-}"
	redact_dir "$dir" "${ZCP_FARM_S3_KEY:-}"
	redact_dir "$dir" "${CLAUDE_CODE_OAUTH_TOKEN:-}"
	redact_dir "$dir" "${ZCP_E2E_LAUNCH_KEY:-}"
	redact_dir "$dir" "${ZCP_API_KEY:-}"
}

# ---- child-tree cleanup (D6) ----------------------------------------------

# session_pids prints every pid whose session id equals sid ($1). A
# reparented grandchild that moved itself into a NEW process group (its
# ppid also moves to the reaper once its immediate parent dies) keeps the
# SAME session id for as long as it lives — session, not process group, is
# the one membership that survives both reparenting and a pgid change — so
# this is the only reachable-by-construction way to find it (D6). Tries, in
# order: /proc/*/stat field 6 (the run container's Ubuntu image always has
# this); `ps -o pid=,sid=` (a Linux host without /proc mounted, still
# offers the "sid" ps keyword); a python3 os.getsid() fallback (this repo's
# macOS dev machine has neither of the above — BSD ps has no "sid" keyword
# at all — so the offline test rig needs a third tier to run at all).
# Prints nothing (never errors) when no tier is available.
session_pids() {
	sid="$1"
	if [ -d /proc ] && [ -r "/proc/$sid/stat" ]; then
		for statfile in /proc/[0-9]*/stat; do
			[ -r "$statfile" ] || continue
			pid=$(basename "$(dirname "$statfile")")
			line=$(cat "$statfile" 2>/dev/null) || continue
			# The comm field ("(name)") may itself contain spaces/parens;
			# fields after the LAST ")" are state ppid pgrp session ..., so
			# session is the 4th whitespace-separated field from there.
			rest=${line##*) }
			psid=$(printf '%s\n' "$rest" | awk '{print $4}')
			[ "$psid" = "$sid" ] && printf '%s\n' "$pid"
		done
		return 0
	fi
	if ps -eo pid=,sid= >/dev/null 2>&1; then
		ps -eo pid=,sid= | awk -v s="$sid" '$2==s{print $1}'
		return 0
	fi
	if command -v python3 >/dev/null 2>&1; then
		# Plain "<<", not "<<-": a tab-stripping heredoc would flatten
		# python's own indentation (every leading tab removed uniformly,
		# not just up to the shell function's own nesting level), so this
		# block is deliberately left-flush instead of matching the
		# surrounding shell indentation.
		python3 - "$sid" <<'PYEOF'
import os, subprocess, sys
sid = int(sys.argv[1])
try:
    out = subprocess.check_output(["ps", "-eo", "pid="]).decode()
except Exception:
    sys.exit(0)
for tok in out.split():
    try:
        pid = int(tok)
    except ValueError:
        continue
    try:
        if os.getsid(pid) == sid:
            print(pid)
    except OSError:
        pass
PYEOF
	fi
}

# kill_child_group signals every process in the child's session (D6) —
# TERM first, a grace period, then KILL, so nothing is still running by the
# time redaction/upload starts (docs/spec-eval-farm.md §2.3 FM-13: "the
# supervisor must ... kill the whole group ... BEFORE redaction + upload,
# so a bundle is final only when nothing is still running"). Both the
# child-kill path and the normal-exit path reach this: it is the sole
# cleanup step in finish_and_upload's trap.
kill_child_group() {
	[ -f "$RUNDIR/child.pid" ] || return 0
	sid=$(cat "$RUNDIR/child.pid")
	[ -z "$sid" ] && return 0

	pids=$(session_pids "$sid")
	[ -z "$pids" ] && return 0
	for pid in $pids; do
		kill -TERM "$pid" 2>/dev/null || true
	done
	sleep 0.3
	pids=$(session_pids "$sid")
	for pid in $pids; do
		kill -KILL "$pid" 2>/dev/null || true
	done
}

# ---- capture manifest sync after redaction (D8) --------------------------

# update_one_capture_manifest patches a single manifest.json's per-file
# "sizeBytes"/"sha256" entries for every file redact_dir actually changed
# UNDER THAT MANIFEST'S OWN DIRECTORY (its "files[].path" entries are
# relative to where it lives — docs/spec-eval-farm.md §1.3 FM-7,
# internal/capture's SessionManifestDocument.Files shape —
# WriteSessionManifest/ReadSessionManifest), then writes it back in place.
# No-op when nothing under that directory was redacted. Uses perl -MJSON::PP
# (core module, no jq/python assumption on the run container) — key order in
# the rewritten file is irrelevant, the reader parses JSON rather than
# diffing bytes.
update_one_capture_manifest() {
	manifest="$1"
	manifest_dir=$(dirname "$manifest")

	updates="$RUNDIR/redacted-updates.tsv"
	: >"$updates"
	sort -u "$RUNDIR/redacted.log" | while IFS= read -r f; do
		case "$f" in
		"$manifest_dir"/*) ;;
		*) continue ;;
		esac
		rel=${f#"$manifest_dir"/}
		size=$(wc -c <"$f" | tr -d ' ')
		sha=$(sha256sum "$f" | awk '{print $1}')
		printf '%s\t%s\t%s\n' "$rel" "$size" "$sha" >>"$updates"
	done

	[ -s "$updates" ] || return 0

	perl -MJSON::PP -e '
		my ($manifest_path, $updates_path) = @ARGV;
		open my $mh, "<", $manifest_path or die "open manifest: $!";
		my $raw = do { local $/; <$mh> };
		close $mh;
		my $doc = JSON::PP->new->decode($raw);

		open my $uh, "<", $updates_path or die "open updates: $!";
		my %by_path;
		while (my $line = <$uh>) {
			chomp $line;
			my ($path, $size, $sha) = split /\t/, $line;
			$by_path{$path} = { size => $size + 0, sha => $sha };
		}
		close $uh;

		for my $file (@{ $doc->{files} || [] }) {
			my $u = $by_path{$file->{path}};
			next unless $u;
			$file->{sizeBytes} = $u->{size};
			$file->{sha256} = $u->{sha};
		}

		open my $oh, ">", $manifest_path or die "write manifest: $!";
		print $oh JSON::PP->new->canonical->encode($doc);
		close $oh;
	' "$manifest" "$updates"
}

# update_capture_manifest rewrites every capture manifest.json under
# $CAPTURE_DIR after an FM-7 redaction pass: the flat legacy layout
# ($CAPTURE_DIR/manifest.json, kept for safety) and the capture window(s)
# the evaluator writes at $CAPTURE_DIR/capture-<id>/manifest.json (each
# window's own manifest, with "files[].path" relative to ITS OWN
# directory — see update_one_capture_manifest). Without this, the FM-7
# rewrite (real, load-bearing: the transcript held a sink key/secret in
# clear from an expanded initCommands=[...] var) leaves a manifest's
# recorded size+digest pointing at the PRE-redaction bytes, and
# `zcp capture` / eval.BuildBehavioralReport's own size check
# (internal/capture/read_manifest_file.go) then refuses the whole bundle
# as corrupt. No-op when there is no manifest.json anywhere under
# $CAPTURE_DIR or nothing was redacted.
update_capture_manifest() {
	[ -f "$RUNDIR/redacted.log" ] || return 0

	if [ -f "$CAPTURE_DIR/manifest.json" ]; then
		update_one_capture_manifest "$CAPTURE_DIR/manifest.json"
	fi
	for manifest in "$CAPTURE_DIR"/*/manifest.json; do
		[ -f "$manifest" ] || continue
		update_one_capture_manifest "$manifest"
	done
}

# redacted_json_array renders a JSON array of every path redact_dir logged
# (relative to $RUNDIR, e.g. "results/scenario1/meta.json",
# "capture/manifest.json") for done.json's "redacted" field. "[]" when
# nothing was redacted.
redacted_json_array() {
	if [ ! -s "$RUNDIR/redacted.log" ]; then
		printf '[]'
		return
	fi
	printf '['
	first=1
	sort -u "$RUNDIR/redacted.log" | while IFS= read -r f; do
		rel=${f#"$RUNDIR/"}
		if [ "$first" -eq 1 ]; then
			printf '"%s"' "$(json_escape "$rel")"
			first=0
		else
			printf ',"%s"' "$(json_escape "$rel")"
		fi
	done
	printf ']'
	:
}

# ---- upload ---------------------------------------------------------------

# upload_dir PUTs every regular file under dir ($1) to
# runs/$ZCP_FARM_RUN/<part ($2)>/<relpath>.
upload_dir() {
	dir="$1"
	part="$2"
	[ -d "$dir" ] || return 0
	(
		cd "$dir" || exit 1
		find . -type f | sed 's#^\./##'
	) | while IFS= read -r rel; do
		s3_put "$dir/$rel" "runs/$ZCP_FARM_RUN/$part/$rel"
	done
}

# ---- JSON -------------------------------------------------------------

json_escape() {
	printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

# ---- child (FM-13 steps 1-3, §10.4 binding) --------------------------

child_main() {
	RUNDIR="$1"

	started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	started_json="$RUNDIR/started.json"
	printf '{"runId":"%s","scenarioId":"%s","startedAt":"%s"}' \
		"$(json_escape "$ZCP_FARM_RUN")" "$(json_escape "$ZCP_FARM_SCENARIO")" "$started_at" \
		>"$started_json"
	s3_put "$started_json" "runs/$ZCP_FARM_RUN/started.json"

	evaluator_bin="$RUNDIR/evaluator"
	candidate_bin="$RUNDIR/candidate"
	scen_dir="$RUNDIR/scenarios"

	# A download failure (including a plain 404 — an unpinned SHA names no
	# object at all) refuses exactly like a hash mismatch: either way the
	# binary named by ZCP_FARM_EVALUATOR_SHA/ZCP_FARM_CANDIDATE_SHA could
	# not be verified, so it is never run (FM-13 step 3). Guarded
	# individually so a failed GET does not abort the child via `set -e`
	# before the refusal path can write execution-override.
	if ! s3_get "evaluators/$ZCP_FARM_EVALUATOR_SHA/zcp" "$evaluator_bin"; then
		printf '%s' "refused: digest mismatch" >"$RUNDIR/execution-override"
		exit 1
	fi
	if ! s3_get "candidates/$ZCP_FARM_CANDIDATE_SHA/zcp" "$candidate_bin"; then
		printf '%s' "refused: digest mismatch" >"$RUNDIR/execution-override"
		exit 1
	fi
	chmod +x "$evaluator_bin" "$candidate_bin"
	download_prefix "scenarios/$ZCP_FARM_SCENARIOS_DIGEST/" "$scen_dir"

	eval_sha=$(sha256sum "$evaluator_bin" | awk '{print $1}')
	cand_sha=$(sha256sum "$candidate_bin" | awk '{print $1}')
	if [ "$eval_sha" != "$ZCP_FARM_EVALUATOR_SHA" ] || [ "$cand_sha" != "$ZCP_FARM_CANDIDATE_SHA" ]; then
		printf '%s' "refused: digest mismatch" >"$RUNDIR/execution-override"
		exit 1
	fi

	private_home=$(mktemp -d)
	chmod 700 "$private_home"
	HOME=$private_home
	export HOME

	results_dir="$RUNDIR/results"
	work_dir="$RUNDIR/work"
	mkdir -p "$results_dir" "$RUNDIR/capture" "$work_dir"

	cd "$RUNDIR" || exit 1

	# child.pid names whichever process is actually doing the work: written
	# here (not by the supervisor's $!) because the supervisor may have
	# handed us to setsid/perl-setsid, which can fork an intermediate
	# process before landing on this one (D6). $$ is always this process's
	# own pid, and since supervisor_main puts it in a new session, that pid
	# is also its process group id — the id finish_and_upload signals as a
	# whole to reach every descendant, reparented or not (docs/spec-eval-farm.md
	# §2.3 FM-13).
	echo $$ >"$RUNDIR/child.pid"

	# Everything from here on becomes the evaluator's own output (dimension
	# lines included, spec-testing-architecture.md §10.1); exec replaces this
	# process so $RUNDIR/child.pid keeps naming whichever process is live.
	# --work-dir pins the evaluator's private bin dir under $RUNDIR/work
	# instead of its filepath.Dir(workDir)/candidate-bin default
	# (/var/candidate-bin when workDir defaults to /var/www), which uid
	# zerops cannot create (cmd/zcp/eval_behavioral.go ~line 202).
	exec >"$RUNDIR/child.log" 2>&1
	exec "$evaluator_bin" eval behavioral run \
		--candidate "$candidate_bin" \
		--candidate-sha256 "$ZCP_FARM_CANDIDATE_SHA" \
		--project-id "$projectId" \
		--ack-disposable-project yes \
		--capture raw \
		--capture-dir "$RUNDIR/capture" \
		--id "$ZCP_FARM_SCENARIO" \
		--scenarios-dir "$scen_dir" \
		--results-dir "$results_dir" \
		--work-dir "$work_dir"
}

# ---- supervisor (FM-13 step 4, FM-3/FM-4/FM-5/FM-7) --------------------

# finish_and_upload is the trap body: it must run to completion best-effort
# even if an individual step fails, so nothing here uses `set -e` past the
# top of this function (docs/spec-eval-farm.md §1.2 FM-3: absence of
# done.json, for any reason short of a supervisor SIGKILL, means the bundle
# is incomplete — a partial upload must still try for done.json).
finish_and_upload() {
	set +e

	if [ -e "$RUNDIR/.uploaded" ]; then
		return 0
	fi
	: >"$RUNDIR/.uploaded"

	execution=""
	task=""
	task_end=""

	if [ -f "$RUNDIR/execution-override" ]; then
		execution=$(cat "$RUNDIR/execution-override")
		task="not-run"
		task_end="not-run"
	else
		rc="${child_rc:-}"
		if [ -n "$rc" ] && [ "$rc" -gt 128 ] 2>/dev/null; then
			# A signal always overrides whatever the evaluator printed
			# before it died: printed lines are not proof the run (or its
			# capture window close) actually finished.
			sig=$((rc - 128))
			execution="error: killed by signal $sig"
			task="unknown"
			task_end="unknown"
		elif [ -f "$RUNDIR/child.log" ]; then
			execution=$(grep '^Execution:' "$RUNDIR/child.log" | tail -n1 | sed -e 's/^Execution:[[:space:]]*//')
			task=$(grep '^Task:' "$RUNDIR/child.log" | tail -n1 | sed -e 's/^Task:[[:space:]]*//')
			task_end=$(grep '^Task-end evidence:' "$RUNDIR/child.log" | tail -n1 | sed -e 's/^Task-end evidence:[[:space:]]*//')
		fi
		if [ -z "$execution" ]; then
			execution="error: evaluator ended with no dimension output (exit ${rc:-unknown})"
			task="unknown"
			task_end="unknown"
		fi
	fi

	kill_child_group

	redact_known_secrets "$RESULTS_DIR"
	redact_known_secrets "$CAPTURE_DIR"
	update_capture_manifest
	redacted_json=$(redacted_json_array)

	upload_dir "$RESULTS_DIR" "results"
	upload_dir "$CAPTURE_DIR" "capture"

	results_digest=$(tree_digest "$RESULTS_DIR")
	capture_digest=$(tree_digest "$CAPTURE_DIR")

	# The wrapper refuses to start on any other credential shape
	# (supervisor_main's OAuth-only gate), so every bundle that reaches
	# here ran under the farm's oauth-token profile.
	credential_mode="oauth-token"

	done_json="$RUNDIR/done.json"
	printf '{"runId":"%s","scenarioId":"%s","runnerDimensions":{"execution":"%s","task":"%s","taskEnd":"%s"},"parts":{"results":{"treeDigest":"%s"},"capture":{"treeDigest":"%s"}},"evaluatorSha256":"%s","candidateSha256":"%s","credentialMode":"%s","redacted":%s}' \
		"$(json_escape "$ZCP_FARM_RUN")" \
		"$(json_escape "$ZCP_FARM_SCENARIO")" \
		"$(json_escape "$execution")" \
		"$(json_escape "$task")" \
		"$(json_escape "$task_end")" \
		"$results_digest" "$capture_digest" \
		"$(json_escape "$ZCP_FARM_EVALUATOR_SHA")" \
		"$(json_escape "$ZCP_FARM_CANDIDATE_SHA")" \
		"$credential_mode" \
		"$redacted_json" \
		>"$done_json"

	s3_put "$done_json" "runs/$ZCP_FARM_RUN/done.json"
}

# run_detached_child starts child_main in its own session (so its pgid never
# collides with the supervisor's own — a plain "cmd &" under `set -eu` gets
# the *same* pgid as the backgrounding shell, which would make
# kill_child_group's negative-pid signal hit the supervisor too). Prefers the
# real setsid(1) (present on the run container's Ubuntu image); falls back to
# a one-line perl setsid()+exec when it is not on PATH (this dev machine has
# no setsid) so the offline test rig exercises the same process-group
# behavior; degrades to a same-group background job only if neither is
# available (D6 protection then has no effect on that host).
run_detached_child() {
	if command -v setsid >/dev/null 2>&1; then
		setsid sh "$0" --child "$RUNDIR" >"$RUNDIR/supervisor-child.log" 2>&1 &
	elif command -v perl >/dev/null 2>&1; then
		perl -e 'use POSIX qw(setsid); setsid(); exec { $ARGV[0] } @ARGV' \
			sh "$0" --child "$RUNDIR" >"$RUNDIR/supervisor-child.log" 2>&1 &
	else
		sh "$0" --child "$RUNDIR" >"$RUNDIR/supervisor-child.log" 2>&1 &
	fi
}

supervisor_main() {
	# D7: the run project's init commands execute with NO $HOME in the
	# environment at all — under `set -u`, referencing $HOME below would
	# die with "HOME: parameter not set" before started.json is ever
	# written (silent: no bundle, no execution-override, nothing to grade).
	# Fall back to the current user's actual home directory (never a
	# hardcoded "/home/$(id -un)": that only holds on the run container's
	# own image, and would be wrong on a dev machine or CI runner) via
	# shell tilde expansion, which consults the same NSS lookup getpwnam
	# would.
	if [ -z "${HOME:-}" ]; then
		eval HOME="~$(id -un)"
		export HOME
	fi

	RUNDIR="${ZCP_FARM_RUNDIR:-}"
	if [ -z "$RUNDIR" ]; then
		# Fixed, discoverable root (not a bare `mktemp -d`, which produced
		# an undiscoverable /tmp/tmp.* that needed a filesystem scan to find
		# supervisor.pid during the first live tracer runs).
		umask 077
		RUNDIR="$HOME/.zcp-farm/$ZCP_FARM_RUN"
	fi
	mkdir -p "$RUNDIR"
	echo $$ >"$RUNDIR/supervisor.pid"

	RESULTS_DIR="$RUNDIR/results"
	CAPTURE_DIR="$RUNDIR/capture"
	mkdir -p "$RESULTS_DIR" "$CAPTURE_DIR"

	# Fires the upload regardless of how this process ends, short of a
	# SIGKILL of the supervisor itself (FM-13). Idempotency-guarded above so
	# a signal trap followed by the normal `exit 0` below never uploads
	# twice.
	trap finish_and_upload EXIT INT TERM HUP

	# OAuth-only (owner decision 2026-09-10, docs/spec-eval-farm.md §2.3
	# step 2 + §2.4): the wrapper writes the private Claude home from
	# CLAUDE_CODE_OAUTH_TOKEN only, never ANTHROPIC_API_KEY — an API key
	# would let Claude Code shadow the OAuth profile.
	if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
		printf '%s' "refused: ANTHROPIC_API_KEY is set; oauth-token only" >"$RUNDIR/execution-override"
		exit 1
	fi
	if [ -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]; then
		printf '%s' "refused: CLAUDE_CODE_OAUTH_TOKEN is empty" >"$RUNDIR/execution-override"
		exit 1
	fi

	run_detached_child
	childpid=$!

	set +e
	wait "$childpid"
	child_rc=$?
	set -e

	exit 0
}

main() {
	if [ "${1:-}" = "--child" ]; then
		child_main "$2"
		exit $?
	fi
	supervisor_main
}

main "$@"
