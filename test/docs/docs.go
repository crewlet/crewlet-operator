// Package docs reads the manifests the user docs hand to kubectl, and the
// tables they state, so that the suites test what a reader is told to apply
// and told is true rather than a copy of it that can drift. Its own tests
// hold the docs tree to what publishing it needs: every page listed in
// docs/README.md, and every link and anchor resolving, here and in the
// engine's documentation at the crewlet main every suite runs
// (CONTRIBUTING.md, "How the docs are published").
package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// Table returns the body rows of the first Markdown table in the file at path
// whose header row's first cell is header, each row as its cells with the
// surrounding spaces trimmed. The header and the delimiter row are not
// returned.
func Table(path, header string) ([][]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		cells := tableCells(line)
		if len(cells) == 0 || cells[0] != header {
			continue
		}
		if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "|---") {
			return nil, fmt.Errorf("%s: the table headed %q has no delimiter row", path, header)
		}
		var rows [][]string
		for _, row := range lines[i+2:] {
			body := tableCells(row)
			if body == nil {
				break
			}
			if len(body) != len(cells) {
				return nil, fmt.Errorf("%s: the table headed %q has a row of %d cells under %d columns: %q",
					path, header, len(body), len(cells), row)
			}
			rows = append(rows, body)
		}
		return rows, nil
	}
	return nil, fmt.Errorf("%s holds no table headed %q", path, header)
}

// tableCells is a Markdown table row's cells, or nil for a line that is no
// row. A cell holds no escaped pipe in the tables the suites read.
func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") || len(line) < 2 {
		return nil
	}
	parts := strings.Split(line[1:len(line)-1], "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
