"use strict";

// agent-launch handler — S4 (L-AG-2, L-AG-4, R-SEC-LOCAL).
//
// Opens Claude Code with the DEFAULT safety posture. Preferred path is the
// Claude Code VS Code extension command; if that command isn't registered (the
// extension isn't installed) it throws and we fall back to a terminal running
// the bare `claude` CLI.
//
// CRITICAL safety law (L-AG-4 / R-SEC-LOCAL): the launch command is plain
// `claude` — Studio NEVER passes the permission-bypass flag (nor any other flag
// that skips Claude Code's approval prompts). Studio's job is to start the
// agent, not to weaken its guardrails; permission posture stays Claude Code's
// own default. The agent.test.js source pin enforces this verbatim.
//
// vscode is required LAZILY inside handle() so the module loads cleanly under
// plain node in tests (the router require()s every handler at activation).

module.exports = {
  type: "agent-launch",
  handle: async function handle(msg, ctx) {
    const vscode = require("vscode");
    try {
      await vscode.commands.executeCommand("claude-vscode.editor.open");
    } catch (_) {
      // Extension command unavailable — fall back to a terminal running the
      // bare CLI. No bypass flag: plain `claude`, default safety posture.
      const term = vscode.window.createTerminal("Claude Code");
      term.show();
      term.sendText("claude");
    }
  },
};
