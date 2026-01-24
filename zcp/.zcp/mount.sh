#!/bin/bash
# Create SSHFS mount for a dev service

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

# Check if already mounted
if [ -d "/var/www/$svc" ] && [ -n "$(ls -A /var/www/$svc 2>/dev/null)" ]; then
    echo "✓ /var/www/$svc already mounted"
    exit 0
fi

echo "Creating mount for $svc..."
mkdir -p "/var/www/$svc"
sudo -E zsc unit create "sshfs-$svc" "sshfs -f -o reconnect,StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3 $svc:/var/www /var/www/$svc"

# Verify
sleep 1
if [ -n "$(ls -A /var/www/$svc 2>/dev/null)" ]; then
    echo "✓ Mounted at /var/www/$svc"
else
    echo "⚠ Mount created but directory empty - service may not be running yet"
fi
