package strictjson

import "testing"

// mustParse parses data and fails the test on any error.
func mustParse(t *testing.T, data string) Value {
	t.Helper()
	v, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) = %v", data, err)
	}
	return v
}

func TestParseValueKinds(t *testing.T) {
	root := mustParse(t, `{"s":"x","n":7,"b":true,"a":[1,false],"o":{}}`)
	if root.Kind != Object {
		t.Fatalf("root kind = %v, want Object", root.Kind)
	}
	s, _ := root.Field("s")
	if s.Kind != String || s.Str != "x" {
		t.Fatalf("string field = %+v", s)
	}
	n, _ := root.Field("n")
	if n.Kind != Number || n.Num != 7 {
		t.Fatalf("number field = %+v", n)
	}
	b, _ := root.Field("b")
	if b.Kind != Bool || !b.B {
		t.Fatalf("bool field = %+v", b)
	}
	a, _ := root.Field("a")
	if a.Kind != Array || len(a.Arr) != 2 {
		t.Fatalf("array field = %+v", a)
	}
	if a.Arr[0].Kind != Number || a.Arr[0].Num != 1 || a.Arr[1].Kind != Bool || a.Arr[1].B {
		t.Fatalf("array elements = %+v", a.Arr)
	}
	o, _ := root.Field("o")
	if o.Kind != Object || len(o.Obj) != 0 {
		t.Fatalf("empty object field = %+v", o)
	}
}

func TestParseRejectsStrictViolations(t *testing.T) {
	cases := map[string]string{
		"empty":              ``,
		"malformed":          `{"a":`,
		"trailing data":      `{} {}`,
		"duplicate field":    `{"a":1,"a":2}`,
		"explicit null":      `{"a":null}`,
		"top-level null":     `null`,
		"negative number":    `{"a":-1}`,
		"fraction":           `{"a":1.5}`,
		"exponent":           `{"a":1e3}`,
		"overflow number":    `{"a":18446744073709551616}`,
		"non-string name":    `{1:2}`,
		"unterminated array": `[1,`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(doc)); err == nil {
				t.Fatalf("Parse(%q) succeeded, want error", doc)
			}
		})
	}
}

func TestFieldMissing(t *testing.T) {
	root := mustParse(t, `{"a":1}`)
	if _, ok := root.Field("b"); ok {
		t.Fatal("Field(\"b\") reported present")
	}
}

func TestRejectUnknown(t *testing.T) {
	root := mustParse(t, `{"a":1,"b":2}`)
	if err := root.RejectUnknown("a", "b"); err != nil {
		t.Fatalf("RejectUnknown(allowed) = %v", err)
	}
	if err := root.RejectUnknown("a"); err == nil {
		t.Fatal("RejectUnknown(partial) succeeded, want error")
	}
	if err := root.RejectUnknown(); err == nil {
		t.Fatal("RejectUnknown(none) succeeded, want error")
	}
}

func TestParseUint64Bounds(t *testing.T) {
	root := mustParse(t, `{"a":18446744073709551615}`)
	n, _ := root.Field("a")
	if n.Num != ^uint64(0) {
		t.Fatalf("max uint64 decoded as %d", n.Num)
	}
}
