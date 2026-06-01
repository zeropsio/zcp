#!/bin/sh
# Copyright (c) 2026 Zerops s.r.o. All rights reserved. MIT license.
#
# Canonical install command:
#   curl -sSfL https://raw.githubusercontent.com/zeropsio/zcp/main/install.sh | sh
#
# Install specific version:
#   curl -sSfL https://raw.githubusercontent.com/zeropsio/zcp/main/install.sh | sh -s v0.1.0
#
# Zerops initCommands (HOME=/, needs sudo for /usr/local/bin):
#   curl -sSfL https://raw.githubusercontent.com/zeropsio/zcp/main/install.sh | sudo sh

set -e

case $(uname -sm) in
"Darwin x86_64") target="darwin-amd64" ;;
"Darwin arm64") target="darwin-arm64" ;;
"Linux i386") target="linux-386" ;;
*) target="linux-amd64" ;;
esac

if [ $# -eq 0 ]; then
  zcp_uri="https://github.com/zeropsio/zcp/releases/latest/download/zcp-${target}"
else
  zcp_uri="https://github.com/zeropsio/zcp/releases/download/${1}/zcp-${target}"
fi

# Determine install directory.
# - Root (sudo): /usr/local/bin — system-wide, visible to all users.
# - Normal user with valid HOME: $HOME/.local/bin — per-user install.
# - HOME unset or "/" (Zerops initCommands): /usr/local/bin fallback.
bin_dir_existed=1
if [ "$(id -u)" = "0" ]; then
  bin_dir="/usr/local/bin"
elif [ -n "$HOME" ] && [ "$HOME" != "/" ]; then
  bin_dir="$HOME/.local/bin"
  if [ ! -d "$bin_dir" ]; then
    if mkdir -p "$bin_dir" 2>/dev/null; then
      bin_dir_existed=0
      # Reload profile so newly-created ~/.local/bin lands in PATH.
      if [ "$(uname -s)" = "Linux" ]; then
        if [ -f "$HOME/.bash_profile" ]; then
          . "$HOME/.bash_profile"
        elif [ -f "$HOME/.profile" ]; then
          . "$HOME/.profile"
        fi
      fi
    else
      bin_dir="/usr/local/bin"
    fi
  fi
else
  bin_dir="/usr/local/bin"
fi
bin_path="$bin_dir/zcp"

# Download the binary with retries. GitHub's release CDN occasionally
# returns a transient 5xx (502/503/504), drops the connection mid-stream,
# or hands back an empty 200 — each should be retried, not fatal. Download
# to a temp file and only move it into place once a non-empty file actually
# arrives, so a failed attempt never clobbers an existing working binary or
# leaves a half-written one behind.
tmp_path="$bin_path.$$.download"
trap 'rm -f "$tmp_path"' EXIT

attempt=1
max_attempts=3
while :; do
  # --connect-timeout bounds a hung connect; --speed-limit/--speed-time abort
  # a wedged transfer (sustained <1 KB/s for 30s) so the retry can take over.
  # A slow-but-progressing link stays above that floor and is never cut off.
  # The trailing [ -s ] retries an empty 200 instead of installing 0 bytes.
  curl --fail --location --connect-timeout 30 \
    --speed-limit 1024 --speed-time 30 --progress-bar \
    --output "$tmp_path" "$zcp_uri" && [ -s "$tmp_path" ] && break
  status=$?
  if [ "$attempt" -ge "$max_attempts" ]; then
    echo "zcp download failed after $max_attempts attempts (exit $status)." >&2
    echo "  url: $zcp_uri" >&2
    echo "  This is usually a transient GitHub/CDN error; re-run the install in a moment." >&2
    exit "$status"
  fi
  delay=$((attempt * 2))
  echo "zcp download attempt $attempt/$max_attempts failed (exit $status); retrying in ${delay}s..." >&2
  sleep "$delay"
  attempt=$((attempt + 1))
done

chmod +x "$tmp_path"
mv -f "$tmp_path" "$bin_path"
trap - EXIT

# If installed to /usr/local/bin (root) and ~/.local/bin exists, ensure
# a symlink there so PATH preference doesn't shadow the system binary.
if [ "$bin_dir" = "/usr/local/bin" ] && [ -n "$HOME" ] && [ "$HOME" != "/" ]; then
  user_bin="$HOME/.local/bin/zcp"
  if [ -f "$user_bin" ] && [ ! -L "$user_bin" ]; then
    rm -f "$user_bin"
    ln -s "$bin_path" "$user_bin"
    echo "Replaced $user_bin (stale copy) with symlink → $bin_path"
  elif [ -d "$HOME/.local/bin" ] && [ ! -e "$user_bin" ]; then
    ln -s "$bin_path" "$user_bin"
  fi
fi

echo
echo "zcp was installed successfully to '$bin_path'"

if command -v zcp >/dev/null; then
  echo "Run 'zcp --help' to get started"
  if [ "$bin_dir_existed" = 0 ]; then
    echo "You may need to relaunch your shell."
  fi
else
  if [ "$(uname -s)" = "Darwin" ]; then
    echo 'Add the following line to /etc/paths and relaunch your shell:'
    echo "  $HOME/.local/bin"
    echo
    echo 'You can do so by running:'
    echo "sudo sh -c 'echo \"$HOME/.local/bin\" >> /etc/paths'"
  else
    echo "Manually add the directory to your '\$HOME/.profile' (or similar) and relaunch your shell."
    echo '  export PATH="$HOME/.local/bin:$PATH"'
  fi
  echo
  echo "Run '$bin_path --help' to get started"
fi

echo
echo "Stuck? Join our Discord https://discord.com/invite/WDvCZ54"
