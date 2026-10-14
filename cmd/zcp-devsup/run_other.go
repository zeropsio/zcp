//go:build !linux && !darwin

package main

import (
	"fmt"
	"io"
)

func runSupervisor(_ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "zcp-devsup runs on Linux dev containers (and macOS for development) only")
	return 1
}

func runCtl(args []string, stdout, stderr io.Writer) int { return runSupervisor(args, stdout, stderr) }
