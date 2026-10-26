// Command mate-release-check refuses a zcp release until a compatible Mate is published.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/zeropsio/zcp/internal/mate"
)

func main() {
	os.Exit(run(context.Background(), nil, os.Stderr))
}

func run(ctx context.Context, client *http.Client, output io.Writer) int {
	published, err := mate.CheckPublishedRelease(ctx, client)
	if err != nil {
		fmt.Fprintf(output, "Release refused: %v. Publish a compatible Mate release first.\n", err)
		return 1
	}
	fmt.Fprintf(output, "Published Mate %s (contract %d) permits this zcp release.\n", published.Version, published.Contract)
	return 0
}
