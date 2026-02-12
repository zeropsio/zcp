#!/bin/sh
# Copyright (c) 2026 Zerops s.r.o. All rights reserved. MIT license.
#
# Canonical install command:
#   curl -sSfL https://raw.githubusercontent.com/zeropsio/zcp/v2/install.sh | sh
#
# Install specific version:
#   curl -sSfL https://raw.githubusercontent.com/zeropsio/zcp/v2/install.sh | sh -s v0.1.0
#
# Zerops initCommands (HOME=/, needs sudo for /usr/local/bin):
#   curl -sSfL https://raw.githubusercontent.com/zeropsio/zcp/v2/install.sh | sudo sh
#
# NOTE: Update URL branch (v2 → main) when this branch is merged.

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

curl --fail --location --progress-bar --output "$bin_path" "$zcp_uri"
chmod +x "$bin_path"

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
