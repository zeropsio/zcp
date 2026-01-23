#!/bin/bash
# Security hook for Claude Code - blocks dangerous env patterns
# Called by Claude Code PreToolCall hook

# Read the tool input from stdin (JSON)
input=$(cat)

# Extract the command from Bash tool calls
command=$(echo "$input" | jq -r '.tool_input.command // empty' 2>/dev/null)

if [ -z "$command" ]; then
    # Not a Bash tool call or no command - allow
    exit 0
fi

# Patterns that expose secrets
DANGEROUS_PATTERNS=(
    # Dumping all environment variables
    "ssh .* ['\"]env['\"]"
    "ssh .* ['\"]env |"
    "ssh .* ['\"]env\|"
    "ssh .* ['\"]printenv"
    "ssh .* 'env'"
    "ssh .* \"env\""
    "ssh .* 'printenv"
    "ssh .* \"printenv"
    # Grepping env output (exposes matching secrets)
    "env.*|.*grep"
    "printenv.*|"
)

for pattern in "${DANGEROUS_PATTERNS[@]}"; do
    if echo "$command" | grep -qiE "$pattern"; then
        cat <<EOF
{
  "decision": "block",
  "reason": "SECURITY: Command would expose environment variables/secrets. Use 'ssh service \"echo \\\$VAR_NAME\"' to fetch specific variables, or use 'source .zcp/lib/env.sh && env_from service VAR_NAME'"
}
EOF
        exit 0
    fi
done

# Check for hardcoded passwords in connection strings
# Pattern: protocol://user:password@host where password is literal (not a variable)
if echo "$command" | grep -qE '://[^:]+:[^$@{][^@]*@'; then
    # Might be a hardcoded password - check if it's not a variable reference
    if ! echo "$command" | grep -qE '://[^:]+:\$'; then
        cat <<EOF
{
  "decision": "block",
  "reason": "SECURITY: Command appears to contain hardcoded credentials. Use command substitution: psql \"\$(env_from service db_connectionString)\" instead of hardcoding passwords"
}
EOF
        exit 0
    fi
fi

# Allow the command
echo '{"decision": "allow"}'
exit 0
