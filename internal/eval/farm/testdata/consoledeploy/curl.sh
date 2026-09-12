#!/bin/sh
# Offline double for `curl`, exercised only via deploy.sh's api_request
# helper (internal/eval/farm/consoledeploy_test.go). It never touches a
# network: it parses just enough of its own argv to identify which REST
# call deploy.sh is making, records it, and answers from a canned fixture
# body the test dropped into $STUB_DIR beforehand.
#
# Routing is by URL suffix, most specific first (a single GET
# ".../service-stack/<sid>" must not shadow the more specific
# ".../import" / ".../enable-subdomain-access" / ".../autoscaling"
# suffixes).
set -eu

method="GET"
datafile=""
url=""
prev=""
for arg in "$@"; do
	case "$prev" in
	-X) method="$arg" ;;
	--data) datafile="${arg#@}" ;;
	esac
	# The URL is always deploy.sh's final argv token for every api_request
	# call; overwriting on every iteration leaves the last one standing.
	url="$arg"
	prev="$arg"
done

printf 'CURL %s %s\n' "$method" "$url" >>"$STUB_DIR/calls.log"

case "$url" in
*/service-stack/import)
	if [ -n "${CONSOLE_DEPLOY_TEST_FAIL_IMPORT:-}" ]; then
		# Answers as real curl -f does for an HTTP 500: no body, exit 22.
		exit 22
	fi
	if [ -n "$datafile" ]; then
		cp "$datafile" "$STUB_DIR/import-body-capture.json"
	fi
	cat "$STUB_DIR/import-response.json"
	;;
*/enable-subdomain-access)
	printf '{}'
	;;
*/autoscaling)
	if [ -n "$datafile" ]; then
		cp "$datafile" "$STUB_DIR/autoscaling-body-capture.json"
	fi
	printf '{"process":{"status":"PENDING","actionName":"stack.updateAutoscaling"}}'
	;;
*/service-stack)
	cat "$STUB_DIR/list-response.json"
	;;
*/service-stack/*)
	cat "$STUB_DIR/single-response.json"
	;;
*)
	echo "stub curl: unrecognized url $url" >&2
	exit 1
	;;
esac
