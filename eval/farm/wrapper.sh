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
# Env contract: docs/spec-eval-farm.md §2.2 (FM-12), including
# ZCP_FARM_SCENARIOS_DIGEST — the scenario tree digest under which
# `scenarios/<digest>/**` is fetched. ZCP_FARM_RUNDIR is a wrapper-local
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
# value or a missing dir. Uses perl's fixed-string \Q...\E quoting rather
# than a sed regex substitution — a sed BRE only escapes backslash, "/",
# and "&", so a token value containing any OTHER regex metacharacter
# ("[", "*", ".", "^", "$", …) was interpreted as a pattern: at best a
# wrong match, at worst a sed parse error that, under `set +e` in
# finish_and_upload's trap, left the file uploaded UNREDACTED with the
# error silently swallowed (R8, LAND review). The value travels via an env
# var (REDACT_VALUE), never as text inside perl's own program, so it is
# parsed exactly once, as data. Every file it actually rewrote (checked
# with a plain-text grep first, so a file the value never appeared in is
# never touched or logged) is appended to $RUNDIR/redacted.log — D8 needs
# that list to patch capture/manifest.json's per-file size/sha256
# afterward and to name what changed in done.json.
redact_dir() {
	dir="$1"
	value="$2"
	[ -z "$value" ] && return 0
	[ -d "$dir" ] || return 0

	# Materialize the traversal before touching a file. A pipeline would expose
	# only its final command's status in POSIX sh, allowing a failed find/read to
	# masquerade as a complete redaction pass.
	redact_list="$RUNDIR/.redact-list"
	if ! find "$dir" -type f >"$redact_list"; then
		rm -f "$redact_list"
		return 1
	fi

	redact_failed=0
	while IFS= read -r f; do
		[ -n "$f" ] || continue
		grep -qF -- "$value" "$f" 2>/dev/null
		grep_status=$?
		case "$grep_status" in
		0)
			if ! REDACT_VALUE="$value" perl -pi -e 's/\Q$ENV{REDACT_VALUE}\E/<redacted>/g' "$f"; then
				redact_failed=1
				continue
			fi
			# A successful rewrite command is not proof that the credential was
			# removed. Re-read the file and distinguish a remaining match (0)
			# from verified absence (1) and a read failure (>1).
			grep -qF -- "$value" "$f" 2>/dev/null
			verify_status=$?
			if [ "$verify_status" -ne 1 ]; then
				redact_failed=1
				continue
			fi
			if ! printf '%s\n' "$f" >>"$RUNDIR/redacted.log"; then
				redact_failed=1
			fi
			;;
		1) ;;
		*) redact_failed=1 ;;
		esac
	done <"$redact_list"
	if ! rm -f "$redact_list"; then
		redact_failed=1
	fi
	return "$redact_failed"
}

redact_known_secrets() {
	dir="$1"
	known_secrets_failed=0
	if ! redact_dir "$dir" "${ZCP_FARM_S3_SECRET:-}"; then
		known_secrets_failed=1
	fi
	if ! redact_dir "$dir" "${ZCP_FARM_S3_KEY:-}"; then
		known_secrets_failed=1
	fi
	if ! redact_dir "$dir" "${CLAUDE_CODE_OAUTH_TOKEN:-}"; then
		known_secrets_failed=1
	fi
	if ! redact_dir "$dir" "${ZCP_E2E_LAUNCH_KEY:-}"; then
		known_secrets_failed=1
	fi
	if ! redact_dir "$dir" "${ZCP_API_KEY:-}"; then
		known_secrets_failed=1
	fi
	return "$known_secrets_failed"
}

# ---- child-tree cleanup (D6) ----------------------------------------------

# Linux's PR_SET_CHILD_SUBREAPER makes every orphaned descendant reparent to
# this supervisor instead of PID 1. That kernel-owned membership survives a
# descendant calling setsid(2), unlike both a process group and a session.
# perl is already a hard wrapper dependency for fixed-string redaction; the
# raw syscall numbers are stable Linux ABI values for the two architectures
# used by the run image. Unsupported Linux architectures fail closed before
# an evaluator starts.
linux_prctl_syscall() {
	case "$(uname -m)" in
	x86_64 | amd64) printf '%s' 157 ;;
	aarch64 | arm64) printf '%s' 167 ;;
	*) return 1 ;;
	esac
}

is_child_subreaper() {
	# PR_SET_CHILD_SUBREAPER is preserved across exec but deliberately not
	# inherited across fork. The helper stamps its own PID only after the
	# syscall succeeds; exec preserves that PID, while every child sees a
	# different $$ and therefore cannot masquerade as this supervisor.
	[ "${ZCP_FARM_INTERNAL_SUBREAPER_PID:-}" = "$$" ]
}

exec_as_child_subreaper() {
	prctl_nr=$(linux_prctl_syscall) || return 1
	command -v perl >/dev/null 2>&1 || return 1
	exec perl -e '
		my $nr = shift;
		syscall($nr, 36, 1, 0, 0, 0) == 0 or exit 125;
		$ENV{ZCP_FARM_INTERNAL_SUBREAPER_PID} = $$;
		exec @ARGV;
		exit 126;
	' "$prctl_nr" sh "$0" "$@"
}

# read_proc_stat_fields reads one complete /proc/<pid>/stat snapshot and sets
# proc_state (field 3), proc_ppid (4), proc_sid (6), and proc_num_threads (20).
# comm (field 2) may contain spaces, parentheses, and newlines, so a line-based
# read is invalid; the kernel fields begin only after the LAST ") ". Return 2
# when the file cannot be read (normally a process-exit race), and 1 when bytes
# were read but do not contain a complete, usable snapshot.
read_proc_stat_fields() {
	proc_stat_path="$1"
	proc_stat=$(cat "$proc_stat_path" 2>/dev/null) || return 2
	case "$proc_stat" in
	*") "*) ;;
	*) return 1 ;;
	esac
	proc_stat_rest=${proc_stat##*) }
	# The only newline in a valid snapshot can be inside comm and was removed
	# with the prefix above. A remaining newline means the snapshot is invalid.
	case "$proc_stat_rest" in
	*'
'*) return 1 ;;
	esac
	proc_stat_fields=$(printf '%s\n' "$proc_stat_rest" | awk '
		NF < 18 ||
		$1 !~ /^[[:alpha:]]$/ ||
		$2 !~ /^[0-9]+$/ ||
		$3 !~ /^[0-9]+$/ ||
		$4 !~ /^[0-9]+$/ ||
		$18 !~ /^[0-9]+$/ ||
		$18 == 0 { exit 1 }
		{ print $1, $2, $4, $18 }
	') || return 1
	[ -n "$proc_stat_fields" ] || return 1

	proc_state=${proc_stat_fields%% *}
	proc_stat_fields=${proc_stat_fields#* }
	proc_ppid=${proc_stat_fields%% *}
	proc_stat_fields=${proc_stat_fields#* }
	proc_sid=${proc_stat_fields%% *}
	proc_num_threads=${proc_stat_fields#* }
}

# A fully parsed, single-thread zombie has no executable threads left and is
# waiting only to be reaped. A zombie reporting multiple threads is treated as
# active/uncertain and remains in cleanup until the kernel snapshot changes.
proc_stat_is_safely_inactive() {
	[ "$proc_state" = "Z" ] && [ "$proc_num_threads" = "1" ]
}

# collect_adopted_children sets cleanup_pids to the supervisor's currently
# live direct children. After the original evaluator has been waited for, a
# subreaper owns every surviving descendant root, including a setsid escape.
# A helper used to read the kernel file can appear in that same snapshot;
# the PPID/state recheck below drops it after the command has been reaped.
collect_adopted_children() {
	children_file="/proc/$$/task/$$/children"
	[ -r "$children_file" ] || return 1
	children=$(cat "$children_file") || return 1
	cleanup_pids=""
	for pid in $children; do
		stat_file="/proc/$pid/stat"
		if read_proc_stat_fields "$stat_file"; then
			:
		else
			stat_rc=$?
			# A child can exit between the children snapshot and opening stat.
			# Any other read error, or readable but malformed bytes, leaves
			# liveness uncertain and must block publication.
			[ "$stat_rc" -eq 2 ] && [ ! -e "$stat_file" ] && continue
			return 1
		fi
		[ "$proc_ppid" = "$$" ] || continue
		proc_stat_is_safely_inactive && continue
		cleanup_pids="$cleanup_pids $pid"
	done
}

# session_pids is the non-Linux offline-rig fallback. It prints every pid
# whose session id equals sid ($1), covering reparenting and process-group
# changes but not setsid(2); production Linux uses subreaper membership above.
# Tries /proc, a ps implementation with the sid keyword, then python3's
# os.getsid() for this repo's macOS test machine. Prints nothing when no tier
# is available.
session_pids() {
	sid="$1"
	if [ -d /proc ] && [ -r "/proc/$sid/stat" ]; then
		for statfile in /proc/[0-9]*/stat; do
			[ -r "$statfile" ] || continue
			pid=$(basename "$(dirname "$statfile")")
			if read_proc_stat_fields "$statfile"; then
				:
			else
				stat_rc=$?
				[ "$stat_rc" -eq 2 ] && [ ! -e "$statfile" ] && continue
				return 1
			fi
			[ "$proc_sid" = "$sid" ] && printf '%s\n' "$pid"
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

# kill_child_group terminates the evaluator's complete descendant tree before
# redaction/upload. On Linux the supervisor is a verified child subreaper, so
# every surviving root is an exact direct child even after setsid(2). KILL is
# iterated because killing one adopted root can expose another generation.
# Other hosts retain the session cleanup for the offline rig; farm production
# is Linux and refuses to start unless subreaper setup succeeds.
kill_child_group() {
	if [ "$(uname -s)" = "Linux" ]; then
		is_child_subreaper || return 1
		collect_adopted_children || return 1
		for pid in $cleanup_pids; do
			kill -TERM "$pid" 2>/dev/null || true
		done
		sleep 0.3

		cleanup_round=0
		while [ "$cleanup_round" -lt 100 ]; do
			collect_adopted_children || return 1
			[ -z "$cleanup_pids" ] && return 0
			for pid in $cleanup_pids; do
				kill -KILL "$pid" 2>/dev/null || true
			done
			cleanup_round=$((cleanup_round + 1))
			sleep 0.02
		done
		collect_adopted_children || return 1
		[ -z "$cleanup_pids" ]
		return $?
	fi

	[ -f "$RUNDIR/child.pid" ] || return 0
	sid=$(cat "$RUNDIR/child.pid")
	case "$sid" in
	'' | *[!0-9]*) return 1 ;;
	esac

	pids=$(session_pids "$sid") || return 1
	[ -z "$pids" ] && return 0
	for pid in $pids; do
		kill -TERM "$pid" 2>/dev/null || true
	done
	sleep 0.3
	pids=$(session_pids "$sid") || return 1
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
	sorted_redactions="$RUNDIR/.redacted-sorted"
	if ! : >"$updates" || ! LC_ALL=C sort -u "$RUNDIR/redacted.log" >"$sorted_redactions"; then
		rm -f "$updates" "$sorted_redactions"
		return 1
	fi
	manifest_scan_failed=0
	while IFS= read -r f; do
		case "$f" in
		"$manifest_dir"/*) ;;
		*) continue ;;
		esac
		rel=${f#"$manifest_dir"/}
		if ! size=$(wc -c <"$f"); then
			manifest_scan_failed=1
			continue
		fi
		if ! sha_line=$(sha256sum "$f"); then
			manifest_scan_failed=1
			continue
		fi
		sha=${sha_line%% *}
		if ! printf '%s\t%s\t%s\n' "$rel" "$size" "$sha" >>"$updates"; then
			manifest_scan_failed=1
		fi
	done <"$sorted_redactions"
	if ! rm -f "$sorted_redactions"; then
		manifest_scan_failed=1
	fi
	if [ "$manifest_scan_failed" -ne 0 ]; then
		rm -f "$updates"
		return 1
	fi

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

		my $tmp = "$manifest_path.zcp-redact-$$";
		open my $oh, ">", $tmp or die "write manifest temp: $!";
		print $oh JSON::PP->new->canonical->encode($doc)
			or die "write manifest temp: $!";
		close $oh or die "close manifest temp: $!";
		rename $tmp, $manifest_path or die "replace manifest: $!";
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

	manifest_rewrite_failed=0
	if [ -f "$CAPTURE_DIR/manifest.json" ]; then
		if ! update_one_capture_manifest "$CAPTURE_DIR/manifest.json"; then
			manifest_rewrite_failed=1
		fi
	fi
	for manifest in "$CAPTURE_DIR"/*/manifest.json; do
		[ -f "$manifest" ] || continue
		if ! update_one_capture_manifest "$manifest"; then
			manifest_rewrite_failed=1
		fi
	done
	return "$manifest_rewrite_failed"
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
	redacted_json_list="$RUNDIR/.redacted-json-list"
	if ! LC_ALL=C sort -u "$RUNDIR/redacted.log" >"$redacted_json_list"; then
		rm -f "$redacted_json_list"
		return 1
	fi
	redacted_json_failed=0
	if ! printf '['; then
		redacted_json_failed=1
	fi
	first=1
	while IFS= read -r f; do
		rel=${f#"$RUNDIR/"}
		if ! escaped_rel=$(json_escape "$rel"); then
			redacted_json_failed=1
			continue
		fi
		if [ "$first" -eq 1 ]; then
			if ! printf '"%s"' "$escaped_rel"; then
				redacted_json_failed=1
			fi
			first=0
		else
			if ! printf ',"%s"' "$escaped_rel"; then
				redacted_json_failed=1
			fi
		fi
	done <"$redacted_json_list"
	if ! rm -f "$redacted_json_list"; then
		redacted_json_failed=1
	fi
	if ! printf ']'; then
		redacted_json_failed=1
	fi
	return "$redacted_json_failed"
}

# ---- upload ---------------------------------------------------------------

# upload_dir PUTs every regular file under dir ($1) to
# runs/$ZCP_FARM_RUN/<part ($2)>/<relpath>.
upload_dir() {
	dir="$1"
	part="$2"
	[ -d "$dir" ] || return 0
	list_file="$RUNDIR/.upload-list-$part"
	(
		cd "$dir" || exit 1
		find . -type f | sed 's#^\./##'
	) >"$list_file" || { rm -f "$list_file"; return 1; }
	upload_failed=0
	while IFS= read -r rel; do
		[ -n "$rel" ] || continue
		if ! s3_put "$dir/$rel" "runs/$ZCP_FARM_RUN/$part/$rel"; then
			upload_failed=1
		fi
	done <"$list_file"
	rm -f "$list_file"
	return "$upload_failed"
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
	# work_dir is the agent's own cwd, not evaluator scratch (D21,
	# docs/spec-eval-farm.md §2.3): it defaults to /var/www, matching a real
	# container's mount root (internal/ops/mount.go mountBase) and the
	# evaluator's own default outside a binding (internal/eval/runner.go), so
	# the agent's `claude` and the MCP `zcp serve` child it spawns share the
	# same root zerops_mount uses and the deploy preflight expects.
	# ZCP_FARM_WORK_DIR overrides it for the offline test rig only (mirrors
	# ZCP_FARM_RUNDIR below) — never set in a real farm run.
	work_dir="${ZCP_FARM_WORK_DIR:-/var/www}"
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
	# --work-dir is /var/www (see work_dir above); the evaluator's own
	# private bin/Claude-home dirs are siblings of --results-dir instead of
	# --work-dir's parent (cmd/zcp/eval_behavioral.go buildExecutionBinding),
	# so /var/www's parent is never touched.
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

# finish_and_upload is the trap body: cleanup and local evidence construction
# continue best-effort even if an individual step fails, so nothing here uses
# `set -e` past the top of this function. Publishing is fail-closed: a failed
# sanitization or part upload leaves done.json absent from the sink, marking
# the remote bundle incomplete under docs/spec-eval-farm.md §1.2 FM-3.
finish_and_upload() {
	set +e

	# The marker is written only after done.json has been accepted by the
	# sink.  A shell trap can be entered more than once for signals, so keep
	# the reentrancy guard in process memory; a marker created before upload
	# would falsely claim a failed bundle was complete.
	if [ "${finish_running:-0}" -eq 1 ] || [ -e "$RUNDIR/.uploaded" ]; then
		return 0
	fi
	finish_running=1

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

	cleanup_ok=1
	if ! kill_child_group; then
		cleanup_ok=0
		execution="error: descendant cleanup could not be proven"
		task="unknown"
		task_end="unknown"
	fi

	sanitization_ok="$cleanup_ok"
	redacted_json='[]'
	if [ "$cleanup_ok" -eq 1 ]; then
		if ! redact_known_secrets "$RESULTS_DIR"; then
			sanitization_ok=0
		fi
		if ! redact_known_secrets "$CAPTURE_DIR"; then
			sanitization_ok=0
		fi
		if ! update_capture_manifest; then
			sanitization_ok=0
		fi
		if ! redacted_json=$(redacted_json_array); then
			sanitization_ok=0
			redacted_json='[]'
		fi
	fi

	parts_ok="$sanitization_ok"
	if [ "$sanitization_ok" -eq 1 ]; then
		if ! upload_dir "$RESULTS_DIR" "results"; then
			parts_ok=0
		fi
		if ! upload_dir "$CAPTURE_DIR" "capture"; then
			parts_ok=0
		fi
	fi

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

	if [ "$parts_ok" -eq 1 ] && s3_put "$done_json" "runs/$ZCP_FARM_RUN/done.json"; then
		: >"$RUNDIR/.uploaded"
	fi
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

	# Claim the run directory before creating any supervisor/evidence files or
	# installing the upload trap. mkdir is the atomic cross-process claim. The
	# harness supplies an empty temporary directory, while production supplies
	# a path below the fixed farm root; only the parent may be created here.
	# Any existing entry means this run was completed, interrupted, or was
	# created by an older wrapper. In all cases preserve it and refuse to
	# launch another paid evaluator attempt.
	parent_dir=$(dirname "$RUNDIR")
	mkdir -p "$parent_dir"
	if [ ! -e "$RUNDIR" ]; then
		# In production the run directory itself is the atomic claim. The
		# marker below is still kept as durable evidence of that claim.
		if ! mkdir "$RUNDIR" 2>/dev/null; then
			printf '%s\n' "refused: run directory is already claimed by another attempt" >&2
			return 1
		fi
	fi
	if [ -e "$RUNDIR/.attempt" ] || [ -e "$RUNDIR/done.json" ] || [ -e "$RUNDIR/supervisor.pid" ] || [ -e "$RUNDIR/started.json" ] || [ -e "$RUNDIR/execution-override" ]; then
		printf '%s\n' "refused: run directory already has a completed or interrupted attempt" >&2
		return 1
	fi
	if [ -n "$(find "$RUNDIR" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]; then
		printf '%s\n' "refused: run directory contains a legacy attempt" >&2
		return 1
	fi
	if ! mkdir "$RUNDIR/.attempt" 2>/dev/null; then
		printf '%s\n' "refused: run directory is already claimed by another attempt" >&2
		return 1
	fi
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
	if [ "$(uname -s)" = "Linux" ] && ! is_child_subreaper; then
		if ! exec_as_child_subreaper "$@"; then
			printf '%s\n' "refused: Linux child-subreaper setup unavailable" >&2
			return 1
		fi
	fi
	supervisor_main
}

main "$@"
