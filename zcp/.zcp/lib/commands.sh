#!/bin/bash
# Commands loader for Zerops Workflow

# Use local variable to avoid overwriting parent SCRIPT_DIR
_CMD_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source all command modules
source "$_CMD_LIB_DIR/commands/init.sh"
source "$_CMD_LIB_DIR/commands/transition.sh"
source "$_CMD_LIB_DIR/commands/discovery.sh"
source "$_CMD_LIB_DIR/commands/status.sh"
source "$_CMD_LIB_DIR/commands/extend.sh"
