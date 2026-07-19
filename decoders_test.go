package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v2"
	"github.com/cplieger/wiregen/v2/testdata/basic"
	"github.com/cplieger/wiregen/v2/testdata/edges"
)

// Tests for decoder emission: the primitive validators chosen per field, the
// empty / all-optional struct shapes, the decoder path-segment override, and
// the absence of an empty `import type {}` line.

func TestAllKindsDecoders(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllKinds]())
	dec := mustGen(t, r.GenerateDecoders)
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
	dec := mustGen(t, r.GenerateDecoders)
	if !strings.Contains(dec, "const out: AllOptional = {") {
		t.Errorf("expected empty required block, got:\n%s", dec)
	}
}

func TestEmptyStructDecoder(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.EmptyStruct]())
	dec := mustGen(t, r.GenerateDecoders)
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
	dec := mustGen(t, r.GenerateDecoders)
	if !strings.Contains(dec, "$.custom_path") {
		t.Errorf("expected custom path, got:\n%s", dec)
	}
}

func TestNoEmptyTypeImport(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/v2/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.HasBytes]()}
	dec := mustGen(t, r.GenerateDecoders)
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
	dec := mustGen(t, r.GenerateDecoders)
	if !strings.Contains(dec, `if (o["status"] !== undefined && o["status"] !== null) out.status = reqOneOf(o, "status", MY_ENUMS,`) {
		t.Errorf("optional enum field should decode with guarded reqOneOf, got:\n%s", dec)
	}
}

// TestDecoders_OptionalByteSliceUsesOptStr pins the optional []byte branch
// of emitOptionalField: a *[]byte field keeps the []byte-as-string mapping
// into the optional path, decoding via optStr and never decodeArray.
func TestDecoders_OptionalByteSliceUsesOptStr(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.OptionalByteSlice]())
	dec := mustGen(t, r.GenerateDecoders)
	if !strings.Contains(dec, `const data = o["data"] === null ? undefined : optStr(o, "data",`) {
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
				`const a = o["a"] === null ? undefined : optStr(o, "a", "$.many_options");`,
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
			name:  "nested collection element recurses with per-level validation",
			types: []wiregen.WireType{wiregen.TypeRef[edges.SliceOfSlice]()},
			wants: []string{`decodeArray(o["matrix"], (v) => v === null ? [] : decodeArray(v, (v) => { if (typeof v !== "string") throw new TypeError("expected string"); return v as string; }, "$.slice_of_slice.matrix"), "$.slice_of_slice.matrix")`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := mustGen(t, edgesReg(tc.types...).GenerateDecoders)
			for _, want := range tc.wants {
				if !strings.Contains(dec, want) {
					t.Errorf("decoder missing %q\n--- output ---\n%s", want, dec)
				}
			}
		})
	}
}

// TestDecoders_NullHandling pins the JSON-null decode contract: a required
// slice/map accepts null as empty (encoding/json marshals a nil non-omitempty
// slice/map to null), an optional field treats present-null as absent in both
// the guard-style and const-ternary branches, and the raw/interface
// pass-throughs keep null as data. Nullable-vs-optional is a documented
// non-goal: null and absent collapse to the same TS undefined.
func TestDecoders_NullHandling(t *testing.T) {
	dec := mustGen(t, edgesReg(wiregen.TypeRef[edges.AllKinds]()).GenerateDecoders)
	wants := []string{
		// required slice: null -> empty array
		`slice: o["slice"] === null ? [] : decodeArray(o["slice"],`,
		// required []byte (base64 string on the wire; nil marshals null): null -> ""
		`bytes: o["bytes"] === null ? "" : reqStr(o, "bytes", "$.all_kinds"),`,
		// required map (no omitempty — maps keep source optionality): null -> {}
		`map: o["map"] === null ? {} : decodeRecord(o["map"],`,
		// required raw/interface pass-throughs keep null as data (bare cast)
		`raw: o["raw"] as unknown,`,
		`iface: o["iface"] as unknown,`,
	}
	for _, want := range wants {
		if !strings.Contains(dec, want) {
			t.Errorf("AllKinds decoder missing %q\n--- output ---\n%s", want, dec)
		}
	}

	dec = mustGen(t, edgesReg(wiregen.TypeRef[edges.NestedOptPtr](), wiregen.TypeRef[edges.Inner]()).GenerateDecoders)
	if want := `if (o["inner"] !== undefined && o["inner"] !== null) out.inner = decodeInner(o["inner"]);`; !strings.Contains(dec, want) {
		t.Errorf("optional struct decoder missing null-skipping guard %q\n--- output ---\n%s", want, dec)
	}

	dec = mustGen(t, edgesReg(wiregen.TypeRef[edges.AllOptional]()).GenerateDecoders)
	if want := `const a = o["a"] === null ? undefined : optStr(o, "a", "$.all_optional");`; !strings.Contains(dec, want) {
		t.Errorf("optional primitive decoder missing null ternary %q\n--- output ---\n%s", want, dec)
	}
}
