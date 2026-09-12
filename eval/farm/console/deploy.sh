#!/bin/sh
# eval/farm/console/deploy.sh — deploys or updates the farm console service
# (docs/spec-eval-farm.md §8.1 FM-49). Idempotent: imports the "console"
# service into ZCP_FARM_PROJECT_ID only when it does not yet exist,
# otherwise reuses the existing service and its already-recorded console
# token — never a fresh import over a live service, never a tokenless copy
# of the operator env file. An agent must NEVER run this against the real
# platform: the orchestrator runs it once the slice that adds it lands.
#
# Inputs, defaults, and the exact step sequence are the contract this
# script was written from: docs/spec-eval-farm.md §8.1/§2.4, brief
# plans/zcp-farm-observer-2026-09-11-briefs/S6-console-deploy.md.
#
# Portable POSIX sh + curl + python3 (JSON only) — no jq. set -eu; no
# set -x. Secrets (the account token, the OAuth token, the generated
# console token) never appear on stdout/stderr, in a child's argv, or
# survive on disk once this script exits:
#   - the account token used to authenticate every REST call travels via a
#     0600 curl -K config file (a "header = ..." line), never a `-H`
#     argv flag;
#   - the import request body (which carries both secrets) is written to a
#     0600 temp file and passed as `--data @file`, removed right after;
#   - zcli's account token travels as the documented ZEROPS_TOKEN env var,
#     never argv, and its private login-data file lives outside the
#     pushed stage directory, in its own temp dir removed at exit.

set -eu

# ---- inputs -----------------------------------------------------------

if [ -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]; then
	echo "CLAUDE_CODE_OAUTH_TOKEN is required" >&2
	exit 1
fi
if [ -z "${ZCP_FARM_ACCOUNT_TOKEN:-}" ]; then
	echo "ZCP_FARM_ACCOUNT_TOKEN is required" >&2
	exit 1
fi
if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
	echo "ANTHROPIC_API_KEY must not be set" >&2
	exit 1
fi

ZCP_FARM_PROJECT_ID="${ZCP_FARM_PROJECT_ID:-swY2yczpQlqVLlcz0fCyFA}"
ZCP_API_HOST="${ZCP_API_HOST:-api.app-prg1.zerops.io}"
ZCP_FARM_CONSOLE_ENV_FILE="${ZCP_FARM_CONSOLE_ENV_FILE:-$HOME/.zerops-dev/agent-creds/farm-console.env}"
ZCLI="${ZCLI:-zcli}"

if [ -z "${REPO_ROOT:-}" ]; then
	# shellcheck disable=SC1007
	# ^ "CDPATH= cd ..." is the idiom, not a mistyped assignment: it clears
	# CDPATH for this one `cd` so a CDPATH set in the caller's environment
	# can't make `cd` print the new directory to stdout ahead of `pwd`.
	script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
	# shellcheck disable=SC1007
	REPO_ROOT=$(CDPATH= cd -- "$script_dir/../../.." && pwd)
fi

# ---- cleanup ------------------------------------------------------------

# STAGE (the build output the push reads), CLIDIR (zcli's own private
# login-data dir) and SECRETDIR (every temp file that itself carries a
# secret value) are all removed on every exit path — success, a failed
# request (set -e exits before a manual `rm -f` runs), or an interrupt
# mid-curl. Nothing that could carry a secret survives this script.
STAGE=""
CLIDIR=""
SECRETDIR=""
cleanup() {
	rc=$?
	[ -n "$STAGE" ] && rm -rf "$STAGE"
	[ -n "$CLIDIR" ] && rm -rf "$CLIDIR"
	[ -n "$SECRETDIR" ] && rm -rf "$SECRETDIR"
	exit "$rc"
}
trap cleanup EXIT

# SECRETDIR (mode 0700) holds every temp file that carries a secret value
# (api_request's curl -K bearer-token config, the import request body) —
# created once, up front, and registered with the trap above before the
# first secret-holding file is ever written into it. An explicit template
# (rather than a bare `mktemp -d`) is deliberate: unlike GNU mktemp, BSD/
# macOS mktemp ignores $TMPDIR for a template-less call, so this is what
# makes both the fix and its test honor an overridden $TMPDIR.
SECRETDIR=$(mktemp -d "${TMPDIR:-/tmp}/zcp-console-deploy-secrets.XXXXXX")
chmod 700 "$SECRETDIR"

# ---- REST helpers ---------------------------------------------------------

# api_request issues method ($1) against path ($2) on $ZCP_API_HOST —
# datafile ($3, optional) is a 0600 file holding a JSON body, passed as
# --data @file. The bearer token travels via a 0600 curl -K config file
# (never a `-H`/`--oauth2-bearer` argv flag, which `ps` on the host could
# read).
api_request() {
	method="$1"
	path="$2"
	datafile="${3:-}"

	cfg=$(mktemp "$SECRETDIR/curl-cfg.XXXXXX")
	chmod 600 "$cfg"
	printf 'header = "Authorization: Bearer %s"\n' "$ZCP_FARM_ACCOUNT_TOKEN" >"$cfg"

	set +e
	if [ -n "$datafile" ]; then
		out=$(curl -sS -f -K "$cfg" -X "$method" -H "Content-Type: application/json" --data "@$datafile" "https://$ZCP_API_HOST$path")
	else
		out=$(curl -sS -f -K "$cfg" -X "$method" "https://$ZCP_API_HOST$path")
	fi
	rc=$?
	set -e
	rm -f "$cfg"

	if [ "$rc" -ne 0 ]; then
		echo "request failed: $method $path (exit $rc)" >&2
		exit 1
	fi
	printf '%s' "$out"
}

# ---- JSON helpers (python3, no jq) -----------------------------------------

# find_console_service prints "<id>\n<status>\n" when the service-stack
# list body ($1) carries a service named "console", nothing otherwise.
find_console_service() {
	printf '%s' "$1" | python3 -c '
import json, sys
d = json.load(sys.stdin)
for it in d.get("list") or []:
	if it.get("name") == "console":
		print(it.get("id", ""))
		print(it.get("status", ""))
		break
'
}

service_stack_status() {
	printf '%s' "$1" | python3 -c 'import json, sys; d = json.load(sys.stdin); print(d.get("status", ""))'
}

import_result_service_id() {
	printf '%s' "$1" | python3 -c 'import json, sys; d = json.load(sys.stdin); ss = d.get("serviceStacks") or []; print(ss[0].get("id", "") if ss else "")'
}

# extract_subdomain_url pulls the first "https://...zerops.app..." substring
# out of a service-stack detail body ($1), wherever it is nested — the
# platform's own field for it is not pinned to one key, so this reads the
# same string a browser/human would see rather than reconstructing it from
# project.subdomainHost + hostname + port.
extract_subdomain_url() {
	printf '%s' "$1" | python3 -c '
import re, sys
m = re.search(r"https://[^\"\\\\ ]*zerops\.app[^\"\\\\ ]*", sys.stdin.read())
print(m.group(0) if m else "")
'
}

# ---- env file (0600, mode preserved across updates) ------------------------

# write_env_var replaces (or appends) key ($2) = value ($3) in file ($1),
# preserving every other line already there. umask 077 covers both the
# parent dir (created 0700 if missing) and the temp file the write goes
# through before the atomic mv.
write_env_var() {
	file="$1"
	key="$2"
	value="$3"

	dir=$(dirname "$file")
	umask 077
	mkdir -p "$dir"
	tmp=$(mktemp "$dir/.farm-console-env.XXXXXX")
	chmod 600 "$tmp"
	if [ -f "$file" ]; then
		grep -v "^${key}=" "$file" >"$tmp" 2>/dev/null || : >"$tmp"
	else
		: >"$tmp"
	fi
	printf '%s=%s\n' "$key" "$value" >>"$tmp"
	mv "$tmp" "$file"
	chmod 600 "$file"
}

read_env_var() {
	file="$1"
	key="$2"
	[ -f "$file" ] || return 0
	grep "^${key}=" "$file" 2>/dev/null | tail -n1 | cut -d= -f2-
}

# ---- (1) build the linux zcp binary + stage the push directory ------------

STAGE=$(mktemp -d)
(cd "$REPO_ROOT" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$STAGE/zcp" ./cmd/zcp)
cp "$REPO_ROOT/eval/farm/console/zerops.yml" "$STAGE/zerops.yml"

# ---- (2) look up the "console" service -------------------------------------

list_body=$(api_request GET "/api/rest/public/project/$ZCP_FARM_PROJECT_ID/service-stack")
found=$(find_console_service "$list_body")

if [ -z "$found" ]; then
	# ---- (3a) missing: mint a token, record it, import ---------------

	console_token=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
	write_env_var "$ZCP_FARM_CONSOLE_ENV_FILE" ZCP_FARM_CONSOLE_TOKEN "$console_token"

	yamlfile=$(mktemp "$SECRETDIR/import-body.XXXXXX")
	chmod 600 "$yamlfile"
	# shellcheck disable=SC2016
	# ^ deliberate: the program below is single-quoted so the SHELL never
	# expands anything in it — both secrets travel in as env vars
	# (os.environ reads them), never interpolated into the program text
	# itself, so neither can break out of the string or land in argv.
	OAUTH_TOKEN="$CLAUDE_CODE_OAUTH_TOKEN" CONSOLE_TOKEN="$console_token" YAMLFILE="$yamlfile" python3 -c '
import json
import os

yaml = """services:
  - hostname: console
    type: nodejs@22
    startWithoutCode: true
    envSecrets:
      CLAUDE_CODE_OAUTH_TOKEN: "%s"
      ZCP_FARM_CONSOLE_TOKEN: "%s"
    envVariables:
      ZCP_AUTHORING: "1"
      ZCP_FARM_S3_URL: ${os_apiUrl}
      ZCP_FARM_S3_BUCKET: ${os_bucketName}
      ZCP_FARM_S3_KEY: ${os_accessKeyId}
      ZCP_FARM_S3_SECRET: ${os_secretAccessKey}
""" % (os.environ["OAUTH_TOKEN"], os.environ["CONSOLE_TOKEN"])

with open(os.environ["YAMLFILE"], "w") as f:
	json.dump({"yaml": yaml}, f)
'
	import_body=$(api_request POST "/api/rest/public/project/$ZCP_FARM_PROJECT_ID/service-stack/import" "$yamlfile")
	rm -f "$yamlfile"

	sid=$(import_result_service_id "$import_body")
	if [ -z "$sid" ]; then
		echo "import: no service id returned" >&2
		exit 1
	fi

	i=0
	while :; do
		detail=$(api_request GET "/api/rest/public/service-stack/$sid")
		status=$(service_stack_status "$detail")
		case "$status" in
		READY_TO_DEPLOY | ACTIVE) break ;;
		esac
		i=$((i + 1))
		if [ "$i" -ge 60 ]; then
			echo "console service did not become ready (last status: $status)" >&2
			exit 1
		fi
		sleep 1
	done
else
	# ---- (3b) present: keep the existing token, never overwrite it ----

	sid=$(printf '%s\n' "$found" | sed -n '1p')

	existing_token=$(read_env_var "$ZCP_FARM_CONSOLE_ENV_FILE" ZCP_FARM_CONSOLE_TOKEN)
	if [ -z "$existing_token" ]; then
		echo "$ZCP_FARM_CONSOLE_ENV_FILE has no ZCP_FARM_CONSOLE_TOKEN line" >&2
		exit 1
	fi
fi

# ---- (4) push the built binary --------------------------------------------

CLIDIR=$(mktemp -d)
ZEROPS_TOKEN="$ZCP_FARM_ACCOUNT_TOKEN" ZEROPS_CLI_DATA_FILE_PATH="$CLIDIR/cli.data" "$ZCLI" push \
	--projectId "$ZCP_FARM_PROJECT_ID" \
	--serviceId "$sid" \
	--workingDir "$STAGE" \
	--noGit
rm -rf "$CLIDIR"
CLIDIR=""

# ---- (5) enable the subdomain (the import flag alone does not route a
# service imported without code) --------------------------------------------

api_request PUT "/api/rest/public/service-stack/$sid/enable-subdomain-access" >/dev/null

# ---- (5b) pin the console's scaling on every deploy, so an existing
# service converges too: exactly one container (the observation queue,
# worker and caches live in one process, §8.5) and a 2 GB RAM floor with
# 1 GB kept free, so three concurrent observer processes never outrun
# vertical autoscaling (at the platform's 0.125 GB default floor an
# observer's claude was OOM-killed) --------------------------------------

scalefile=$(mktemp "$SECRETDIR/autoscaling-body.XXXXXX")
printf '%s' '{"customAutoscaling":{"verticalAutoscaling":{"minResource":{"memoryGBytes":2},"minFreeResource":{"memoryGBytes":1}},"horizontalAutoscaling":{"minContainerCount":1,"maxContainerCount":1}}}' >"$scalefile"
api_request PUT "/api/rest/public/service-stack/$sid/autoscaling" "$scalefile" >/dev/null
rm -f "$scalefile"

# ---- (6) record + print the URL --------------------------------------------

detail_body=$(api_request GET "/api/rest/public/service-stack/$sid")
url=$(extract_subdomain_url "$detail_body")
write_env_var "$ZCP_FARM_CONSOLE_ENV_FILE" ZCP_FARM_CONSOLE_URL "$url"
printf 'console: %s\n' "$url"

# ---- (7) remove the stage dir (also covered by the EXIT trap) -------------

rm -rf "$STAGE"
STAGE=""
