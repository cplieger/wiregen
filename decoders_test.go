package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/basic"
	"github.com/cplieger/wiregen/v4/testdata/edges"
)

// Tests for decoder emission: the primitive validators chosen per field, the
// empty / all-optional struct shapes, the decoder path-segment override, and
// the absence of an empty `import type {}` line.

func TestAllKindsDecoders(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllKinds]())
	dec := mustGen(t, r.decodersFile)
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
	dec := mustGen(t, r.decodersFile)
	if !strings.Contains(dec, "const out: AllOptional = {") {
		t.Errorf("expected empty required block, got:\n%s", dec)
	}
}

func TestEmptyStructDecoder(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.EmptyStruct]())
	dec := mustGen(t, r.decodersFile)
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
	dec := mustGen(t, r.decodersFile)
	if !strings.Contains(dec, "$.custom_path") {
		t.Errorf("expected custom path, got:\n%s", dec)
	}
}

func TestNoEmptyTypeImport(t *testing.T) {
	r := newTestRegistry(
		wiregen.WithValidatorsImport("./v.js"),
	)
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.HasBytes]()}
	dec := mustGen(t, r.decodersFile)
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
	r.Enums = []wiregen.EnumRef{wiregen.StringEnum[edges.MyEnum]()}
	dec := mustGen(t, r.decodersFile)
	if !strings.Contains(dec, `if ($v !== undefined && $v !== null) out.status = reqOneOf(o, "status", MY_ENUMS,`) {
		t.Errorf("optional enum field should decode with guarded reqOneOf, got:\n%s", dec)
	}
}

// TestDecoders_OptionalByteSliceIsGuardedString pins the optional []byte branch
// of emitOptionalField: a *[]byte field keeps the []byte-as-string mapping
// into the optional path, decoding via reqStr under the presence guard and
// never decodeArray.
func TestDecoders_OptionalByteSliceIsGuardedString(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.OptionalByteSlice]())
	dec := mustGen(t, r.decodersFile)
	if !strings.Contains(dec, `if ($v !== undefined && $v !== null) out.data = own(o, "data") === null ? "" : reqStr(o, "data",`) {
		t.Errorf("optional *[]byte should decode as a string, got:\n%s", dec)
	}
	if strings.Contains(dec, `decodeArray(own(o, "data")`) {
		t.Errorf("optional *[]byte must not decode as an array, got:\n%s", dec)
	}
}

// TestDecoders_UncoveredEmitBranches pins the decoder-emission branches the
// existing suite leaves uncovered (emitOptionalField + elemDecoderExpr): an
// optional json:",string" field (reqStr under the guard), a number-typed collection element
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
			name:  "optional json-string field decodes as the text of a JSON string under the guard",
			types: []wiregen.WireType{wiregen.TypeRef[edges.ManyOptions]()},
			wants: []string{
				`const $v = own(o, "a");`,
				`if ($v !== undefined && $v !== null) out.a = checkPattern(reqStr(o, "a", "$.many_options"), "^\"(?:`,
				`)*\"$", "$.many_options.a");`,
			},
		},
		{
			name:  "pointer collection element keeps null and typeof-guards the rest",
			types: []wiregen.WireType{wiregen.TypeRef[edges.MapOfPtrs]()},
			wants: []string{`decodeRecord(own(o, "scores"), (v) => v === null ? null : ((v) => { if (typeof v !== "number") throw new globalThis.TypeError("expected number"); return v as number; })(v), "$.map_of_ptrs.scores")`},
		},
		{
			name:  "nested collection element recurses with per-level validation",
			types: []wiregen.WireType{wiregen.TypeRef[edges.SliceOfSlice]()},
			wants: []string{`decodeArray(own(o, "matrix"), (v) => v === null ? [] : decodeArray(v, (v) => { if (typeof v !== "string") throw new globalThis.TypeError("expected string"); return v as string; }, "$.slice_of_slice.matrix"), "$.slice_of_slice.matrix")`},
		},
		{
			name:  "map element inside a slice accepts null as the empty record",
			types: []wiregen.WireType{wiregen.TypeRef[edges.SliceOfMap]()},
			wants: []string{`decodeArray(own(o, "items"), (v) => v === null ? {} : decodeRecord(v, (v) => { if (typeof v !== "string") throw new globalThis.TypeError("expected string"); return v as string; }, "$.slice_of_map.items"), "$.slice_of_map.items")`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := mustGen(t, edgesReg(tc.types...).decodersFile)
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
// slice/map to null), an optional field treats present-null as absent, and
// the json.RawMessage
// pass-through keeps null as data. The decoders read null and absent alike
// (docs/type-mapping.md "Null and presence").
func TestDecoders_NullHandling(t *testing.T) {
	dec := mustGen(t, edgesReg(wiregen.TypeRef[edges.AllKinds]()).decodersFile)
	wants := []string{
		// required slice: null -> empty array
		`slice: own(o, "slice") === null ? [] : decodeArray(own(o, "slice"),`,
		// required []byte (base64 string on the wire; nil marshals null): null -> ""
		`bytes: own(o, "bytes") === null ? "" : reqStr(o, "bytes", "$.all_kinds"),`,
		// required map (no omitempty — maps keep source optionality): null -> {}
		`map: own(o, "map") === null ? {} : decodeRecord(own(o, "map"),`,
		// a required json.RawMessage keeps null as data (bare cast)
		`raw: own(o, "raw") as unknown,`,
	}
	for _, want := range wants {
		if !strings.Contains(dec, want) {
			t.Errorf("AllKinds decoder missing %q\n--- output ---\n%s", want, dec)
		}
	}

	dec = mustGen(t, edgesReg(wiregen.TypeRef[edges.NestedOptPtr](), wiregen.TypeRef[edges.Inner]()).decodersFile)
	if want := `if ($v !== undefined && $v !== null) out.inner = decodeInner(own(o, "inner"));`; !strings.Contains(dec, want) {
		t.Errorf("optional struct decoder missing null-skipping guard %q\n--- output ---\n%s", want, dec)
	}

	dec = mustGen(t, edgesReg(wiregen.TypeRef[edges.AllOptional]()).decodersFile)
	if want := `if ($v !== undefined && $v !== null) out.a = reqStr(o, "a", "$.all_optional");`; !strings.Contains(dec, want) {
		t.Errorf("optional primitive decoder missing the presence guard %q\n--- output ---\n%s", want, dec)
	}
}

// TestDecoders_OptionalPassthroughKeepsNull: an optional json.RawMessage field
// passes a present null through as data, like its required
// form, while every other optional field reads null as absent.
func TestDecoders_OptionalPassthroughKeepsNull(t *testing.T) {
	r := newTestRegistry(
		wiregen.WithValidatorsImport("./v.js"),
	)
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.HasOptionalPassthrough]()}
	dec := mustGen(t, r.decodersFile)
	for _, want := range []string{
		`if ($v !== undefined) out.raw = own(o, "raw") as unknown;`,
	} {
		if !strings.Contains(dec, want) {
			t.Errorf("HasOptionalPassthrough decoder missing %q\n--- output ---\n%s", want, dec)
		}
	}
	if strings.Contains(dec, "!== null") {
		t.Errorf("optional pass-through fields must keep null as data, got:\n%s", dec)
	}
}

// TestDecoders_OptionalMappedField: an optional field of a consumer-mapped
// type reads an absent key or a null as absent, never calling the consumer's
// decoder on it, then decodes through that decoder when one is mapped and
// casts to the mapped TS type when not.
func TestDecoders_OptionalMappedField(t *testing.T) {
	const customID = "github.com/cplieger/wiregen/v4/testdata/basic.CustomID"
	cases := []struct {
		name    string
		decoder string
		wants   []string
	}{
		{
			name:    "decoder mapping",
			decoder: "decodeCustomID",
			wants: []string{
				`const $v = own(o, "id");`,
				`if ($v !== undefined && $v !== null) out.id = decodeCustomID(o, "id", "$.has_optional_mapped");`,
			},
		},
		{
			name:  "type mapping only",
			wants: []string{`if ($v !== undefined && $v !== null) out.id = own(o, "id") as CustomRef;`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRegistry(
				wiregen.WithValidatorsImport("./v.js"),
			)
			r.Types = []wiregen.WireType{wiregen.TypeRef[basic.HasOptionalMapped]()}
			r.Mappings = map[string]wiregen.Mapping{customID: {TS: "CustomRef", Decoder: tc.decoder}}
			dec := mustGen(t, r.decodersFile)
			for _, want := range tc.wants {
				if !strings.Contains(dec, want) {
					t.Errorf("HasOptionalMapped decoder missing %q\n--- output ---\n%s", want, dec)
				}
			}
		})
	}
}
