package main

import "errors"

// The private mode is only a STDIO child owned by Mate's invocation bridge.
// Initialization advertises its capability; an old binary that ignores flags
// cannot satisfy that handshake. Other historical MCP invocations stay ordinary.
func serveMode(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "serve" {
		return false, nil
	}
	if len(args) == 1 {
		return false, nil
	}
	if len(args) == 2 && args[1] == "--mate-invocation" {
		return true, nil
	}
	return false, errors.New("usage: zcp serve [--mate-invocation]")
}
