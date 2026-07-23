package skillpacks

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// frontmatterScanBytes bounds how much of a SKILL.md's leading content the
// frontmatter scanner will ever look at (a "bounded YAML frontmatter" scan,
// as opposed to reading and parsing an arbitrarily large file just to reach
// two delimiter lines).
const frontmatterScanBytes = 64 * 1024

// frontmatter is a third-party SKILL.md's front-matter, reduced to the two
// standard fields ZCP requires. Unknown upstream fields are ignored here (as
// they will be by any other reader) and never rewritten — ZCP does not edit
// third-party SKILL.md content.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// parseFrontmatter extracts name/description from a SKILL.md's leading
// "---\n...\n---" block. Both fields must be nonempty after parsing.
func parseFrontmatter(content []byte) (frontmatter, error) {
	scan := content
	if len(scan) > frontmatterScanBytes {
		scan = scan[:frontmatterScanBytes]
	}
	lines := strings.Split(string(scan), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return frontmatter{}, fmt.Errorf("SKILL.md must start with a \"---\" frontmatter delimiter")
	}
	closingIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			closingIdx = i
			break
		}
	}
	if closingIdx == -1 {
		return frontmatter{}, fmt.Errorf("SKILL.md frontmatter has no closing \"---\" within the first %d bytes", frontmatterScanBytes)
	}

	yamlBlock := strings.Join(lines[1:closingIdx], "\n")
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return frontmatter{}, fmt.Errorf("parse SKILL.md frontmatter: %w", err)
	}
	if strings.TrimSpace(fm.Name) == "" {
		return frontmatter{}, fmt.Errorf("SKILL.md frontmatter is missing a nonempty \"name\"")
	}
	if strings.TrimSpace(fm.Description) == "" {
		return frontmatter{}, fmt.Errorf("SKILL.md frontmatter is missing a nonempty \"description\"")
	}
	return fm, nil
}
