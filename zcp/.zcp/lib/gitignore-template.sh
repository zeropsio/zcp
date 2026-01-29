#!/usr/bin/env bash
# .zcp/lib/gitignore-template.sh - Generate standard .gitignore for bootstrapped apps
#
# Usage: .zcp/lib/gitignore-template.sh > /var/www/app/.gitignore

cat <<'GITIGNORE'
# Dependencies (auto-generated)
node_modules/
vendor/
.venv/
__pycache__/

# Lock files that may cause issues if pre-committed
# (uncomment if you want to exclude them)
# *.lock
# go.sum

# Build outputs
/dist/
/build/
*.exe
*.dll
*.so
*.dylib

# IDE
.idea/
.vscode/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db

# Logs
*.log
/tmp/

# Environment (secrets)
.env
.env.local
*.pem
*.key
GITIGNORE
