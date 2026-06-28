package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/basic"
	"github.com/cplieger/wiregen/testdata/edges"
)

// Tests for the Go-type → TypeScript-type mapping performed by the AST field
// walk (the encoding/json fidelity contract): basic kinds, pointers, slices,
// maps, aliases, time.Time, []byte, json.RawMessage, interfaces, and the
// empty/optional/unregistered edge cases.

const edgesPkg = "github.com/cplieger/wiregen/testdata/edges"

// edgesReg builds a registry over the edges fixture package for the given
// registered types. Shared by the type-mapping, embedding, tag, and decoder
// test files.
func edgesReg(types ...wiregen.WireType) *wiregen.Registry {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{edgesPkg}
	r.Types = types
	return r
}

// --- basic kind coverage ---

func TestAllKindsTypes(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllKinds]())
	out := r.GenerateTypes()
	checks := map[string]string{
		"bool: boolean":                "bool",
		"int: number":                  "int",
		"int8: number":                 "int8",
		"int16: number":                "int16",
		"int32: number":                "int32",
		"int64: number":                "int64",
		"uint: number":                 "uint",
		"uint8: number":                "uint8",
		"uint16: number":               "uint16",
		"uint32: number":               "uint32",
		"uint64: number":               "uint64",
		"float32: number":              "float32",
		"float64: number":              "float64",
		"string: string":               "str",
		"slice: string[]":              "slice",
		"map?: Record<string, string>": "map",
		"bytes: string":                "bytes",
		"raw: unknown":                 "raw",
		"iface: unknown":               "iface",
	}
	for expected, label := range checks {
		if !strings.Contains(out, expected) {
			t.Errorf("missing %s mapping, expected %q in:\n%s", label, expected, out)
		}
	}
}

// --- recursion / cycles ---

func TestRecursiveEmbeddedStruct(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TreeNode]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "value: string;") {
		t.Errorf("expected value field, got:\n%s", out)
	}
}

func TestMutualRecursion(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.CycleA](), wiregen.TypeRef[edges.CycleB]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface CycleA") {
		t.Errorf("missing CycleA, got:\n%s", out)
	}
	if !strings.Contains(out, "export interface CycleB") {
		t.Errorf("missing CycleB, got:\n%s", out)
	}
}

func TestSelfReferentialSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.SelfSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "children?: SelfSlice[];") {
		t.Errorf("expected self-referential slice, got:\n%s", out)
	}
}

func TestSelfReferentialMap(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.SelfMap]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "children?: Record<string, SelfMap>;") {
		t.Errorf("expected self-referential map, got:\n%s", out)
	}
}

func TestThreeWayCycle(t *testing.T) {
	r := edgesReg(
		wiregen.TypeRef[edges.CycleX](),
		wiregen.TypeRef[edges.CycleY](),
		wiregen.TypeRef[edges.CycleZ](),
	)
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface CycleX") {
		t.Errorf("missing CycleX, got:\n%s", out)
	}
}

// --- type aliases (Go 1.22+ types.Alias) ---

func TestTypeAlias(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasAliases]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "label: string;") {
		t.Errorf("type alias MyString should resolve to string, got:\n%s", out)
	}
	if !strings.Contains(out, "count: number;") {
		t.Errorf("type alias MyInt should resolve to number, got:\n%s", out)
	}
}

func TestTimeAlias(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasTimeAlias]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "at: string;") {
		t.Errorf("time.Time alias should still resolve to string, got:\n%s", out)
	}
}

func TestAliasOfAlias(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasAliasOfAlias]())
	out := r.GenerateTypes()
	// AliasOfAlias -> MyString -> string
	if !strings.Contains(out, "val: string;") {
		t.Errorf("alias-of-alias should resolve to string, got:\n%s", out)
	}
}

func TestPointerToAlias(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasPtrAlias]())
	out := r.GenerateTypes()
	// *MyString -> optional string
	if !strings.Contains(out, "name?: string;") {
		t.Errorf("*alias should be optional string, got:\n%s", out)
	}
}

func TestDeepTimeAlias(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasDeepTimeAlias]())
	out := r.GenerateTypes()
	// MyTimeAlias -> MyTime -> time.Time -> string
	if !strings.Contains(out, "at: string;") {
		t.Errorf("deep time alias should resolve to string, got:\n%s", out)
	}
}

func TestTimeToString(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.HasTime]()}
	out := r.GenerateTypes()
	if !strings.Contains(out, "created: string;") {
		t.Errorf("time.Time should map to string, got:\n%s", out)
	}
	if !strings.Contains(out, "updated?: string;") {
		t.Errorf("*time.Time should map to optional string, got:\n%s", out)
	}
}

// --- pointers ---

func TestDoublePointer(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DoublePtr]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "val?: string") {
		t.Errorf("double pointer should be optional string, got:\n%s", out)
	}
}

func TestTriplePointer(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TriplePtr]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "val?: string") {
		t.Errorf("***string should be optional string, got:\n%s", out)
	}
}

func TestAllPointerFieldsOptional(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllPointerFields]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "a?: string") {
		t.Errorf("*string should be optional, got:\n%s", out)
	}
	if !strings.Contains(out, "b?: number") {
		t.Errorf("*int should be optional, got:\n%s", out)
	}
	if !strings.Contains(out, "c?: boolean") {
		t.Errorf("*bool should be optional, got:\n%s", out)
	}
	if !strings.Contains(out, "d?: number") {
		t.Errorf("*float64 should be optional, got:\n%s", out)
	}
}

func TestPointerToSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.PtrToSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "items?: string[]") {
		t.Errorf("*[]string should be optional string[], got:\n%s", out)
	}
}

func TestPointerToMap(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.PtrToMap]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "data?: Record<string, number>") {
		t.Errorf("*map[string]int should be optional Record<string, number>, got:\n%s", out)
	}
}

func TestOptionalByteSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.OptionalByteSlice]())
	out := r.GenerateTypes()
	// *[]byte → optional string (pointer + []byte = optional + base64)
	if !strings.Contains(out, "data?: string") {
		t.Errorf("*[]byte should produce optional string, got:\n%s", out)
	}
}

// --- nested slices and maps ---

func TestSliceOfSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.SliceOfSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "matrix: string[][]") {
		t.Errorf("expected string[][], got:\n%s", out)
	}
}

func TestSliceOfSliceOfSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.SliceOfSliceOfSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "cube: number[][][]") {
		t.Errorf("[][][]int should produce number[][][], got:\n%s", out)
	}
}

func TestMapOfSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "data?: Record<string, number[]>") {
		t.Errorf("expected Record<string, number[]>, got:\n%s", out)
	}
}

func TestSliceOfMap(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.SliceOfMap]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "items: Record<string, string>[]") {
		t.Errorf("expected Record<string, string>[], got:\n%s", out)
	}
}

func TestMapOfStructs(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfStructs](), wiregen.TypeRef[edges.MapVal]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "data?: Record<string, MapVal>") {
		t.Errorf("missing map of structs, got:\n%s", out)
	}
}

func TestMapOfMap(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfMap]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "Record<string, Record<string, number>>") {
		t.Errorf("map of map should produce nested Record, got:\n%s", out)
	}
}

func TestDeeplyNestedMap(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DeeplyNestedMap]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "Record<string, Record<string, Record<string, string>>>") {
		t.Errorf("deeply nested map should produce nested Records, got:\n%s", out)
	}
}

func TestSliceOfPointers(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.SliceOfPtrs]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "names: string[]") {
		t.Errorf("[]*string should produce string[], got:\n%s", out)
	}
}

func TestMapOfPointers(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfPtrs]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "scores?: Record<string, number>") {
		t.Errorf("map[string]*int should produce Record<string, number>, got:\n%s", out)
	}
}

func TestMapOfBytes(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfBytes]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "blobs?: Record<string, string>") {
		t.Errorf("map[string][]byte should produce Record<string, string>, got:\n%s", out)
	}
}

func TestNestedOptionalPointerStruct(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.NestedOptPtr](), wiregen.TypeRef[edges.Inner]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "inner?: Inner") {
		t.Errorf("missing optional nested struct, got:\n%s", out)
	}
}

func TestTimeSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TimeSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "dates: string[]") {
		t.Errorf("[]time.Time should produce string[], got:\n%s", out)
	}
}

func TestRawMessageSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.RawSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "items: unknown[]") {
		t.Errorf("[]json.RawMessage should produce unknown[], got:\n%s", out)
	}
}

func TestInterfaceSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.InterfaceSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "items: unknown[]") {
		t.Errorf("[]interface{} should produce unknown[], got:\n%s", out)
	}
}

// --- empty / optional / unregistered edge cases ---

func TestEmptyStruct(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.EmptyStruct]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface EmptyStruct {\n}") {
		t.Errorf("expected empty interface, got:\n%s", out)
	}
}

func TestOptionalEnumField(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasOptEnum]())
	r.Enums = map[string]wiregen.EnumDef{"MyEnum": {Values: []string{"a", "b"}}}
	out := r.GenerateTypes()
	if !strings.Contains(out, "status?: MyEnum") {
		t.Errorf("expected optional enum field, got:\n%s", out)
	}
}

func TestMapOfUnregisteredStruct(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfUnregistered]())
	out := r.GenerateTypes()
	// map[string]EmbedPtrBase where EmbedPtrBase is NOT registered: the field
	// is present and optional (maps are always optional).
	if !strings.Contains(out, "data?:") {
		t.Errorf("map field should be present and optional, got:\n%s", out)
	}
}

// --- name overrides ---

func TestTSNameOverride(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.Inner]())
	r.TSNameOverride = map[string]string{"Inner": "InnerDTO"}
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface InnerDTO") {
		t.Errorf("expected InnerDTO, got:\n%s", out)
	}
}

func TestTSNameOverride_sanitizesNonIdentifier(t *testing.T) {
	// A non-identifier override must be sanitized to a valid TS identifier
	// rather than emitted verbatim (mirrors EnumTSName via tsEnumName); a valid
	// identifier override is unchanged (TestTSNameOverride). An override that
	// sanitizes to empty falls back to the Go name.
	r := edgesReg(wiregen.TypeRef[edges.Inner]())
	r.TSNameOverride = map[string]string{"Inner": "Inner Profile 2"}
	if out := r.GenerateTypes(); !strings.Contains(out, "export interface InnerProfile2") ||
		strings.Contains(out, "Inner Profile 2") {
		t.Errorf("expected sanitized InnerProfile2 (no verbatim spaces), got:\n%s", out)
	}

	r2 := edgesReg(wiregen.TypeRef[edges.Inner]())
	r2.TSNameOverride = map[string]string{"Inner": "@@@"}
	if out := r2.GenerateTypes(); !strings.Contains(out, "export interface Inner") {
		t.Errorf("expected fallback to Go name Inner for an all-invalid override, got:\n%s", out)
	}
}

func TestEnumTSNameOverride(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasOptEnum]())
	r.Enums = map[string]wiregen.EnumDef{"MyEnum": {Values: []string{"a", "b"}}}
	r.EnumTSName = map[string]string{"MyEnum": "StatusEnum"}
	out := r.GenerateTypes()
	if !strings.Contains(out, "export type StatusEnum") {
		t.Errorf("expected StatusEnum, got:\n%s", out)
	}
}

// TestJSONNumberMapsToNumber pins the cycle-1 json.Number branch: an
// encoding/json.Number field maps to TS number (not string) and decodes via
// reqNum, matching encoding/json's unquoted-number wire form.
func TestJSONNumberMapsToNumber(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasJSONNumber]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "amount: number;") {
		t.Errorf("json.Number should map to number, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, `reqNum(o, "amount"`) {
		t.Errorf("json.Number should decode with reqNum, got:\n%s", dec)
	}
}
