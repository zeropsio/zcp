//go:build api

package schema

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestImportValidator_PublishedPRG1Schema_ValidatesFlipShape(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	validator, err := NewCache(time.Hour, "", nil).ImportValidator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if errs := validator.Validate(flipImportYAML); len(errs) != 0 {
		t.Fatalf("published schema rejected nested flip shape: %+v", errs)
	}
	errs := validator.Validate("services:\n  - hostname: flip\n    type: ubuntu/nodejs@24\n    minCpu: 1\n")
	if len(errs) == 0 {
		t.Fatal("published schema accepted flat minCpu")
	}
	if errs[0].Service != "flip" || !strings.Contains(errs[0].Correction, "verticalAutoscaling.minCpu") {
		t.Fatalf("published schema finding lacks recovery: %+v", errs)
	}
	t.Logf("published import schema: %s sha256=%s", validator.SourceURL, validator.Digest)
}
