#!/usr/bin/env bash
# Distill a Claude Code stream-json transcript into a readable turn-by-turn view.
# Usage: distill.sh <transcript.jsonl>
set -euo pipefail
T="$1"
jq -rc '
  def trunc(n): if (. != null and (.|length) > n) then (.[0:n] + "\n…[TRUNCATED " + ((length-n)|tostring) + " chars]") else (.//"") end;
  if .type=="assistant" then
    (.message.content[]? |
      if .type=="thinking" then "\n🧠 THINKING:\n" + ((.thinking//"") | trunc(1800))
      elif .type=="text" then "\n💬 ASSISTANT:\n" + (.text//"")
      elif .type=="tool_use" then "\n🔧 TOOL_CALL  " + (.name//"?") + "  input=" + ((.input|tojson) | trunc(1400))
      else empty end)
  elif .type=="user" then
    (.message.content[]? |
      if .type=="tool_result" then
        "\n📥 TOOL_RESULT" + (if .is_error==true then " [IS_ERROR]" else "" end) + ":\n" + (
          (.content) |
          (if type=="array" then (map(.text//(tojson))|join("\n")) elif type=="string" then . else tojson end)
          | trunc(3800)
        )
      else empty end)
  else empty end
' "$T"
