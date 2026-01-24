#!/usr/bin/env bash
# DEPRECATED - Use bootstrap instead: .zcp/workflow.sh bootstrap

RED="${RED:-\033[0;31m}"
YELLOW="${YELLOW:-\033[1;33m}"
NC="${NC:-\033[0m}"

cmd_compose() {
    echo -e "${RED}ERROR: compose is deprecated. Use bootstrap instead:${NC}"
    echo ""
    echo "  .zcp/workflow.sh bootstrap --runtime <type> --services <list>"
    echo ""
    echo "For help: .zcp/workflow.sh bootstrap --help"
    return 1
}

cmd_verify_synthesis() {
    echo -e "${RED}ERROR: verify_synthesis is deprecated.${NC}"
    echo ""
    echo "Use: .zcp/verify.sh {hostname} 8080 / /status"
    return 1
}

export -f cmd_compose cmd_verify_synthesis
