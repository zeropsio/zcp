#!/bin/bash
# Create SSHFS mount for a dev service

set -o pipefail
umask 077

# Source validation functions
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$SCRIPT_DIR/lib/validate.sh" ]; then
    source "$SCRIPT_DIR/lib/validate.sh"
fi

# Handle --help
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    cat <<'EOF'
.zcp/mount.sh - Create SSHFS mount for a dev service

USAGE:
  .zcp/mount.sh <service>
  .zcp/mount.sh --help

EXAMPLES:
  .zcp/mount.sh appdev     # Mount appdev at /var/www/appdev
  .zcp/mount.sh backend    # Mount backend at /var/www/backend

DESCRIPTION:
  Creates an SSHFS mount from a runtime service's /var/www directory
  to /var/www/<service> on ZCP. This allows direct file editing.

  The mount will automatically reconnect if the connection drops.

NOTES:
  - Only works with runtime services (go, nodejs, php, python, etc.)
  - Managed services (postgresql, valkey, etc.) cannot be mounted
  - Run after service import to enable SSHFS editing on dev
  - Empty directory is EXPECTED with startWithoutCode: true (this is correct!)
EOF
    exit 0
fi

if [ -z "$1" ]; then
    echo "Usage: .zcp/mount.sh <service>"
    echo "Example: .zcp/mount.sh appdev"
    echo "Use --help for more information"
    exit 1
fi

svc="$1"

# CRITICAL: Validate service name to prevent command injection (CRITICAL-1)
if type validate_service_name &>/dev/null; then
    if ! validate_service_name "$svc"; then
        echo "Use --help for valid service name format"
        exit 1
    fi
else
    # Fallback validation if validate.sh not loaded
    if [[ ! "$svc" =~ ^[a-zA-Z][a-zA-Z0-9_-]{0,62}$ ]]; then
        echo "ERROR: Invalid service name: '$svc'" >&2
        echo "       Must start with letter, contain only [a-zA-Z0-9_-], max 63 chars" >&2
        exit 1
    fi
fi

# Check if already mounted (mount point exists AND is accessible via SSHFS)
# Note: empty directory is valid with startWithoutCode: true
if [ -d "/var/www/$svc" ] && mountpoint -q "/var/www/$svc" 2>/dev/null; then
    echo "✓ /var/www/$svc already mounted"
    exit 0
fi

# Fallback check if mountpoint command not available
if [ -d "/var/www/$svc" ] && ls "/var/www/$svc" >/dev/null 2>&1 && [ -e "/var/www/$svc/.." ]; then
    # If we can access parent via mount, it's likely mounted
    if mount | grep -q "/var/www/$svc"; then
        echo "✓ /var/www/$svc already mounted"
        exit 0
    fi
fi

echo "Creating mount for $svc..."
mkdir -p "/var/www/$svc"
sudo -E zsc unit create "sshfs-$svc" "sshfs -f -o reconnect,StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3 $svc:/var/www /var/www/$svc"

# Verify mount is accessible
sleep 1
if ls "/var/www/$svc" >/dev/null 2>&1; then
    local_files=$(ls -A "/var/www/$svc" 2>/dev/null | wc -l)
    if [ "$local_files" -eq 0 ]; then
        echo "✓ Mounted at /var/www/$svc (empty - expected with startWithoutCode: true)"
    else
        echo "✓ Mounted at /var/www/$svc ($local_files files)"
    fi
else
    echo "⚠ Mount may not be accessible - check service status"
fi
