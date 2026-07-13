package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/zeropsio/zcp/internal/capture"
)

const bootstrapGuideSeparator = "\n\n---\n\n"

type bootstrapGuideComposition struct {
	builder    strings.Builder
	components []capture.CompositionComponent
}

func (c *bootstrapGuideComposition) append(kind, owner, text string) {
	start := c.builder.Len()
	c.builder.WriteString(text)
	c.components = append(c.components, capture.CompositionComponent{Kind: kind, Owner: owner, Start: start, End: c.builder.Len()})
}

func (c *bootstrapGuideComposition) separator() {
	c.append("separator", "workflow.bootstrapGuideSeparator", bootstrapGuideSeparator)
}

func (c *bootstrapGuideComposition) String() string { return c.builder.String() }
func (c *bootstrapGuideComposition) Len() int       { return c.builder.Len() }

func (c *bootstrapGuideComposition) record() {
	output := c.String()
	_, err := capture.RecordCompositionFromEnvironment(capture.CompositionRecord{
		Surface:    "bootstrap.guide",
		Output:     output,
		Components: append([]capture.CompositionComponent(nil), c.components...),
	})
	if err != nil {
		// Capture-only failures stay off the MCP payload but cannot be silent.
		fmt.Fprintf(os.Stderr, "zcp capture provenance: %v\n", err)
	}
}
