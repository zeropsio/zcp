#!/usr/bin/env python3
"""zcp-claude-kickoff — process wrapper for the Claude Code VS Code plugin.

Installed by `zcp init` and pointed at by the `claudeCode.claudeProcessWrapper`
setting, so the plugin launches the claude CLI *through* this wrapper. It exists
for exactly one job: let the welcome panel's "Onboard me" button deliver a
kickoff prompt that is SUBMITTED (not merely prefilled — the plugin's own
editor.open initialPrompt only fills the composer).

Mechanism. The plugin drives the CLI over a duplex stream-json protocol and is
launched with `--replay-user-messages`. This wrapper forwards both pipes and,
when a kickoff is armed:
  * writes one user-message frame into the CLI's stdin at the `initialize`
    control-response (the session is live and subscribed by then), and
  * mirrors that same turn — same uuid — onto stdout right after the CLI's
    `system/init`, so the bubble renders at the earliest in-panel moment. The
    CLI's own later replay carries the same uuid and is deduped by the webview,
    so there is no double bubble and a reload rebuilds the identical turn.

Transparency. With no kickoff armed (the overwhelmingly common case — every
normal spawn, `auth status`, a plain terminal `claude`) the wrapper execs the
real binary immediately: zero proxy, zero risk. Only a session spawn
(`--input-format stream-json`) with an armed marker is ever proxied.

Arm a kickoff by writing {"prompt": "..."} to ~/.zcp/state/claude-kickoff.json
(the welcome panel's armKickoffMarker does this). The marker is consumed once,
before the child is spawned, so it fires exactly once.

Set ZCP_KICKOFF_DEBUG=1 to trace to ~/.zcp/state/claude-kickoff.log.
"""

import datetime
import json
import os
import shutil
import subprocess
import sys
import threading
import time
import uuid

MARKER = os.path.expanduser("~/.zcp/state/claude-kickoff.json")
DEBUG_LOG = os.path.expanduser("~/.zcp/state/claude-kickoff.log")
MARKER_TTL_S = 180


def log(msg):
    if not os.environ.get("ZCP_KICKOFF_DEBUG"):
        return
    try:
        os.makedirs(os.path.dirname(DEBUG_LOG), exist_ok=True)
        with open(DEBUG_LOG, "a") as fh:
            fh.write("%.3f %s\n" % (time.time(), msg))
    except Exception:
        pass


def real_claude():
    """The genuine CLI. Only reached for a bare `wrapper` invocation with no
    binary argument; the plugin always passes its own binary as argv[0]. The
    wrapper is installed at a dedicated path (never named `claude` on PATH), so
    which('claude') resolves the real binary, never this script."""
    return shutil.which("claude") or "claude"


def is_session_spawn(argv):
    """Only the duplex session process may be injected into — the plugin also
    runs short-lived helpers (`auth status --json`) through this same wrapper."""
    return "--input-format" in argv and "stream-json" in argv


def take_kickoff():
    """Read and immediately consume the marker — one shot, even on error."""
    try:
        if not os.path.exists(MARKER):
            return None
        stale = (time.time() - os.path.getmtime(MARKER)) > MARKER_TTL_S
        with open(MARKER) as fh:
            data = json.load(fh)
        os.remove(MARKER)
        if stale:
            log("marker ignored: older than %ds" % MARKER_TTL_S)
            return None
        prompt = (data or {}).get("prompt")
        return prompt if isinstance(prompt, str) and prompt.strip() else None
    except Exception as err:
        log("marker unreadable: %r" % (err,))
        try:
            os.remove(MARKER)
        except Exception:
            pass
        return None


def _now_iso():
    try:
        return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z"
    except Exception:
        return "1970-01-01T00:00:00.000Z"


def user_frame(text, uid, session_id="", is_replay=False):
    """The exact shape the CLI emits for a human turn. Used with the SAME uid on
    stdin (the real turn) and — as a replay — on stdout (mirror); matching uid
    lets the webview dedupe the CLI's own later replay."""
    frame = {
        "type": "user",
        "uuid": uid,
        "session_id": session_id,
        "parent_tool_use_id": None,
        "origin": {"kind": "human"},
        "message": {"role": "user", "content": [{"type": "text", "text": text}]},
    }
    if is_replay:
        frame["timestamp"] = _now_iso()
        frame["isReplay"] = True
    return json.dumps(frame) + "\n"


def main():
    argv = sys.argv[1:]
    # The plugin passes [pluginBinary, ...flags]; a bare call passes [...flags].
    cmd = argv if (argv and not argv[0].startswith("-")) else [real_claude()] + argv

    prompt = take_kickoff() if is_session_spawn(argv) else None
    if prompt is None:
        os.execvp(cmd[0], cmd)  # transparent: never in the path at all

    log("kickoff armed; proxying session spawn (argc=%d)" % len(argv))
    child = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, bufsize=0)
    injected = threading.Event()
    mirrored = {"v": False}
    init_req_id = {"v": ""}
    kickoff_uuid = str(uuid.uuid4())
    write_lock = threading.Lock()

    def pump_stdin():
        """Forward the host's NDJSON verbatim, noting the `initialize` request
        id — the success response to it is our stdin-injection cue."""
        try:
            while True:
                line = sys.stdin.buffer.readline()
                if not line:
                    break
                if not init_req_id["v"] and b'"subtype":"initialize"' in line:
                    try:
                        obj = json.loads(line)
                        if obj.get("type") == "control_request":
                            init_req_id["v"] = obj.get("request_id") or ""
                    except Exception:
                        pass
                with write_lock:
                    child.stdin.write(line)
                    child.stdin.flush()
        except Exception:
            pass
        finally:
            try:
                child.stdin.close()
            except Exception:
                pass

    def inject_stdin():
        if injected.is_set():
            return
        injected.set()
        try:
            with write_lock:
                child.stdin.write(user_frame(prompt, kickoff_uuid).encode())
                child.stdin.flush()
            log("injected turn on stdin")
        except Exception as err:
            log("stdin inject failed: %r" % (err,))

    def inject_after_timeout():
        # Safety net: if the handshake never round-trips, still submit rather
        # than leave the user staring at a panel they asked to be onboarded in.
        time.sleep(12)
        inject_stdin()

    threading.Thread(target=pump_stdin, daemon=True).start()
    threading.Thread(target=inject_after_timeout, daemon=True).start()

    try:
        for line in child.stdout:
            sys.stdout.buffer.write(line)
            sys.stdout.buffer.flush()
            # Mirror the turn UP to the webview at the earliest in-panel moment
            # (system/init). Same uuid as the stdin turn -> the CLI's own later
            # replay is deduped: no double bubble, reload stays consistent.
            if not mirrored["v"] and b'"subtype":"init"' in line and b'"type":"system"' in line:
                mirrored["v"] = True
                sid = ""
                try:
                    obj = json.loads(line)
                    if obj.get("type") == "system":
                        sid = obj.get("session_id", "") or ""
                except Exception:
                    pass
                try:
                    with write_lock:
                        sys.stdout.buffer.write(user_frame(prompt, kickoff_uuid, session_id=sid, is_replay=True).encode())
                        sys.stdout.buffer.flush()
                    log("mirrored turn to webview")
                except Exception as err:
                    log("mirror failed: %r" % (err,))
            # Cue for the stdin turn: the success response to the host's
            # `initialize` control request — the session is live and subscribed.
            if (not injected.is_set() and init_req_id["v"]
                    and b'"type":"control_response"' in line
                    and init_req_id["v"].encode() in line):
                inject_stdin()
    except Exception:
        pass

    sys.exit(child.wait())


if __name__ == "__main__":
    main()
