package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// yamlOf is the YAML of a JSON document, as Crewlet reads it back: every
// document the operator hands the engine (a data or seat pod's crewlet.yaml,
// the company document) is encoded to JSON first and written as YAML here.
//
// Crewlet reads both with gopkg.in/yaml.v3, a YAML 1.2 parser, so they are
// written by the same family's encoder (go.yaml.in/yaml/v3, its maintained
// continuation). The YAML 1.1 encoder that wrote them before
// (sigs.k8s.io/yaml's JSONToYAML, go-yaml v2) is the wrong half of a pair:
// YAML 1.1 reads U+0085 (NEL) as a line break and YAML 1.2 does not, so a
// NEL in a string reached the engine as a space (an address of a Company's
// that the engine then refused to start on), and every other C1 control
// character could not be written at all, so the document failed to render.
//
// Two choices keep the encoder from changing a string on its way:
//
//   - The document is built from the JSON's tokens rather than by decoding it
//     as YAML, because a YAML reader refuses a C1 control character in its
//     input before any escape is read, and a JSON string holds one raw.
//   - A string holding a line break is written double-quoted, with every
//     break escaped. The encoder would otherwise write it as a literal block,
//     which cannot hold every string: a block whose string opens with a tab
//     before its first break is refused by the parser, and so is the whole
//     document. Every other string is written in the style the encoder
//     chooses, which TestYAMLWritesEveryStringAsGiven and
//     TestContractYAMLReachesTheEngineAsWritten hold it to.
//
// Keys are written sorted, as encoding/json writes a map's and as the render
// has always written them, so a document's bytes, and the hash a data pod is
// restarted on, follow its content alone. Numbers keep the value they have in
// the JSON: an integer is written as one, and any other number as the
// shortest float that reads back to it.
func yamlOf(encoded []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.UseNumber()
	node, err := yamlNodeOf(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("more than one JSON value: %w", err)
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	// A list's items at its key's own indentation, as the render has always
	// written them.
	enc.CompactSeqIndent()
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// yamlNodeOf reads the next JSON value off dec as a YAML node.
func yamlNodeOf(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return yamlMappingOf(dec)
		case '[':
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for dec.More() {
				item, err := yamlNodeOf(dec)
				if err != nil {
					return nil, err
				}
				seq.Content = append(seq.Content, item)
			}
			_, err := dec.Token()
			return seq, err
		}
	case string:
		return yamlString(t), nil
	case json.Number:
		return yamlNumber(t)
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	return nil, fmt.Errorf("unexpected JSON token %v", tok)
}

// yamlMappingOf reads the rest of a JSON object off dec, its '{' read, as a
// YAML mapping whose keys are sorted.
func yamlMappingOf(dec *json.Decoder) (*yaml.Node, error) {
	type entry struct {
		key   string
		value *yaml.Node
	}
	var entries []entry
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected JSON object key %v", tok)
		}
		value, err := yamlNodeOf(dec)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{key: key, value: value})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	slices.SortStableFunc(entries, func(a, b entry) int { return strings.Compare(a.key, b.key) })
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, e := range entries {
		mapping.Content = append(mapping.Content, yamlString(e.key), e.value)
	}
	return mapping, nil
}

// yamlString is a string's node. The encoder chooses its style, but for a
// string holding a line break, which is written double-quoted (yamlOf says
// why).
func yamlString(s string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	if strings.ContainsAny(s, "\n\r\u0085\u2028\u2029") {
		node.Style = yaml.DoubleQuotedStyle
	}
	return node
}

// yamlNumber is a JSON number's node: an integer as it is, any other number
// as the shortest float that reads back to it, untagged, so that it is
// written plain and read as the number it is.
func yamlNumber(n json.Number) (*yaml.Node, error) {
	if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(i, 10)}, nil
	}
	if u, err := strconv.ParseUint(n.String(), 10, 64); err == nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(u, 10)}, nil
	}
	f, err := n.Float64()
	if err != nil {
		return nil, fmt.Errorf("JSON number %s: %w", n, err)
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatFloat(f, 'g', -1, 64)}, nil
}
