package docs

// The documentation is published twice: as it reads on GitHub, and on the
// documentation site, which reads docs/README.md's lists as its navigation,
// renders them as cards on the index page, and fails its own build on a page
// the index does not list. These tests hold the tree to both before either can
// break: every page is in the index, as a card, every relative link names a
// file that exists, every #anchor names a heading its target renders, every
// link into the engine's repository names a file and heading of the engine
// every suite runs, and every absolute link into the documentation site names
// a page it builds. A broken link fails here, in this repository's own CI,
// rather than on GitHub where nobody is told or on the site where the build
// that notices is not this repository's.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// repoRoot is the module root: this package is test/docs, two levels below it.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("%s is not the module root: %v", root, err)
	}
	return root
}

// markdownFiles is every Markdown file a reader is pointed at: the files at
// the repository root, docs/, adr/ and the GitHub templates. bin/ (the pinned
// tools' own trees) and the generated chart are not this repository's prose.
func markdownFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			files = append(files, filepath.Join(root, entry.Name()))
		}
	}
	for _, dir := range []string{"docs", "adr", ".github"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".md") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(files)
	return files
}

// fence opens or closes a fenced code block: its marker character and length.
var fence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

// prose returns the lines of a Markdown file, index for index, with every line
// of a fenced code block emptied, so that neither a link nor a heading is read
// out of an example.
func prose(source string) []string {
	lines := strings.Split(source, "\n")
	out := make([]string, len(lines))
	open := ""
	for i, line := range lines {
		if m := fence.FindStringSubmatch(line); m != nil {
			marker := m[1]
			switch {
			case open == "":
				open = marker
			case marker[0] == open[0] && len(marker) >= len(open) &&
				strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), marker[:1])) == "":
				open = ""
			}
			continue
		}
		if open == "" {
			out[i] = line
		}
	}
	return out
}

// stripCode removes inline code spans from a line, so that a regular
// expression or a YAML fragment quoted in prose is not read as a link.
func stripCode(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] != '`' {
			b.WriteByte(line[i])
			i++
			continue
		}
		run := i
		for run < len(line) && line[run] == '`' {
			run++
		}
		ticks := line[i:run]
		end := strings.Index(line[run:], ticks)
		if end < 0 {
			// An unmatched run is literal backticks.
			b.WriteString(ticks)
			i = run
			continue
		}
		i = run + end + len(ticks)
	}
	return b.String()
}

var (
	// inlineLink is the destination of an inline link or image:
	// `[text](dest)`, `[text](<dest>)`, `[text](dest "title")`.
	inlineLink = regexp.MustCompile(`\]\(\s*(?:<([^>]*)>|([^)\s]+))(?:\s+"[^"]*")?\s*\)`)
	// refDefinition is a link reference definition: `[label]: dest`.
	refDefinition = regexp.MustCompile(`^ {0,3}\[[^\]]+\]:\s*<?(\S+?)>?(?:\s|$)`)
	// htmlLink is an href or src attribute in raw HTML.
	htmlLink = regexp.MustCompile(`\b(?:href|src)="([^"]*)"`)
)

// link is one destination written in a Markdown file.
type link struct {
	line   int
	target string
}

// links returns every link destination in a Markdown file, outside code.
func links(source string) []link {
	var out []link
	for i, line := range prose(source) {
		line = stripCode(line)
		for _, m := range inlineLink.FindAllStringSubmatch(line, -1) {
			out = append(out, link{i + 1, m[1] + m[2]})
		}
		if m := refDefinition.FindStringSubmatch(line); m != nil {
			out = append(out, link{i + 1, m[1]})
		}
		for _, m := range htmlLink.FindAllStringSubmatch(line, -1) {
			out = append(out, link{i + 1, m[1]})
		}
	}
	return out
}

var (
	atxHeading    = regexp.MustCompile(`^ {0,3}#{1,6}(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
	setextLine    = regexp.MustCompile(`^ {0,3}(?:=+|-+)[ \t]*$`)
	htmlAnchor    = regexp.MustCompile(`<a\s+(?:id|name)="([^"]+)"`)
	markdownLink  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	htmlTag       = regexp.MustCompile(`<[^>]+>`)
	notSetextText = regexp.MustCompile(`^\s*(?:$|[-*+>|#]|\d+[.)]\s)`)
)

// slug is the anchor GitHub gives a heading, and the documentation site's
// renderer gives it too (both use the github-slugger rule): the heading's
// text, lower-cased, with every character that is not a letter, a digit, a
// hyphen or an underscore removed and each space turned into a hyphen. Runs
// are not collapsed: "LLM ↔ tool" is "llm--tool".
func slug(heading string) string {
	text := markdownLink.ReplaceAllString(heading, "$1")
	text = htmlTag.ReplaceAllString(text, "")
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// anchors returns every fragment a Markdown file can be linked to by: each
// heading's slug, a repeated one suffixed -1, -2, … in document order as
// GitHub does, and the id or name of every <a> anchor in its HTML.
func anchors(source string) map[string]bool {
	out := map[string]bool{}
	seen := map[string]int{}
	add := func(heading string) {
		s := slug(heading)
		if n, ok := seen[s]; ok {
			seen[s] = n + 1
			s = fmt.Sprintf("%s-%d", s, n+1)
		} else {
			seen[s] = 0
		}
		out[s] = true
	}
	lines := prose(source)
	for i, line := range lines {
		if m := atxHeading.FindStringSubmatch(line); m != nil && strings.HasPrefix(strings.TrimSpace(line), "#") {
			add(strings.ReplaceAll(m[1], "`", ""))
		} else if i > 0 && setextLine.MatchString(line) && !notSetextText.MatchString(lines[i-1]) {
			add(strings.ReplaceAll(strings.TrimSpace(lines[i-1]), "`", ""))
		}
		for _, m := range htmlAnchor.FindAllStringSubmatch(line, -1) {
			out[m[1]] = true
		}
	}
	return out
}

// external matches a destination with a scheme, or a protocol-relative one.
var external = regexp.MustCompile(`^(?:[a-zA-Z][a-zA-Z0-9+.-]*:|//)`)

// splitTarget is a destination's path and fragment, the path unescaped.
func splitTarget(target string) (string, string, error) {
	path, fragment, _ := strings.Cut(target, "#")
	path, _, _ = strings.Cut(path, "?")
	unescaped, err := url.PathUnescape(path)
	return unescaped, fragment, err
}

// checkTarget reports what is wrong with a file and fragment a link names,
// or nothing: the file or directory must exist, inside within, and a fragment
// must be an anchor of the Markdown file it names.
func checkTarget(path, fragment, within string) error {
	rel, err := filepath.Rel(within, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s is outside %s", path, within)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("names %s, which does not exist", rel)
	}
	if fragment == "" {
		return nil
	}
	if info.IsDir() || !strings.HasSuffix(path, ".md") {
		return fmt.Errorf("names #%s in %s, which has no headings to anchor to", fragment, rel)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	if !anchors(string(data))[fragment] {
		return fmt.Errorf("names #%s, which no heading of %s renders", fragment, rel)
	}
	return nil
}

func TestSlugsFollowGitHub(t *testing.T) {
	// The cases the docs and crd-ref-docs actually produce. A slug rule that
	// drifted from GitHub's would pass every anchor it agreed with itself on
	// and break the page the reader lands on.
	for heading, want := range map[string]string{
		"Getting started":                      "getting-started",
		"The image needs the CLI":              "the-image-needs-the-cli",
		"crewlet.ai/v1alpha1":                  "crewletaiv1alpha1",
		"LLM ↔ tool proxy":                     "llm--tool-proxy",
		"What an agent's Slack app is":         "what-an-agents-slack-app-is",
		"spec.paused and `status.paused`":      "specpaused-and-statuspaused",
		"Seats, pools & [placement](x.md)":     "seats-pools--placement",
		"snake_case keys":                      "snake_case-keys",
		"Step 2: apply (server-side)":          "step-2-apply-server-side",
		"<a id=\"x\"></a>Anchored":             "anchored",
		"Données":                              "données",
		"A pool removed while seats still run": "a-pool-removed-while-seats-still-run",
	} {
		if got := slug(strings.ReplaceAll(heading, "`", "")); got != want {
			t.Errorf("slug(%q) = %q, want %q", heading, got, want)
		}
	}

	got := anchors("# Status\n\n## Status\n\n```sh\n# Status\n```\n\nStatus\n---\n")
	for _, want := range []string{"status", "status-1", "status-2"} {
		if !got[want] {
			t.Errorf("anchors are missing %q (a repeated heading is suffixed, a fenced one is not a heading): %v", want, got)
		}
	}
	if got["status-3"] {
		t.Errorf("a `# Status` comment inside a fence was read as a heading: %v", got)
	}
}

var (
	// listEntry is a list item the documentation site's navigation reads as an
	// entry: a dash at the very start of the line, then a bold link, whatever
	// follows it. The site matches it against the line as written, so an item
	// indented under another is no entry of its navigation.
	listEntry = regexp.MustCompile(`^-\s+\*\*\[[^\]]+\]\(([^)\s]+)\)\*\*`)
	// tableEntry is the other shape the navigation reads: a table row whose
	// first cell is one link, indented or not.
	tableEntry = regexp.MustCompile(`^\|\s*\[[^\]]+\]\(([^)\s]+)\)\s*\|`)
	// cardEntry is the entry the site also draws as a card on the index page:
	// `- **[Title](page.md)** — description`, with a space, an em dash and a
	// space between the link and the description. The site turns a list into
	// cards only when every item of it opens that way, and leaves the whole
	// list plain, saying nothing, when one does not.
	cardEntry = regexp.MustCompile(`^-\s+\*\*\[[^\]]+\]\([^)\s]+\)\*\* — \S`)
	// indexSection is a heading the site reads as a sidebar section.
	indexSection = regexp.MustCompile(`^##\s+\S`)
	// listMarker opens a list item, of a bullet or an ordered list.
	listMarker = regexp.MustCompile(`^ ?(?:[-*+]|\d{1,9}[.)])(?:[ \t]|$)`)
	// listCloser is a line that ends a list whatever came before it: a
	// heading, a thematic break or a block quote.
	listCloser = regexp.MustCompile(`^ {0,3}(?:#{1,6}(?:[ \t]|$)|(?:-[ \t]*){3,}$|(?:\*[ \t]*){3,}$|(?:_[ \t]*){3,}$|>)`)
)

// indentOf is the column a line's text starts at, a tab advancing to the
// next multiple of four as CommonMark counts it.
func indentOf(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4 - n%4
		default:
			return n
		}
	}
	return n
}

// itemWidth is the column a list item's content starts at: a line indented
// that far or further belongs to the item rather than ending the list.
func itemWidth(line string) int {
	m := listMarker.FindString(line)
	marker := len(strings.TrimRight(m, " \t"))
	gap := indentOf(line[marker:])
	if gap == 0 || gap > 4 || strings.TrimSpace(line[marker:]) == "" {
		return marker + 1
	}
	return marker + gap
}

// indexEntries returns the navigation entries of an index, in order, and a
// problem for each line the documentation site would refuse, draw other than
// as a card, or read other than as written:
//   - an entry above the first `## ` heading belongs to no sidebar section,
//     and the site fails its build on it rather than guess one;
//   - an entry that is not cardEntry, or that a table row stands for, is no
//     card, and neither is any entry of its list;
//   - a blank line between the items of a list, or between an entry and a
//     line indented under it, makes the list loose, and the site draws a
//     loose list as a plain list;
//   - a list item that is not an entry, beside the entries, keeps its list
//     plain, and the navigation does not read it;
//   - an entry indented under another is not read by the navigation at all;
//   - a link in a description cannot be clicked, since a card lays the link
//     of its title over its whole face;
//   - a description that goes on past its line is read by the navigation to
//     the end of the first only.
func indexEntries(source string) ([]link, []string) {
	var entries []link
	var refused []string
	refuse := func(line int, format string, args ...any) {
		refused = append(refused, fmt.Sprintf("%d: ", line)+fmt.Sprintf(format, args...))
	}
	inSection := false
	// item is the line of the list item the lines since belong to, or 0 when
	// no list is open; width is the column its content starts at, entry
	// whether it is an index entry that has not been refused for going on,
	// and blank whether a blank line came after it.
	item, width, entry, blank := 0, 0, false, false
	for i, line := range prose(source) {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			blank = item != 0
			continue
		}
		if indexSection.MatchString(line) {
			inSection = true
			item = 0
			continue
		}
		if m := tableEntry.FindStringSubmatch(trimmed); m != nil {
			if inSection {
				entries = append(entries, link{n, m[1]})
				refuse(n, "index entry %q is a table row, which the site's navigation reads but never draws as a card: "+
					"write it `- **[Title](page.md)** — description`", m[1])
			} else {
				refuse(n, "index entry %q is above the first `## ` heading, so it is in no section of the site's "+
					"sidebar; move it under the section it belongs to", m[1])
			}
			// A table is no part of the list above it.
			item = 0
			continue
		}
		if m := listEntry.FindStringSubmatch(line); m != nil {
			target := m[1]
			description := line[strings.Index(line, ")**")+len(")**"):]
			switch {
			case !inSection:
				refuse(n, "index entry %q is above the first `## ` heading, so it is in no section of the site's "+
					"sidebar; move it under the section it belongs to", target)
			case !cardEntry.MatchString(line):
				refuse(n, "index entry %q does not put ` — `, a space, an em dash and a space, between its link and "+
					"its description, so the site draws its list as a plain list instead of cards", target)
			case strings.Contains(description, "]("):
				refuse(n, "index entry %q has a link in its description, which nobody could click: a card lays the "+
					"link of its title over its whole face; name the page in words instead", target)
			case item != 0 && blank:
				refuse(n, "a blank line separates index entry %q from the item above it, which makes the list loose, "+
					"and the site draws a loose list as a plain list instead of cards", target)
			}
			if inSection {
				entries = append(entries, link{n, target})
			}
			item, width, entry, blank = n, itemWidth(line), true, false
			continue
		}
		if m := listEntry.FindStringSubmatch(trimmed); m != nil {
			refuse(n, "index entry %q is indented, and the site's navigation reads an entry only at the start of "+
				"its line, so the page is in no section of its sidebar; write it at the start of the line", m[1])
			blank = false
			continue
		}
		if item == 0 {
			continue
		}
		indented := indentOf(line) >= width
		switch {
		case !indented && listCloser.MatchString(line):
			item = 0
		case !indented && listMarker.MatchString(line):
			refuse(n, "a list item that is not an index entry follows the entries above it: the site's navigation "+
				"does not read it, and the site draws the list it is in as a plain list instead of cards")
			item, width, entry, blank = n, itemWidth(line), false, false
		case indented && blank:
			refuse(n, "a blank line and then a line indented under the item on line %d make the list loose, and "+
				"the site draws a loose list as a plain list instead of cards", item)
			entry, blank = false, false
		case indented || !blank:
			if entry {
				refuse(n, "the entry on line %d goes on here, but an entry is one line: the site's navigation reads "+
					"its description to the end of that line only", item)
			}
			entry = false
		default:
			// Text after a blank line, not indented: the list has ended.
			item = 0
		}
	}
	return entries, refused
}

func TestEveryPageIsInTheIndex(t *testing.T) {
	root := repoRoot(t)
	docsDir := filepath.Join(root, "docs")
	index := filepath.Join(docsDir, "README.md")
	data, err := os.ReadFile(filepath.Clean(index))
	if err != nil {
		t.Fatal(err)
	}
	// The site serves the index as the section's home, index.md, whatever it is
	// called, and refuses a tree that has both.
	if _, err := os.Lstat(filepath.Join(docsDir, "index.md")); err == nil {
		t.Errorf("docs/index.md exists beside docs/README.md, and the documentation site would serve both " +
			"as the home page; docs/README.md is the index, so fold docs/index.md into it")
	}

	entries, refused := indexEntries(string(data))
	for _, problem := range refused {
		t.Errorf("docs/README.md:%s", problem)
	}
	listed := map[string]int{}
	for _, entry := range entries {
		page, fragment, err := splitTarget(entry.target)
		switch {
		case err != nil || external.MatchString(entry.target):
			t.Errorf("docs/README.md:%d: index entry %q is not a page of docs/", entry.line, entry.target)
			continue
		case fragment != "" || !strings.HasSuffix(page, ".md"):
			t.Errorf("docs/README.md:%d: index entry %q names no page: an entry is a whole .md file", entry.line, entry.target)
			continue
		}
		page = filepath.ToSlash(filepath.Clean(page))
		if strings.HasPrefix(page, "../") {
			t.Errorf("docs/README.md:%d: index entry %q is outside docs/", entry.line, entry.target)
			continue
		}
		listed[page]++
		if _, err := os.Stat(filepath.Join(docsDir, page)); err != nil {
			t.Errorf("docs/README.md:%d: index entry %q names a page that does not exist", entry.line, entry.target)
		}
	}

	err = filepath.WalkDir(docsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			// The site publishes regular files only, and fails its build on
			// anything else: a symbolic link here would otherwise be read as
			// whatever it names on the machine building the site.
			rel, err := filepath.Rel(docsDir, path)
			if err != nil {
				return err
			}
			kind := "not a regular file"
			if d.Type()&fs.ModeSymlink != 0 {
				kind = "a symbolic link"
			}
			t.Errorf("docs/%s is %s, which the documentation site refuses; commit a copy of what it points at",
				filepath.ToSlash(rel), kind)
			return nil
		}
		// assets/README.md describes the figures to whoever draws them; the
		// site publishes the directory without it, as no page.
		if !strings.HasSuffix(path, ".md") || path == index || path == filepath.Join(docsDir, "assets", "README.md") {
			return nil
		}
		rel, err := filepath.Rel(docsDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch listed[rel] {
		case 0:
			t.Errorf("docs/%s is not listed in docs/README.md: add `- **[Title](%s)** — what it covers` "+
				"under the section it belongs to, or the documentation site has no way to reach it", rel, rel)
		case 1:
		default:
			t.Errorf("docs/%s is listed %d times in docs/README.md; the navigation lists a page once", rel, listed[rel])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRelativeLinksResolve(t *testing.T) {
	root := repoRoot(t)
	for _, file := range markdownFiles(t, root) {
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatal(err)
		}
		name, err := filepath.Rel(root, file)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range links(string(data)) {
			if l.target == "" || external.MatchString(l.target) {
				continue
			}
			path, fragment, err := splitTarget(l.target)
			if err != nil {
				t.Errorf("%s:%d: %q: %v", name, l.line, l.target, err)
				continue
			}
			target := file
			if path != "" {
				if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
					t.Errorf("%s:%d: %q is an absolute path, which resolves against the site serving the "+
						"page rather than this repository; write it relative to %s", name, l.line, l.target, name)
					continue
				}
				target = filepath.Join(filepath.Dir(file), filepath.FromSlash(path))
			}
			if err := checkTarget(target, fragment, root); err != nil {
				t.Errorf("%s:%d: %q %v", name, l.line, l.target, err)
			}
		}
	}
}

// engineLink is a link to a file of the engine's repository on GitHub: a page
// of its documentation, which the documentation site serves as its own page
// of the engine's main, or another file, such as its RELEASING.md, which the
// site leaves a link to GitHub.
var engineLink = regexp.MustCompile(`^https://` + regexp.QuoteMeta(enginetest.Module) +
	`/(?:blob|tree)/([^/#?]+)(/[^#?]*)?(?:#(.*))?$`)

// hasIndexPage reports whether a directory has the page the documentation
// site serves a link to the directory as: its index.md, or its README.md.
func hasIndexPage(dir string) bool {
	for _, name := range []string{"index.md", "README.md"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func TestEngineLinksResolve(t *testing.T) {
	// The operator documents what the engine does by linking the engine's own
	// pages rather than restating them, and the engine renames its pages as it
	// moves. Checked against the source of the engine every suite runs,
	// crewlet's main, so an engine change that removes or renames a page or a
	// heading the docs point at fails here, on the operator's next run.
	root := repoRoot(t)
	var engineRoot string
	for _, file := range markdownFiles(t, root) {
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatal(err)
		}
		name, err := filepath.Rel(root, file)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range links(string(data)) {
			m := engineLink.FindStringSubmatch(l.target)
			if m == nil {
				continue
			}
			ref, path, fragment := m[1], m[2], m[3]
			if ref != "main" {
				// main is the engine every suite runs and the one the site
				// serves as the engine's unreleased documentation; a link at
				// another ref is checked against nothing and rendered as
				// nothing.
				t.Errorf("%s:%d: %q links the engine at %q; write it at main", name, l.line, l.target, ref)
				continue
			}
			if engineRoot == "" {
				engineRoot = enginetest.Source(t)
			}
			unescaped, err := url.PathUnescape(path)
			if err != nil {
				t.Errorf("%s:%d: %q: %v", name, l.line, l.target, err)
				continue
			}
			unescaped = strings.TrimPrefix(unescaped, "/")
			target := filepath.Join(engineRoot, filepath.FromSlash(unescaped))
			inDocs := strings.HasPrefix(unescaped+"/", "docs/")
			if info, err := os.Stat(target); inDocs && err == nil && info.IsDir() && !hasIndexPage(target) {
				// The site serves a directory only as its index page, as
				// GitHub shows one under the listing; a directory without one
				// stays a GitHub link there and warns on every build. The
				// engine's index lists each directory's pages under a heading:
				// link that section instead.
				t.Errorf("%s:%d: %q names a directory of the engine's docs with no index page, which the "+
					"documentation site cannot serve; link a page, such as docs/index.md#<section>", name, l.line, l.target)
				continue
			}
			if err := checkTarget(target, fragment, engineRoot); err != nil {
				t.Errorf("%s:%d: %q, in the engine at %s, %v", name, l.line, l.target, enginetest.Version(t), err)
			}
		}
	}
}

func TestTheGateCanFail(t *testing.T) {
	// Each check above is only as good as its parser: a links() that found
	// nothing would pass every tree. So the parser is held to a document with
	// one of everything, and the checks to targets that are wrong.
	doc := strings.Join([]string{
		"# Title",
		"",
		"See [a](a.md), [b](<b c.md> \"t\"), ![img](img.svg) and <a href=\"h.md\">h</a>.",
		"Not `[code](code.md)`, and not this:",
		"",
		"```md",
		"[fenced](fenced.md)",
		"```",
		"",
		"[ref]: ref.md",
	}, "\n")
	found := links(doc)
	got := make([]string, 0, len(found))
	for _, l := range found {
		got = append(got, l.target)
	}
	if want := []string{"a.md", "b c.md", "img.svg", "h.md", "ref.md"}; !slices.Equal(got, want) {
		t.Errorf("links() = %q, want %q", got, want)
	}

	dir := t.TempDir()
	page := filepath.Join(dir, "page.md")
	if err := os.WriteFile(page, []byte("# Page\n\n## Section\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ path, fragment string }{
		{filepath.Join(dir, "missing.md"), ""},
		{page, "no-such-section"},
		{dir, "section"},
		{filepath.Join(dir, "..", "outside.md"), ""},
	} {
		if err := checkTarget(c.path, c.fragment, dir); err == nil {
			t.Errorf("checkTarget(%s, #%s) passed", c.path, c.fragment)
		}
	}
	if err := checkTarget(page, "section", dir); err != nil {
		t.Errorf("checkTarget of a real heading failed: %v", err)
	}
	if hasIndexPage(dir) {
		t.Errorf("hasIndexPage(%s) is true for a directory holding only page.md", dir)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Listing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hasIndexPage(dir) {
		t.Errorf("hasIndexPage(%s) is false for a directory with a README.md", dir)
	}
	index := []string{
		"# Docs",
		"",
		"- **[Stray](stray.md)** — above every section",
		"",
		"```md",
		"## Not a heading",
		"```",
		"| [Fenced](fenced.md) | still above every section |",
		"",
		"## Guides",
		"",
		"- **[Guide](guide.md)** — a card",
		"",
		"- **[Loose](loose.md)** — after a blank line",
		"- **[Colon](colon.md)**: a colon, which the navigation reads and no card does",
		"- **[Hyphen](hyphen.md)** - a hyphen",
		"- **[Linked](linked.md)** — a description with [a link](elsewhere.md)",
		"- **[Wrapped](wrapped.md)** — a description",
		"  that goes on",
		"- More pages are coming",
		"  - **[Nested](nested.md)** — under an item, where the navigation does not look",
		"| [Row](row.md) | a table row |",
		"",
		"- **[After](after.md)** — a card after a table, in a list of its own",
		"",
		"## Reference",
		"",
		"- **[Paragraph](paragraph.md)** — a card",
		"",
		"  with a second paragraph under it",
		"",
		"Prose naming **[a page](prose.md)** is no entry, and ends the list.",
	}
	entries, refused := indexEntries(strings.Join(index, "\n"))
	targets := make([]string, 0, len(entries))
	for _, e := range entries {
		targets = append(targets, e.target)
	}
	want := []string{
		"guide.md", "loose.md", "colon.md", "hyphen.md", "linked.md", "wrapped.md", "row.md", "after.md", "paragraph.md",
	}
	if !slices.Equal(targets, want) {
		t.Errorf("indexEntries() = %v, want %v", targets, want)
	}
	// Each line below is refused, once, for the reason given; every other line
	// is not, the cards and the prose among them.
	refusals := []struct{ line, reason string }{
		{"- **[Stray](stray.md)** — above every section", "above the first `## ` heading"},
		{"| [Fenced](fenced.md) | still above every section |", "above the first `## ` heading"},
		{"- **[Loose](loose.md)** — after a blank line", "makes the list loose"},
		{"- **[Colon](colon.md)**: a colon, which the navigation reads and no card does", "an em dash"},
		{"- **[Hyphen](hyphen.md)** - a hyphen", "an em dash"},
		{"- **[Linked](linked.md)** — a description with [a link](elsewhere.md)", "nobody could click"},
		{"  that goes on", "an entry is one line"},
		{"- More pages are coming", "not an index entry"},
		{"  - **[Nested](nested.md)** — under an item, where the navigation does not look", "is indented"},
		{"| [Row](row.md) | a table row |", "table row"},
		{"  with a second paragraph under it", "make the list loose"},
	}
	for _, r := range refusals {
		line := slices.Index(index, r.line) + 1
		prefix := fmt.Sprintf("%d: ", line)
		if line == 0 || !slices.ContainsFunc(refused, func(got string) bool {
			return strings.HasPrefix(got, prefix) && strings.Contains(got, r.reason)
		}) {
			t.Errorf("indexEntries() did not refuse line %d, %q, saying %q: %q", line, r.line, r.reason, refused)
		}
	}
	if len(refused) != len(refusals) {
		t.Errorf("indexEntries() refused %d lines, want the %d above and no other: %q", len(refused), len(refusals), refused)
	}

	for _, l := range []string{
		"https://github.com/crewlet/crewlet/blob/main/docs/concepts/configuration.md#managed-configuration",
		"https://github.com/crewlet/crewlet/blob/main/RELEASING.md#images-from-main",
	} {
		if !engineLink.MatchString(l) {
			t.Errorf("engineLink does not match %s", l)
		}
	}
	if engineLink.MatchString("https://github.com/crewlet/crewlet-operator/blob/main/docs/README.md") {
		t.Error("engineLink matches the wrong repository")
	}
}

// siteOrigin is the documentation site, whose absolute links are held to the
// paths it builds.
const siteOrigin = "https://docs.crewlet.ai"

// siteVersioned is a path under a product and a version segment: the only
// paths the site builds besides its own home. main is next/, the newest
// release latest/, an older release line vX.Y/.
var siteVersioned = regexp.MustCompile(`^/(engine|operator)/(latest|next|v[0-9]+\.[0-9]+)/(.*)$`)

// engineRawTrees are the engine's directories outside docs/ the site publishes
// file for file beside each version's pages: its JSON Schemas and its agent
// skill.
var engineRawTrees = []string{"schema/", "skills/"}

// siteLinkProblem reports why an absolute link into the documentation site
// names nothing the site builds, or "" when it names something. operatorDocs
// is this tree's docs/; engineRoot returns the source of the engine every
// suite runs, and is called only for a link into the engine's latest/ or
// next/.
//
// A page is docs/<page>.md served as <page>/ (the index, README.md here and
// index.md in the engine, as the version's home), and every page is a
// directory, so a page without its trailing slash is a redirect on every
// click. The engine's schema/ and skills/ are served beside its pages, under
// the same version. An older line (vX.Y/) is not in either tree and is held to
// its shape alone.
//
// latest/ is checked against the same tree as next/, and that is sound only
// because of two rules the site keeps:
//
//   - latest/ is main or a release cut from the same repository's main,
//     never another program. Before a product's first release the site
//     answers latest/ with a redirect to next/; from then it is the newest
//     release. The engine's one older tag, v0.1.0, predates the main every
//     suite runs, and the site never serves it at latest/.
//   - what the release lacks, latest/ redirects to next/: a page, schema or
//     skill file only main has answers at its latest/ path with a redirect
//     to its next/ copy.
//
// So a link to a page or file main has reaches main's copy or a release's.
// What this cannot see is a release's copy of a page main has since changed,
// which is why a #fragment into the site is not checked here.
func siteLinkProblem(target, operatorDocs string, engineRoot func() string) string {
	path, _, _ := strings.Cut(strings.TrimPrefix(target, siteOrigin), "#")
	path, _, _ = strings.Cut(path, "?")
	// A full stop ending a sentence is prose, not part of the path.
	path = strings.TrimRight(path, ".")
	if path == "" || path == "/" {
		return "" // the site's home
	}
	parts := siteVersioned.FindStringSubmatch(path)
	if parts == nil {
		return "is not under /operator/ or /engine/ and a version (latest, next or vX.Y), where the site " +
			"builds nothing; link this tree's pages under /operator/latest/"
	}
	product, version, rest := parts[1], parts[2], parts[3]
	if strings.HasPrefix(version, "v") {
		return ""
	}
	last := rest[strings.LastIndex(rest, "/")+1:]
	if last != "" && !strings.Contains(last, ".") {
		return "names a page without its trailing slash, which the site answers with a redirect"
	}
	if product == "engine" {
		root := engineRoot()
		for _, tree := range engineRawTrees {
			if strings.HasPrefix(rest, tree) {
				if !isRegularIn(root, rest) {
					return fmt.Sprintf("names %s, which is not in the engine's %s", rest, tree)
				}
				return ""
			}
		}
	}
	docs, index := operatorDocs, "README.md"
	if product == "engine" {
		docs, index = filepath.Join(engineRoot(), "docs"), "index.md"
	}
	file := index
	switch page := strings.TrimSuffix(rest, "/"); {
	case page == "":
	case strings.Contains(last, "."):
		file = page
	default:
		file = page + ".md"
	}
	if !isRegularIn(docs, file) {
		return fmt.Sprintf("names %s, which is not in the %s's docs/", file, product)
	}
	return ""
}

// isRegularIn reports whether the slash-separated path rel names a regular
// file under dir. It is read through dir as an os.Root, so a link's path
// (which may hold "..") cannot reach a file outside the tree it names.
func isRegularIn(dir, rel string) bool {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	info, err := root.Stat(filepath.FromSlash(rel))
	return err == nil && info.Mode().IsRegular()
}

// siteLink matches an absolute link into the documentation site in any text,
// and captures its path. The class stops at '#', '?', ')', a quote and
// whitespace, so a fragment, a query, a Markdown paren and the end of a string
// literal stay outside it; a '.' is in it, because a file has an extension,
// and siteLinkProblem tells one from a full stop.
var siteLink = regexp.MustCompile(regexp.QuoteMeta(siteOrigin) + `(/[A-Za-z0-9/._-]*)?`)

// binaryProbe is how much of a file is read to decide it is not text: git's
// own figure for the same decision, a NUL byte in the first eight thousand.
const binaryProbe = 8000

// textFiles is every text file of this module: Go, YAML, Markdown, the chart's
// templates and NOTES.txt, the workflows, the Makefile. bin/ (the pinned tools)
// is not this repository's, and neither is a nested checkout (a directory
// holding a .git of its own), which is another commit of this tree.
func textFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	// Read through the module as an os.Root, so nothing the walk names can
	// lead a read outside it.
	tree, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tree.Close() }()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path == filepath.Join(root, "bin") {
				return filepath.SkipDir
			}
			if path != root {
				if _, err := tree.Lstat(filepath.Join(relative, ".git")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		head := make([]byte, binaryProbe)
		file, err := tree.Open(relative)
		if err != nil {
			return err
		}
		n, readErr := io.ReadFull(file, head)
		if closeErr := file.Close(); closeErr != nil {
			return closeErr
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return readErr
		}
		if !bytes.Contains(head[:n], []byte{0}) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSiteLinksNameABuiltPage(t *testing.T) {
	// The site moved every product under its own path and a version, and
	// nothing forwards a path it no longer builds: /operator/<page>/, the
	// shape the operator's pages had before, is a 404, and so is a page
	// renamed under a link that still names it.
	//
	// Every text file, not the Markdown alone: a link the chart's NOTES.txt
	// prints after an install, a Go doc comment or a sample's annotation is
	// handed out as surely as one in a page.
	root := repoRoot(t)
	operatorDocs := filepath.Join(root, "docs")
	var engineRoot string
	engine := func() string {
		if engineRoot == "" {
			engineRoot = enginetest.Source(t)
		}
		return engineRoot
	}
	files := textFiles(t, root)
	// A walk that read almost nothing certified almost nothing: a root
	// resolved to the wrong directory finds no link and passes. The module
	// holds several hundred text files.
	if len(files) < 200 {
		t.Fatalf("read %d text files under %s, which is not this module", len(files), root)
	}
	checked := 0
	for _, file := range files {
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatal(err)
		}
		name, err := filepath.Rel(root, file)
		if err != nil {
			t.Fatal(err)
		}
		for number, line := range strings.Split(string(data), "\n") {
			for _, match := range siteLink.FindAllStringSubmatch(line, -1) {
				checked++
				if problem := siteLinkProblem(match[0], operatorDocs, engine); problem != "" {
					t.Errorf("%s:%d: %q %s", name, number+1, match[0], problem)
				}
			}
		}
	}
	// README.md and CONTRIBUTING.md each link the site: a reader that found
	// neither would pass every tree.
	if checked < 2 {
		t.Errorf("found %d links to %s in this tree, want at least the README's and "+
			"CONTRIBUTING.md's", checked, siteOrigin)
	}
}

func TestSiteLinkProblemKnowsWhatTheSiteBuilds(t *testing.T) {
	operator := t.TempDir()
	engine := t.TempDir()
	for _, file := range []string{
		filepath.Join(operator, "README.md"),
		filepath.Join(operator, "guides", "installing.md"),
		filepath.Join(engine, "docs", "index.md"),
		filepath.Join(engine, "docs", "concepts", "overview.md"),
		filepath.Join(engine, "schema", "company.schema.json"),
		filepath.Join(engine, "skills", "company-architect", "SKILL.md"),
	} {
		if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("# Page\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	engineRoot := func() string { return engine }
	for _, c := range []struct {
		target string
		built  bool
	}{
		{siteOrigin, true},
		{siteOrigin + "/", true},
		{siteOrigin + "/operator/latest/", true},
		{siteOrigin + "/operator/next/", true},
		{siteOrigin + "/operator/latest/guides/installing/", true},
		{siteOrigin + "/operator/next/guides/installing/#section", true},
		{siteOrigin + "/operator/v0.3/anything/", true},
		{siteOrigin + "/engine/latest/", true},
		{siteOrigin + "/engine/next/concepts/overview/", true},
		{siteOrigin + "/engine/latest/concepts/overview/.", true},
		// The engine's schemas and skills are served beside its pages.
		{siteOrigin + "/engine/latest/schema/company.schema.json", true},
		{siteOrigin + "/engine/next/skills/company-architect/SKILL.md", true},

		{siteOrigin + "/operator/", false},
		{siteOrigin + "/operator", false},
		{siteOrigin + "/operator/guides/installing/", false},
		{siteOrigin + "/next/guides/installing/", false},
		{siteOrigin + "/concepts/overview/", false},
		{siteOrigin + "/operator/latest/guides/installing", false},
		{siteOrigin + "/operator/latest/guides/no-such-page/", false},
		{siteOrigin + "/engine/latest/concepts/no-such-page/", false},
		{siteOrigin + "/engine/latest/schema/no-such.schema.json", false},
		{siteOrigin + "/engine/latest/skills/no-such/SKILL.md", false},
		// Not under docs/: the raw trees are the engine's, at its root.
		{siteOrigin + "/engine/latest/concepts/company.schema.json", false},
		{siteOrigin + "/operator/latest/schema/company.schema.json", false},
		{siteOrigin + "/operator/latest/guides/installing.", false},
	} {
		problem := siteLinkProblem(c.target, operator, engineRoot)
		if (problem == "") != c.built {
			t.Errorf("siteLinkProblem(%q) = %q, want built=%v", c.target, problem, c.built)
		}
	}
}

// yamlFence is the body of a ```yaml block of a Markdown file.
var yamlFence = regexp.MustCompile("(?s)```yaml\n(.*?)\n```\n")

func TestTheReadmeShowsTheGuidesObjects(t *testing.T) {
	// The README shows what a company looks like with objects the
	// getting-started guide applies, which test/crd applies to an API server
	// and the render contract test hands to the engine, so the README's
	// excerpt is one the API and the engine accept rather than a copy of it
	// that drifts. Every object it shows is one of the guide's, byte for byte.
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Clean(filepath.Join(root, "README.md")))
	if err != nil {
		t.Fatal(err)
	}
	manifests, err := Manifests(filepath.Join(root, "docs", "getting-started.md"))
	if err != nil {
		t.Fatal(err)
	}
	applied := map[string]bool{}
	for _, manifest := range manifests {
		for object := range strings.SplitSeq(manifest, "\n---\n") {
			applied[strings.TrimSpace(object)] = true
		}
	}
	blocks := yamlFence.FindAllSubmatch(readme, -1)
	if len(blocks) == 0 {
		t.Fatal("README.md shows no ```yaml block: the test reads it wrong, or the README changed shape")
	}
	for _, block := range blocks {
		for object := range strings.SplitSeq(string(block[1]), "\n---\n") {
			if !applied[strings.TrimSpace(object)] {
				t.Errorf("README.md shows an object docs/getting-started.md does not apply; copy it from the guide:\n%s", object)
			}
		}
	}
}
