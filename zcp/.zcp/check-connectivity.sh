#!/bin/bash

# Gap 48: Service-to-Service Connectivity Check
# Tests internal service connectivity on Zerops vxlan network

set -o pipefail

# ============================================================================
# HELP
# ============================================================================

show_help() {
    cat <<'EOF'
.zcp/check-connectivity.sh - Test internal service connectivity

USAGE:
  .zcp/check-connectivity.sh {from_service} {to_service} {port} [endpoint]

EXAMPLES:
  .zcp/check-connectivity.sh apidev workerdev 8080
  .zcp/check-connectivity.sh apidev workerdev 8080 /health

DESCRIPTION:
  SSHs into {from_service} and attempts to connect to {to_service}:{port}
  on the internal vxlan network.

  Tests performed:
  1. DNS resolution (can from_service resolve to_service?)
  2. TCP port check (is the port open?)
  3. HTTP request (if endpoint provided, tests HTTP response)

ZEROPS NETWORKING:
  - Services use vxlan private network
  - Address: http://{hostname}:{port}
  - All services in project share the network
  - No external access needed for internal communication

COMMON ISSUES:
  - DNS fails: Service not running or wrong hostname
  - Port closed: Service not listening on expected port
  - HTTP fails: Service running but endpoint not responding
EOF
}

# ============================================================================
# MAIN
# ============================================================================

if [ "$1" = "--help" ] || [ -z "$3" ]; then
    show_help
    exit 0
fi

from_svc="$1"
to_svc="$2"
port="$3"
endpoint="${4:-/}"

echo "Testing: $from_svc -> $to_svc:$port$endpoint"
echo ""

# Validate service names
if [[ ! "$from_svc" =~ ^[a-zA-Z][a-zA-Z0-9_-]*$ ]] || \
   [[ ! "$to_svc" =~ ^[a-zA-Z][a-zA-Z0-9_-]*$ ]]; then
    echo "Error: Invalid service name"
    exit 1
fi

# Validate port
if [[ ! "$port" =~ ^[0-9]+$ ]] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
    echo "Error: Invalid port number"
    exit 1
fi

# ============================================================================
# TEST 1: DNS Resolution
# ============================================================================

echo "1. DNS Resolution..."

# NOTE: getent may not be available on all containers, so include fallbacks
dns_result=$(ssh "$from_svc" "getent hosts $to_svc 2>/dev/null || \
    nslookup $to_svc 2>/dev/null | grep -A1 'Name:' | tail -1 || \
    host $to_svc 2>/dev/null | head -1" 2>&1)
dns_exit=$?

if [ $dns_exit -eq 0 ] && [ -n "$dns_result" ]; then
    # Extract IP from various formats
    resolved_ip=$(echo "$dns_result" | grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' | head -1)
    if [ -n "$resolved_ip" ]; then
        echo "   OK: $to_svc resolves to: $resolved_ip"
    else
        echo "   OK: $to_svc resolved (format: ${dns_result:0:50}...)"
    fi
else
    echo "   FAILED: DNS resolution failed for $to_svc"
    echo "   -> Is $to_svc running? Check: zcli service list"
    exit 1
fi

# ============================================================================
# TEST 2: TCP Port Check
# ============================================================================

echo ""
echo "2. TCP Port Check..."

# NOTE: /dev/tcp is bash-specific, nc (netcat) is more portable
tcp_result=$(ssh "$from_svc" "
    if timeout 5 bash -c '</dev/tcp/$to_svc/$port' 2>/dev/null; then
        echo 'OPEN'
    elif command -v nc >/dev/null 2>&1 && timeout 5 nc -z $to_svc $port 2>/dev/null; then
        echo 'OPEN'
    elif command -v curl >/dev/null 2>&1 && timeout 5 curl -s --connect-timeout 3 http://$to_svc:$port >/dev/null 2>&1; then
        echo 'OPEN'
    else
        echo 'CLOSED'
    fi
" 2>&1)

if [ "$tcp_result" = "OPEN" ]; then
    echo "   OK: Port $port is open"
else
    echo "   FAILED: Port $port is not reachable"
    echo "   -> Is $to_svc listening on $port?"
    echo "   -> Check: ssh $to_svc 'netstat -tlnp | grep $port'"
    exit 1
fi

# ============================================================================
# TEST 3: HTTP Request (if endpoint provided)
# ============================================================================

echo ""
echo "3. HTTP Request..."

http_result=$(ssh "$from_svc" "curl -s -w '\n%{http_code}' --connect-timeout 5 http://$to_svc:$port$endpoint" 2>&1)
http_code="${http_result##*$'\n'}"
http_body="${http_result%$'\n'*}"

if [[ "$http_code" =~ ^2 ]]; then
    echo "   OK: HTTP $http_code"
    echo ""
    echo "Response (first 200 chars):"
    echo "${http_body:0:200}"
elif [[ "$http_code" =~ ^[0-9]{3}$ ]]; then
    echo "   WARNING: HTTP $http_code (not 2xx)"
    echo ""
    echo "Response:"
    echo "${http_body:0:500}"
    # Don't exit with error - some endpoints return 4xx/5xx intentionally
else
    echo "   FAILED: Could not complete HTTP request"
    echo "   Response: ${http_result:0:200}"
    exit 1
fi

echo ""
echo "============================================================================"
echo "CONNECTIVITY OK: $from_svc -> $to_svc:$port$endpoint"
echo "============================================================================"
