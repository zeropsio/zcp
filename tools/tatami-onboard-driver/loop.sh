#!/usr/bin/env bash
# Repeat the fresh-registration path N times, one throwaway account each, and
# print a one-line verdict per run. The auth-step failure is intermittent, so a
# single green pass proves nothing — this is how you get a rate.
#
#   ./loop.sh 5
set -u
N="${1:-5}"
EV_ROOT="${EVIDENCE_ROOT:-/Users/macbook/Documents/Zerops-MCP/zcp/plans/tatami-onboarding-auth-2026-08-03/evidence}"
cd "$(dirname "$0")"

for i in $(seq 1 "$N"); do
  DIR="$EV_ROOT/loop-$(date +%H%M%S)-$i"
  mkdir -p "$DIR"
  echo "--- run $i/$N -> $DIR"
  RUN=fresh WATCH_MS="${WATCH_MS:-45000}" EVIDENCE_DIR="$DIR" node driver.mjs > "$DIR/stdout.log" 2>&1
  node -e '
    const fs=require("fs"),p=process.argv[1];
    const f=fs.readdirSync(p).find(x=>x.startsWith("fresh-")&&x.endsWith(".json"));
    if(!f){console.log("  NO CAPTURE (see stdout.log)");process.exit(0)}
    const j=JSON.parse(fs.readFileSync(p+"/"+f,"utf8"));
    const ws=j.websockets.filter(w=>w.url.includes("shell/stream"));
    const fba=j.http.filter(h=>h.url.includes("file-browsing-access")&&h.method==="POST");
    const closes=j.pageWebSocketEvents.filter(e=>e.ev==="close"&&String(e.url).includes("shell/stream"));
    const res=j.notes.filter(n=>n.msg.startsWith("RESULT"))[0];
    const boot=(j.console.find(c=>/bootstrap=/.test(c.text))||{}).text||"(no embed-ready)";
    const cid=ws[0]?new URL(ws[0].url).searchParams.get("containerId"):null;
    console.log("  "+(res?res.msg:"(no result note)"));
    console.log("  fba:"+fba.map(h=>h.status).join(",")+"  wsAttempts:"+ws.length+"  handshake:["+ws.map(w=>w.handshakeStatus??"none").join(",")+"]  closes:["+closes.map(c=>c.code+"/"+(c.reason||"")).join(",")+"]");
    console.log("  containerId:"+cid+"  stack:"+((fba[0]||{}).url||"").replace(/.*service-stack\/([^/]+).*/,"$1")+"  "+boot.slice(0,60));
  ' "$DIR"
done
