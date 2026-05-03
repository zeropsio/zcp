const vscode = require("vscode");

const TARGET_COMMAND = "claude-vscode.editor.open";
const CLAUDE_EXT_ID = "anthropic.claude-code";

async function waitForCommand(commandId, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const cmds = await vscode.commands.getCommands(true);
      if (cmds.includes(commandId)) return true;
    } catch (_) {}
    await new Promise(r => setTimeout(r, 250));
  }
  return false;
}

async function activate(_ctx) {
  console.log("[zcp-bootstrap] activate");

  const hasEditors = vscode.window.tabGroups.all.some(g => g.tabs.length > 0);
  if (hasEditors) {
    console.log("[zcp-bootstrap] editors already open, skipping auto-open");
    return;
  }

  const ext = vscode.extensions.getExtension(CLAUDE_EXT_ID);
  if (!ext) {
    console.warn("[zcp-bootstrap] anthropic.claude-code not installed");
    return;
  }
  try {
    if (!ext.isActive) await ext.activate();
  } catch (err) {
    console.error("[zcp-bootstrap] claude-code activate failed:", err);
  }

  const ready = await waitForCommand(TARGET_COMMAND, 8000);
  if (!ready) {
    console.warn("[zcp-bootstrap] " + TARGET_COMMAND + " never registered");
    return;
  }

  try {
    await vscode.commands.executeCommand(TARGET_COMMAND);
    console.log("[zcp-bootstrap] opened Claude Code as tab");
  } catch (err) {
    console.error("[zcp-bootstrap] open command failed:", err);
  }

  try {
    await vscode.commands.executeCommand("workbench.action.closeAuxiliaryBar");
  } catch (_) {}
}

function deactivate() {}

module.exports = { activate, deactivate };
