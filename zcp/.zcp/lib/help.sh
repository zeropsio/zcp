#!/bin/bash
# Help system loader for Zerops Workflow

# Use local variable to avoid overwriting parent SCRIPT_DIR
_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source help modules
source "$_LIB_DIR/help/full.sh"
source "$_LIB_DIR/help/topics.sh"
