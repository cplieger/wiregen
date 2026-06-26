package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/edges"
)

// Tests for json struct-tag semantics: the "-" skip, the "-," field-named-dash
// case, empty wire names falling back to the Go field name, omitempty/omitzero
// optionality, and the ",string" number-as-string encoding.

func TestDashCommaExcludesHidden(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DashComma]())
	out := r.GenerateTypes()
	// json:"-" excludes the field entirely (the json:"-," field named "-" is
	// covered by the white-box resolveStructFields test).
	if strings.Contains(out, "Hidden") || strings.Contains(out, "hidden") {
		t.Errorf("json:\"-\" field should be excluded, got:\n%s", out)
	}
	if !strings.Contains(out, "name: string;") {
		t.Errorf("normal field should be present, got:\n%s", out)
	}
}

func TestTagVariants(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TagVariants]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "required: string;") {
		t.Errorf("required field missing, got:\n%s", out)
	}
	if !strings.Contains(out, "omitempty_field?: string;") {
		t.Errorf("omitempty field should be optional, got:\n%s", out)
	}
	if !strings.Contains(out, "wire_name: string;") {
		t.Errorf("renamed field should use wire_name, got:\n%s", out)
	}
	if !strings.Contains(out, "NoTag: string;") {
		t.Errorf("untagged field should use Go name, got:\n%s", out)
	}
}

func TestTagEmptyWireName(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TagEmpty]())
	out := r.GenerateTypes()
	// json:",omitempty" → wire name = "Value" (Go field name), optional.
	if !strings.Contains(out, "Value?: string") {
		t.Errorf("empty tag name should use Go field name 'Value', got:\n%s", out)
	}
}

func TestTagStringEncoding(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TagOnlyOptions]())
	out := r.GenerateTypes()
	// json:",string" → wire name = "Count", type = string.
	if !strings.Contains(out, "Count: string") {
		t.Errorf("json:\",string\" should produce string type with Go name, got:\n%s", out)
	}
}

func TestManyTagOptions(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.ManyOptions]())
	out := r.GenerateTypes()
	// json:"a,omitempty,string" → optional + string type.
	if !strings.Contains(out, "a?: string;") {
		t.Errorf("multiple tag options should produce optional string, got:\n%s", out)
	}
}

func TestStructWithRawAndTime(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.StructWithRawAndTime]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "payload: unknown;") {
		t.Errorf("json.RawMessage should be unknown, got:\n%s", out)
	}
	if !strings.Contains(out, "when: string;") {
		t.Errorf("time.Time should be string, got:\n%s", out)
	}
	if !strings.Contains(out, "label: string;") {
		t.Errorf("string should be string, got:\n%s", out)
	}
}
