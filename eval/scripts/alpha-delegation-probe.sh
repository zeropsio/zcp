#!/usr/bin/env bash
# Alpha delegation probe — verifies the integration-token delegation feature
# (one-time token minting) on the Alpha environment, API-level via curl.
#
# Feature under test (live on Alpha 2026-07-03):
#   - New ZCP integration tokens automatically get ONE one-time delegation:
#       {"roleCode":"NO_ACCESS","canCreateProjects":true,
#        "canEditFinances":false,"canViewFinances":false,"projectPermissions":[]}
#   - The ZCP token can call POST /client/{clientId}/integration-token ONCE,
#     consuming the delegation; the minted token is owned by the user who
#     created the delegation (swagger: getIntegrationTokenDelegation).
#
# Usage:
#   ALPHA_ZCP_TOKEN=...      (required) fresh ZCP integration token from Alpha
#   ALPHA_PERSONAL_TOKEN=... (optional) Alpha personal token — enables the
#                            create/revoke-delegation tests + cleanup of minted tokens
#   PROBE_CREATE_PROJECT=1   (optional) also exercise project creation with the
#                            minted token (test 6); project is deleted afterwards
#
#   ./eval/scripts/alpha-delegation-probe.sh
#
# Output: numbered PASS/FAIL/INFO lines + a summary. Tokens are never printed
# (only first 6 chars). Raw responses land in $OUTDIR for inspection.

set -u
API="${ALPHA_API:-https://api.app-alpha.zerops.dev/api/rest/public}"
OUTDIR="${PROBE_OUTDIR:-$(mktemp -d /tmp/alpha-delegation-probe.XXXXXX)}"
mkdir -p "$OUTDIR"

: "${ALPHA_ZCP_TOKEN:?set ALPHA_ZCP_TOKEN to a fresh Alpha ZCP integration token}"
PERSONAL="${ALPHA_PERSONAL_TOKEN:-}"

PASS=0; FAIL=0; NOTE=0
red()   { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
mask()  { printf '%s…' "${1:0:6}"; }

# call <name> <token> <method> <path> [json-body] -> writes $OUTDIR/<name>.json, echoes http code
call() {
  local name="$1" token="$2" method="$3" path="$4" body="${5:-}"
  local args=(-s -o "$OUTDIR/$name.json" -w '%{http_code}' -X "$method" \
    -H "Authorization: Bearer $token" -H "Content-Type: application/json" \
    --max-time 30 "$API$path")
  [ -n "$body" ] && args+=(-d "$body")
  curl "${args[@]}"
}

jqr() { python3 -c "import json,sys;d=json.load(open('$OUTDIR/$1.json'));print(eval(\"d$2\"))" 2>/dev/null; }

check() { # check <label> <got> <want>
  if [ "$2" = "$3" ]; then green "PASS  $1 (got $2)"; PASS=$((PASS+1));
  else red "FAIL  $1 (got $2, want $3) — see $OUTDIR"; FAIL=$((FAIL+1)); fi
}
note() { printf 'INFO  %s\n' "$*"; NOTE=$((NOTE+1)); }

echo "== Alpha delegation probe — API=$API OUTDIR=$OUTDIR"
echo "== ZCP token: $(mask "$ALPHA_ZCP_TOKEN")  personal: ${PERSONAL:+$(mask "$PERSONAL")}${PERSONAL:-<none>}"
echo

# ------------------------------------------------------------------ 1. identity
echo "-- [1] self-identity via /user/info (ZCP token)"
code=$(call t1-userinfo "$ALPHA_ZCP_TOKEN" GET "/user/info")
check "user/info reachable" "$code" 200
USER_ID=$(jqr t1-userinfo "['id']")
CLIENT_ID=$(jqr t1-userinfo "['clientUserList'][0]['clientId']")
note "token user id=$USER_ID clientId=$CLIENT_ID"
[ -z "$CLIENT_ID" ] && { red "cannot continue without clientId"; exit 1; }

# ---------------------------------------------------- 2. find own tokenId + delegation
echo "-- [2] locate own integration token + its delegation"
code=$(call t2-toklist "$ALPHA_ZCP_TOKEN" GET "/client/$CLIENT_ID/integration-token/list")
note "integration-token/list with ZCP token -> $code (may be 403; roles doc says NO_ACCESS allowed)"
TOKEN_ID=""
if [ "$code" = "200" ]; then
  # try to match self: integration tokens are user-shaped; try id == USER_ID first
  TOKEN_ID=$(python3 - "$OUTDIR/t2-toklist.json" "$USER_ID" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); uid=sys.argv[2]
ids=[t['id'] for t in d.get('list',[])]
print(uid if uid in ids else (ids[0] if len(ids)==1 else ''))
PY
)
fi
if [ -z "$TOKEN_ID" ]; then
  note "falling back to tokenId = user/info id ($USER_ID)"
  TOKEN_ID="$USER_ID"
fi
code=$(call t2-deleg "$ALPHA_ZCP_TOKEN" GET "/client/$CLIENT_ID/integration-token/$TOKEN_ID/delegation")
check "list own delegations" "$code" 200
DELEG_COUNT=$(jqr t2-deleg "['list'].__len__()")
note "delegations on this token: ${DELEG_COUNT:-?} (expect 1 on a FRESH ZCP token)"
if [ "${DELEG_COUNT:-0}" -ge 1 ] 2>/dev/null; then
  DELEG_ID=$(jqr t2-deleg "['list'][0]['id']")
  note "delegation id=$DELEG_ID spec=$(jqr t2-deleg "['list'][0]['tokenPermissions']")"
else
  red "no delegation present — token is not fresh, or auto-delegation did not fire"
  DELEG_ID=""
fi

# ------------------------------------------------------------- 3. negative mints
echo "-- [3] negative: mint MORE than delegated (expect rejection)"
code=$(call t3-overreach "$ALPHA_ZCP_TOKEN" POST "/client/$CLIENT_ID/integration-token" \
  '{"name":"zcp-probe-overreach","projects":[],"roleCode":"ADMIN","canCreateProjects":true}')
if [ "$code" = "200" ]; then
  red "FAIL  over-delegation mint SUCCEEDED (roleCode=ADMIN) — security hole, report to BE"
  FAIL=$((FAIL+1))
else
  green "PASS  over-reach mint rejected (got $code: $(head -c 200 "$OUTDIR/t3-overreach.json"))"
  PASS=$((PASS+1))
fi

# ------------------------------------------------------------------- 4. the mint
echo "-- [4] mint the delegated token (consumes the one-time delegation!)"
code=$(call t4-mint "$ALPHA_ZCP_TOKEN" POST "/client/$CLIENT_ID/integration-token" \
  '{"name":"zcp-launch-probe","projects":[],"roleCode":"NO_ACCESS","canCreateProjects":true}')
check "mint delegated token" "$code" 200
MINTED=$(jqr t4-mint "['token']")
MINTED_ID=$(jqr t4-mint "['id']")
if [ -n "$MINTED" ] && [ "$MINTED" != "None" ]; then
  note "minted token id=$MINTED_ID token=$(mask "$MINTED") canCreateProjects=$(jqr t4-mint "['canCreateProjects']") role=$(jqr t4-mint "['roleCode']")"
else
  red "no raw token in mint response — see $OUTDIR/t4-mint.json"; FAIL=$((FAIL+1))
fi

# ------------------------------------------------------- 5. one-time enforcement
echo "-- [5] one-time: second mint must fail; delegation list after consumption"
code=$(call t5-remint "$ALPHA_ZCP_TOKEN" POST "/client/$CLIENT_ID/integration-token" \
  '{"name":"zcp-launch-probe-2","projects":[],"roleCode":"NO_ACCESS","canCreateProjects":true}')
if [ "$code" = "200" ]; then
  red "FAIL  second mint SUCCEEDED — delegation is not one-time!"; FAIL=$((FAIL+1))
else
  green "PASS  second mint rejected (got $code: $(head -c 200 "$OUTDIR/t5-remint.json"))"
  PASS=$((PASS+1))
  note "ZCP error-mapping input — exact body: $(head -c 300 "$OUTDIR/t5-remint.json")"
fi
code=$(call t5-deleg "$ALPHA_ZCP_TOKEN" GET "/client/$CLIENT_ID/integration-token/$TOKEN_ID/delegation")
note "delegation list post-mint -> $code, count=$(jqr t5-deleg "['list'].__len__()") (learn: consumed = deleted, or kept+flagged?)"

# ------------------------------------------------- 6. minted-token capabilities
if [ -n "${MINTED:-}" ] && [ "$MINTED" != "None" ]; then
  echo "-- [6] minted token capabilities"
  code=$(call t6-userinfo "$MINTED" GET "/user/info")
  check "minted token authenticates" "$code" 200
  note "minted token owner userId=$(jqr t6-userinfo "['id']") (swagger says: owned by the DELEGATING user)"
  code=$(call t6-deleg "$MINTED" GET "/client/$CLIENT_ID/integration-token/$MINTED_ID/delegation")
  note "minted token's own delegation list -> $code count=$(jqr t6-deleg "['list'].__len__()") (must be 0 — no delegation chaining)"
  if [ "${PROBE_CREATE_PROJECT:-0}" = "1" ]; then
    code=$(call t6-project "$MINTED" POST "/client/$CLIENT_ID/project" \
      '{"name":"zcp-deleg-probe","tagList":[],"userRoles":[]}')
    check "minted token creates project (canCreateProjects)" "$code" 200
    PROJECT_ID=$(jqr t6-project "['id']")
    if [ -n "${PROJECT_ID:-}" ] && [ "$PROJECT_ID" != "None" ]; then
      note "created project id=$PROJECT_ID — verifying Owner access + cleanup"
      code=$(call t6-projget "$MINTED" GET "/project/$PROJECT_ID")
      check "minted token reads own project (Owner)" "$code" 200
      code=$(call t6-projdel "$MINTED" DELETE "/project/$PROJECT_ID")
      note "cleanup project delete -> $code"
    fi
    # NEGATIVE: NO_ACCESS elsewhere — ZCP's own dev project must be invisible
    code=$(call t6-neg "$MINTED" GET "/client/$CLIENT_ID/integration-token/list")
    note "minted token listing client tokens -> $code (expect 403 for NO_ACCESS... learn the actual)"
  else
    note "skipping project-creation test (set PROBE_CREATE_PROJECT=1 to enable)"
  fi
fi

# --------------------------------- 7. delegation management needs a PERSONAL token
echo "-- [7] delegation create/revoke separation (integration token must NOT self-delegate)"
code=$(call t7-selfdeleg "$ALPHA_ZCP_TOKEN" POST "/client/$CLIENT_ID/integration-token/$TOKEN_ID/delegation" \
  '{"roleCode":"NO_ACCESS","canCreateProjects":true,"projectPermissions":[]}')
if [ "$code" = "200" ]; then
  red "FAIL  ZCP token created a delegation FOR ITSELF — privilege escalation, report to BE"; FAIL=$((FAIL+1))
else
  green "PASS  self-delegation rejected (got $code)"; PASS=$((PASS+1))
fi

if [ -n "$PERSONAL" ]; then
  echo "-- [8] personal-token path: create delegation -> mint -> revoke -> mint"
  code=$(call t8-create "$PERSONAL" POST "/client/$CLIENT_ID/integration-token/$TOKEN_ID/delegation" \
    '{"roleCode":"NO_ACCESS","canCreateProjects":true,"projectPermissions":[]}')
  check "personal token creates delegation" "$code" 200
  D2=$(jqr t8-create "['id']")
  if [ -n "${D2:-}" ] && [ "$D2" != "None" ]; then
    code=$(call t8-revoke "$PERSONAL" DELETE "/client/$CLIENT_ID/integration-token/$TOKEN_ID/delegation/$D2")
    check "personal token revokes delegation" "$code" 200
    code=$(call t8-mintafter "$ALPHA_ZCP_TOKEN" POST "/client/$CLIENT_ID/integration-token" \
      '{"name":"zcp-probe-revoked","projects":[],"roleCode":"NO_ACCESS","canCreateProjects":true}')
    if [ "$code" = "200" ]; then
      red "FAIL  mint succeeded against a REVOKED delegation"; FAIL=$((FAIL+1))
    else
      green "PASS  mint after revoke rejected (got $code)"; PASS=$((PASS+1))
    fi
  fi
  # cleanup minted probe tokens (integration tokens cannot delete tokens)
  if [ -n "${MINTED_ID:-}" ] && [ "$MINTED_ID" != "None" ]; then
    code=$(call t8-cleanup "$PERSONAL" DELETE "/client/$CLIENT_ID/integration-token/$MINTED_ID")
    note "cleanup minted probe token -> $code"
  fi
else
  note "no ALPHA_PERSONAL_TOKEN — skipped create/revoke tests + minted-token cleanup (delete 'zcp-launch-probe' token in Alpha GUI manually)"
fi

echo
echo "== SUMMARY: $PASS pass, $FAIL fail, $NOTE notes — raw responses in $OUTDIR"
exit $((FAIL > 0 ? 1 : 0))
