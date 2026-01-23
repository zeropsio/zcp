#!/bin/bash
# Create SSHFS mount for a dev service

if [ -z "$1" ]; then
    echo "Usage: .zcp/mount.sh <service>"
    echo "Example: .zcp/mount.sh appdev"
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
