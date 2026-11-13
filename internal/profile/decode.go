package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"unicode/utf8"
)

// MaxSize is the largest configuration file, in bytes, that agent-id reads.
const MaxSize = 64 << 10

// maxDepth is the deepest nesting of objects and arrays a document may have.
const maxDepth = 64

// kind is the JSON type of a node.
type kind int

const (
	kindObject kind = iota
	kindArray
	kindString
	kindNumber
	kindBool
	kindNull
)

// node is one JSON value of a document. Objects keep every member in
// document order, duplicates included, so that the duplicate check and the
// inline-secret scan see the document exactly as written. text holds a
// string's value or a number's literal.
type node struct {
	kind    kind
	members []member
	elems   []*node
	text    string
	boolean bool
}

// member is one name and value of an object.
type member struct {
	name  string
	value *node
}

// get returns the value of the first member named name, or nil.
func (n *node) get(name string) *node {
	for _, m := range n.members {
		if m.name == name {
			return m.value
		}
	}
	return nil
}

// decode parses data as exactly one JSON value of at most MaxSize bytes in
// valid UTF-8. It rejects syntax errors, trailing data, and nesting deeper
// than maxDepth, and keeps duplicate members for the later checks.
func decode(data []byte) (*node, error) {
	if len(data) > MaxSize {
		return nil, Errorf(ErrConfigInvalid, "larger than 64 KiB")
	}
	if !utf8.Valid(data) {
		line, column := position(data, invalidUTF8Offset(data))
		return nil, Errorf(ErrConfigInvalid, "not valid UTF-8 at line %d, column %d", line, column)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	root, err := readValue(dec, 0)
	if err != nil {
		return nil, syntaxError(data, dec, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		line, column := position(data, int(dec.InputOffset())-1)
		return nil, Errorf(ErrConfigInvalid, "not valid JSON at line %d, column %d: data after the top-level value", line, column)
	}
	return root, nil
}

// errTooDeep reports nesting beyond maxDepth.
var errTooDeep = errors.New("nested too deeply")

// readValue reads the next JSON value from dec.
func readValue(dec *json.Decoder, depth int) (*node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		if depth == maxDepth {
			return nil, errTooDeep
		}
		if t == '{' {
			return readObject(dec, depth+1)
		}
		return readArray(dec, depth+1)
	case string:
		return &node{kind: kindString, text: t}, nil
	case json.Number:
		return &node{kind: kindNumber, text: t.String()}, nil
	case bool:
		return &node{kind: kindBool, boolean: t}, nil
	default:
		return &node{kind: kindNull}, nil
	}
}

// readObject reads the members of an object whose opening brace dec has
// consumed, and its closing brace.
func readObject(dec *json.Decoder, depth int) (*node, error) {
	n := &node{kind: kindObject}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, _ := tok.(string)
		value, err := readValue(dec, depth)
		if err != nil {
			return nil, err
		}
		n.members = append(n.members, member{name: name, value: value})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return n, nil
}

// readArray reads the elements of an array whose opening bracket dec has
// consumed, and its closing bracket.
func readArray(dec *json.Decoder, depth int) (*node, error) {
	n := &node{kind: kindArray}
	for dec.More() {
		value, err := readValue(dec, depth)
		if err != nil {
			return nil, err
		}
		n.elems = append(n.elems, value)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return n, nil
}

// syntaxError turns a decoder error into the user-facing error with a line
// and column.
func syntaxError(data []byte, dec *json.Decoder, err error) error {
	if errors.Is(err, errTooDeep) {
		return Errorf(ErrConfigInvalid, "nested deeper than %d levels", maxDepth)
	}
	offset := int(dec.InputOffset())
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		offset = int(syntax.Offset) - 1
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		offset = len(data)
	}
	line, column := position(data, offset)
	return Errorf(ErrConfigInvalid, "not valid JSON at line %d, column %d", line, column)
}

// position returns the 1-based line and column, in characters, of the byte
// at offset in data.
func position(data []byte, offset int) (line, column int) {
	offset = min(max(offset, 0), len(data))
	line, column = 1, 1
	for _, r := range string(data[:offset]) {
		if r == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}

// invalidUTF8Offset returns the offset of the first byte of data that is not
// valid UTF-8.
func invalidUTF8Offset(data []byte) int {
	offset := 0
	for offset < len(data) {
		r, size := utf8.DecodeRune(data[offset:])
		if r == utf8.RuneError && size <= 1 {
			return offset
		}
		offset += size
	}
	return offset
}

// encode appends the compact JSON form of n to buf. Members keep their
// order, and numbers keep their literal.
func encode(buf *bytes.Buffer, n *node) {
	switch n.kind {
	case kindObject:
		buf.WriteByte('{')
		for i, m := range n.members {
			if i > 0 {
				buf.WriteByte(',')
			}
			encodeString(buf, m.name)
			buf.WriteByte(':')
			encode(buf, m.value)
		}
		buf.WriteByte('}')
	case kindArray:
		buf.WriteByte('[')
		for i, e := range n.elems {
			if i > 0 {
				buf.WriteByte(',')
			}
			encode(buf, e)
		}
		buf.WriteByte(']')
	case kindString:
		encodeString(buf, n.text)
	case kindNumber:
		buf.WriteString(n.text)
	case kindBool:
		buf.WriteString(strconv.FormatBool(n.boolean))
	default:
		buf.WriteString("null")
	}
}

// encodeString appends s as a JSON string without HTML escaping.
func encodeString(buf *bytes.Buffer, s string) {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	buf.Truncate(buf.Len() - 1)
}

// raw returns the compact JSON form of n.
func raw(n *node) json.RawMessage {
	var buf bytes.Buffer
	encode(&buf, n)
	return buf.Bytes()
}
