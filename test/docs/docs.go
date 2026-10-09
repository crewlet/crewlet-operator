// Package docs reads the manifests the user docs hand to kubectl, so that the
// suites test what a reader is told to apply rather than a copy of it that
// can drift.
package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// heredoc is a shell here-document fed to kubectl in a doc's code block: the
// lines between a `<<'EOF'` and the EOF that closes it.
var heredoc = regexp.MustCompile(`(?s)<<'EOF'\n(.*?)\nEOF\n`)

// Manifests returns the body of every here-document in the Markdown file at
// path, in the order they appear: the manifests the page applies.
func Manifests(path string) ([]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	matches := heredoc.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("%s holds no <<'EOF' here-document", path)
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, string(m[1]))
	}
	return out, nil
}
