package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// maxArchiveNoteChars is `farm archive --note`'s length limit, the same
// discipline as `farm run --note` (§3.3/maxNoteChars).
const maxArchiveNoteChars = 200

// flagArchiveList is `farm archive --list`'s flag name.
const flagArchiveList = "--list"

// runFarmArchive implements `zcp eval farm archive <batch…> [--note "<why>"]`
// and `farm archive --list` (docs/spec-eval-farm.md §3.7): it writes/reads
// batches/<batch>/archived.json, the console's sole signal to hide a batch
// from every default view without deleting any evidence object (§1.4:
// manifests are evidence, never deleted).
func runFarmArchive(args []string, envr *farm.EnvResolver) int {
	list, batches, note, err := parseFarmArchiveArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}
	if n := utf8.RuneCountInString(note); n > maxArchiveNoteChars {
		fmt.Fprintf(os.Stderr, "error: --note: %d characters, want at most %d\n", n, maxArchiveNoteChars)
		return 2
	}

	cfg, err := farmConfigFromResolver(envr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	sink := farm.NewSinkClient(cfg)
	ctx := context.Background()

	if list {
		return runFarmArchiveList(ctx, sink)
	}
	if len(batches) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one batch id is required (or --list)")
		return 2
	}

	// Verify every batch exists before writing anything: a partial archive
	// (some batches marked, one refused) would leave the console unable to
	// explain why a run left the default view (§3.7).
	for _, b := range batches {
		if !farm.ValidBatchID(b) {
			fmt.Fprintf(os.Stderr, "error: %q does not match the batch-id grammar (docs/spec-eval-farm.md §7.6 FM-47)\n", b)
			return 2
		}
		if _, err := farm.GetManifest(ctx, sink, b); err != nil {
			fmt.Fprintf(os.Stderr, "error: batch %s: no manifest.json, refusing to archive\n", b)
			return 1
		}
	}

	marker := farm.ArchiveMarker{ArchivedAt: time.Now().UTC().Format(time.RFC3339), Note: note}
	exit := 0
	for _, b := range batches {
		if err := farm.PutArchive(ctx, sink, b, marker); err != nil {
			if errors.Is(err, farm.ErrObjectExists) {
				fmt.Fprintf(os.Stdout, "batch %s: already archived (no-op)\n", b)
				continue
			}
			fmt.Fprintf(os.Stderr, "error: archive %s: %v\n", b, err)
			exit = 1
			continue
		}
		fmt.Fprintf(os.Stdout, "batch %s: archived\n", b)
	}
	return exit
}

// runFarmArchiveList implements `farm archive --list`: every batch with an
// archive marker, one id per line.
func runFarmArchiveList(ctx context.Context, sink *farm.SinkClient) int {
	ids, err := farm.ListBatches(ctx, sink)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: list batches: %v\n", err)
		return 1
	}
	for _, id := range ids {
		if _, err := farm.GetArchive(ctx, sink, id); err == nil {
			fmt.Fprintln(os.Stdout, id)
		}
	}
	return 0
}

// parseFarmArchiveArgs parses `farm archive`'s flags: every non-flag
// argument is a batch id; --note takes a value; --list takes none.
func parseFarmArchiveArgs(args []string) (list bool, batches []string, note string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case flagArchiveList:
			list = true
		case "--note":
			if i+1 >= len(args) {
				return false, nil, "", fmt.Errorf("--note requires a value")
			}
			note = args[i+1]
			i++
		default:
			batches = append(batches, arg)
		}
	}
	return list, batches, note, nil
}
