// Package strictjson implements the strict JSON value tree shared by the
// versioned protocol records. Both the storage manifest (architecture
// section 9.2) and the workspace private-state record (architecture section
// 7.2) require rejection of unknown fields, duplicate field names, missing
// required fields, and explicit null at every object level, with integer
// fields decoded as unquoted uint64 values. The generic tree is built
// before any protocol value is decoded, so duplicate-name rejection always
// precedes value decoding. A package owns the semantic validation on top of
// this tree; strictjson only guarantees the strict shape.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Kind identifies one JSON value kind in the tree.
type Kind uint8

// The value kinds the parser can produce. Null, booleans beyond the Bool
// kind, and numbers outside the uint64 range never appear in a tree.
const (
	Object Kind = iota
	Array
	String
	Number
	Bool
)

// Value is a validated JSON value tree. Objects keep their fields in source
// order with unique names; numbers are already decoded as uint64; null can
// never appear.
type Value struct {
	Kind Kind
	Str  string
	Num  uint64
	B    bool
	Obj  []Field
	Arr  []Value
}

// Field is one named field of an object value.
type Field struct {
	Name  string
	Value Value
}

// Parse parses data into a Value tree. It rejects malformed JSON,
// duplicate object field names, explicit null, and any number that is not
// an unquoted unsigned 64-bit integer. Trailing data after the top-level
// value is rejected.
func Parse(data []byte) (Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return Value{}, fmt.Errorf("strictjson: parse: %w", err)
	}
	root, err := valueFromToken(dec, tok)
	if err != nil {
		return Value{}, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return Value{}, errors.New("strictjson: parse: trailing data")
		}
		return Value{}, fmt.Errorf("strictjson: parse: %w", err)
	}
	return root, nil
}

func valueFromToken(dec *json.Decoder, tok json.Token) (Value, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return parseObject(dec)
		case '[':
			return parseArray(dec)
		default:
			return Value{}, fmt.Errorf("strictjson: parse: unexpected delimiter %q", t)
		}
	case string:
		return Value{Kind: String, Str: t}, nil
	case json.Number:
		n, err := parseUint64(t.String())
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: Number, Num: n}, nil
	case bool:
		return Value{Kind: Bool, B: t}, nil
	case nil:
		return Value{}, errors.New("strictjson: parse: explicit null is not allowed")
	default:
		return Value{}, fmt.Errorf("strictjson: parse: unexpected token %T", tok)
	}
}

// parseUint64 decodes an unquoted JSON integer. Negative values, fractions,
// exponents, and values beyond uint64 are rejected.
func parseUint64(s string) (uint64, error) {
	if s == "" || s[0] == '-' || strings.ContainsAny(s, ".eE") {
		return 0, fmt.Errorf("strictjson: parse: %q is not an unsigned integer", s)
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("strictjson: parse: %q is not an unsigned integer", s)
	}
	return n, nil
}

func parseObject(dec *json.Decoder) (Value, error) {
	var fields []Field
	seen := make(map[string]bool)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return Value{}, fmt.Errorf("strictjson: parse: %w", err)
		}
		name, ok := tok.(string)
		if !ok {
			return Value{}, errors.New("strictjson: parse: object field name is not a string")
		}
		if seen[name] {
			return Value{}, fmt.Errorf("strictjson: parse: duplicate field %q", name)
		}
		seen[name] = true
		vtok, err := dec.Token()
		if err != nil {
			return Value{}, fmt.Errorf("strictjson: parse: field %q: %w", name, err)
		}
		val, err := valueFromToken(dec, vtok)
		if err != nil {
			return Value{}, fmt.Errorf("strictjson: parse: field %q: %w", name, err)
		}
		fields = append(fields, Field{Name: name, Value: val})
	}
	if _, err := expectDelim(dec, '}'); err != nil {
		return Value{}, err
	}
	return Value{Kind: Object, Obj: fields}, nil
}

func parseArray(dec *json.Decoder) (Value, error) {
	var arr []Value
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return Value{}, fmt.Errorf("strictjson: parse: %w", err)
		}
		val, err := valueFromToken(dec, tok)
		if err != nil {
			return Value{}, err
		}
		arr = append(arr, val)
	}
	if _, err := expectDelim(dec, ']'); err != nil {
		return Value{}, err
	}
	return Value{Kind: Array, Arr: arr}, nil
}

func expectDelim(dec *json.Decoder, want json.Delim) (json.Delim, error) {
	tok, err := dec.Token()
	if err != nil {
		return 0, fmt.Errorf("strictjson: parse: %w", err)
	}
	d, ok := tok.(json.Delim)
	if !ok || d != want {
		return 0, fmt.Errorf("strictjson: parse: expected %q", want)
	}
	return d, nil
}

// Field returns the named field of an object value.
func (v Value) Field(name string) (Value, bool) {
	for _, f := range v.Obj {
		if f.Name == name {
			return f.Value, true
		}
	}
	return Value{}, false
}

// RejectUnknown reports any field of v that is not in allowed.
func (v Value) RejectUnknown(allowed ...string) error {
	ok := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		ok[a] = true
	}
	for _, f := range v.Obj {
		if !ok[f.Name] {
			return fmt.Errorf("strictjson: unknown field %q", f.Name)
		}
	}
	return nil
}
