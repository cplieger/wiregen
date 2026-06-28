package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/basic"
	"github.com/cplieger/wiregen/testdata/edges"
)

// Tests for decoder emission: the primitive validators chosen per field, the
// empty / all-optional struct shapes, the decoder path-segment override, and
// the absence of an empty `import type {}` line.

func TestAllKindsDecoders(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllKinds]())
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "reqBool(o, \"bool\"") {
		t.Errorf("missing reqBool, got:\n%s", dec)
	}
	if !strings.Contains(dec, "reqNum(o, \"int\"") {
		t.Errorf("missing reqNum, got:\n%s", dec)
	}
	if !strings.Contains(dec, "reqStr(o, \"string\"") {
		t.Errorf("missing reqStr, got:\n%s", dec)
	}
}

func TestAllOptionalFieldsDecoder(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllOptional]())
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "const out: AllOptional = {") {
		t.Errorf("expected empty required block, got:\n%s", dec)
	}
}

func TestEmptyStructDecoder(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.EmptyStruct]())
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeEmptyStruct") {
		t.Errorf("empty struct should still get a decoder, got:\n%s", dec)
	}
	if !strings.Contains(dec, "return out;") {
		t.Errorf("decoder should return out, got:\n%s", dec)
	}
}

func TestPathNameOverride(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.Inner]())
	r.PathNameOverride = map[string]string{"Inner": "custom_path"}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "$.custom_path") {
		t.Errorf("expected custom path, got:\n%s", dec)
	}
}

func TestNoEmptyTypeImport(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.HasBytes]()}
	dec := r.GenerateDecoders()
	// HasBytes references no other registered type, so type imports are empty —
	// the import block must be omitted, not emitted as `import type {}`.
	if strings.Contains(dec, "import type {  }") || strings.Contains(dec, "import type {}") {
		t.Errorf("should not emit empty type imports, got:\n%s", dec)
	}
}

// TestDecoders_OptionalEnumUsesReqOneOf pins the optional-enum branch of
// emitOptionalField: an optional enum field decodes via a guarded reqOneOf
// call against the enum value array, never a primitive helper.
func TestDecoders_OptionalEnumUsesReqOneOf(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasOptEnum]())
	r.Enums = map[string]wiregen.EnumDef{"MyEnum": {Values: []string{"a", "b"}}}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, `if (o["status"] !== undefined) out.status = reqOneOf(o, "status", MY_ENUMS,`) {
		t.Errorf("optional enum field should decode with guarded reqOneOf, got:\n%s", dec)
	}
}

// TestDecoders_OptionalByteSliceUsesOptStr pins the optional []byte branch
// of emitOptionalField: a *[]byte field keeps the []byte-as-string mapping
// into the optional path, decoding via optStr and never decodeArray.
func TestDecoders_OptionalByteSliceUsesOptStr(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.OptionalByteSlice]())
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, `const data = optStr(o, "data",`) {
		t.Errorf("optional *[]byte should decode with optStr, got:\n%s", dec)
	}
	if strings.Contains(dec, `decodeArray(o["data"]`) {
		t.Errorf("optional *[]byte must not decode as an array, got:\n%s", dec)
	}
}

// TestDecoders_UncoveredEmitBranches pins the decoder-emission branches the
// existing suite leaves uncovered (emitOptionalField + elemDecoderExpr): an
// optional json:",string" field (optStr), an optional unregistered-struct
// field (passes through as unknown), a number-typed collection element
// (typeof-number guard), and a nested-collection element (identity cast). Each
// asserts the exact emitted decoder expression so a regression in that branch
// is caught.
func TestDecoders_UncoveredEmitBranches(t *testing.T) {
	cases := []struct {
		name  string
		types []wiregen.WireType
		wants []string
	}{
		{
			name:  "optional json-string field decodes via optStr",
			types: []wiregen.WireType{wiregen.TypeRef[edges.ManyOptions]()},
			wants: []string{
				`const a = optStr(o, "a", "$.many_options");`,
				`if (a !== undefined) out.a = a;`,
			},
		},
		{
			name:  "optional unregistered struct passes through as unknown",
			types: []wiregen.WireType{wiregen.TypeRef[edges.NestedOptPtr]()},
			wants: []string{`if (o["inner"] !== undefined) out.inner = o["inner"] as unknown;`},
		},
		{
			name:  "number collection element is typeof-guarded",
			types: []wiregen.WireType{wiregen.TypeRef[edges.MapOfPtrs]()},
			wants: []string{`decodeRecord(o["scores"], (v) => { if (typeof v !== "number") throw new TypeError("expected number"); return v as number; }, "$.map_of_ptrs.scores")`},
		},
		{
			name:  "nested collection element falls back to identity cast",
			types: []wiregen.WireType{wiregen.TypeRef[edges.SliceOfSlice]()},
			wants: []string{`decodeArray(o["matrix"], (v) => v as unknown, "$.slice_of_slice.matrix")`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := edgesReg(tc.types...).GenerateDecoders()
			for _, want := range tc.wants {
				if !strings.Contains(dec, want) {
					t.Errorf("decoder missing %q\n--- output ---\n%s", want, dec)
				}
			}
		})
	}
}
