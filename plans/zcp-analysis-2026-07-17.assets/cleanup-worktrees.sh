#!/usr/bin/env bash
# Provably-safe worktree cleanup (12 of 16, ~617MB). PRECONDITION: feat/eval-nextgen branch stays (7 worktree branches are contained in it).
# NEEDS-HUMAN (do NOT add here): agent-af2165f7b84622358 (52 unique commits, nowhere else), frolicking-snacking-sutton (dirty: 6 untracked guided-mode drafts), keen-strolling-hare (locked, live pid), zesty-brewing-canyon (multiproject-impl, dirty, 71 ahead).
set -e
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/agent-a3c592904a6e39f9e
git branch -D worktree-agent-a3c592904a6e39f9e
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/agent-a50c701d263160277
git branch -D worktree-agent-a50c701d263160277
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/agent-a6a91a0b3d1606034
git branch -D worktree-agent-a6a91a0b3d1606034
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/agent-a9119a7e72e25b6e0
git branch -D worktree-agent-a9119a7e72e25b6e0
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/agent-aa4ad0745ad83b142
git branch -D worktree-agent-aa4ad0745ad83b142
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/agent-ab3529f8516b9aa27
git branch -D worktree-agent-ab3529f8516b9aa27
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/agent-adb7fbc644702ce95
git branch -D worktree-agent-adb7fbc644702ce95

# Group 2 — 0 commits ahead of main, clean checkout, nothing unique
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/bright-oak-dr92
git branch -D worktree-bright-oak-dr92
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/keen-elm-rshx
git branch -D worktree-keen-elm-rshx
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/swift-fox-du77
git branch -D worktree-swift-fox-du77
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/swift-napping-wand
git branch -D worktree-swift-napping-wand
git worktree remove /Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/wiggly-soaring-toucan
git branch -D worktree-wiggly-soaring-toucan
