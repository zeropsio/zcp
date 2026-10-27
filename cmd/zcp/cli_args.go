package main

import (
	"fmt"
	"os"
	"slices"
)

// commandFlags reads a subcommand's arguments before it acts: a help request
// prints usage to stdout and an unknown flag or argument fails, so neither
// reaches the command's side effects. done tells the caller to return code
// without acting; otherwise set holds the known flags present.
func commandFlags(usage string, args []string, known ...string) (set map[string]bool, code int, done bool) {
	if slices.ContainsFunc(args, isHelpArg) {
		fmt.Fprintln(os.Stdout, usage)
		return nil, 0, true
	}
	set = make(map[string]bool, len(args))
	for _, arg := range args {
		if !slices.Contains(known, arg) {
			fmt.Fprintf(os.Stderr, "unknown argument %q\n%s\n", arg, usage)
			return nil, 2, true
		}
		set[arg] = true
	}
	return set, 0, false
}
