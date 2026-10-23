package ops

import (
	"regexp"
	"strings"
)

// configFileName matches the repo config file under either name it may have,
// never a longer name that ends in one (notzerops.yml).
var configFileName = regexp.MustCompile(`\bzerops\.ya?ml\b`)

// NameRepoConfigFile returns text with every mention of the repo's config
// file named as the repo keeps it (file is "zerops.yaml" or "zerops.yml";
// "" leaves text as it is). A GitHub Actions workflow file
// (`.github/workflows/zerops.yml`) is a different file and keeps its name.
// Only text zcp writes itself is named: logs, platform errors and URLs say
// what their writer saw.
func NameRepoConfigFile(text, file string) string {
	if file == "" {
		return text
	}
	var out strings.Builder
	last := 0
	for _, loc := range configFileName.FindAllStringIndex(text, -1) {
		if strings.HasSuffix(text[:loc[0]], "workflows/") {
			continue
		}
		out.WriteString(text[last:loc[0]])
		out.WriteString(file)
		last = loc[1]
	}
	if last == 0 {
		return text
	}
	out.WriteString(text[last:])
	return out.String()
}

// configFileError is err with its text naming the repo's config file as file
// keeps it (NameRepoConfigFile); errors.Is and errors.As still reach err.
type configFileError struct {
	err  error
	file string
}

func (e configFileError) Error() string { return NameRepoConfigFile(e.err.Error(), e.file) }

func (e configFileError) Unwrap() error { return e.err }
