package wiregen_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/cplieger/wiregen"
)

// --- Every reflect.Kind as a field type ---

type AllKinds struct {
	Bool       bool              `json:"bool"`
	Int        int               `json:"int"`
	Int8       int8              `json:"int8"`
	Int16      int16             `json:"int16"`
	Int32      int32             `json:"int32"`
	Int64      int64             `json:"int64"`
	Uint       uint              `json:"uint"`
	Uint8      uint8             `json:"uint8"`
	Uint16     uint16            `json:"uint16"`
	Uint32     uint32            `json:"uint32"`
	Uint64     uint64            `json:"uint64"`
	Float32    float32           `json:"float32"`
	Float64    float64           `json:"float64"`
	String     string            `json:"string"`
	Slice      []string          `json:"slice"`
	Map        map[string]int    `json:"map"`
	Struct     Address           `json:"struct"`
	Ptr        *Address          `json:"ptr,omitempty"`
	Interface  any               `json:"interface"`
	ByteSlice  []byte            `json:"byte_slice"`
	RawJSON    json.RawMessage   `json:"raw_json"`
	SliceOfInt []int             `json:"slice_of_int"`
	MapOfBool  map[string]bool   `json:"map_of_bool"`
	MapOfStr   map[string]string `json:"map_of_str,omitempty"`
	PtrBool    *bool             `json:"ptr_bool,omitempty"`
	PtrInt     *int              `json:"ptr_int,omitempty"`
	PtrStr     *string           `json:"ptr_str,omitempty"`
	PtrFloat   *float64          `json:"ptr_float,omitempty"`
}

func TestAllKindsTypes(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[AllKinds](), reflect.TypeFor[Address]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	expects := map[string]string{
		"bool":         "bool: boolean;",
		"int":          "int: number;",
		"int8":         "int8: number;",
		"int16":        "int16: number;",
		"int32":        "int32: number;",
		"int64":        "int64: number;",
		"uint":         "uint: number;",
		"uint8":        "uint8: number;",
		"uint16":       "uint16: number;",
		"uint32":       "uint32: number;",
		"uint64":       "uint64: number;",
		"float32":      "float32: number;",
		"float64":      "float64: number;",
		"string":       "string: string;",
		"slice":        "slice: string[];",
		"map":          "map?: Record<string, number>;",
		"struct":       "struct: Address;",
		"ptr":          "ptr?: Address;",
		"interface":    "interface: unknown;",
		"byte_slice":   "byte_slice: string;",
		"raw_json":     "raw_json: unknown;",
		"slice_of_int": "slice_of_int: number[];",
		"map_of_bool":  "map_of_bool?: Record<string, boolean>;",
		"map_of_str":   "map_of_str?: Record<string, string>;",
		"ptr_bool":     "ptr_bool?: boolean;",
		"ptr_int":      "ptr_int?: number;",
		"ptr_str":      "ptr_str?: string;",
		"ptr_float":    "ptr_float?: number;",
	}
	for label, expected := range expects {
		if !strings.Contains(out, expected) {
			t.Errorf("field %s: expected %q in output, got:\n%s", label, expected, out)
		}
	}
}

func TestAllKindsDecoders(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[AllKinds](), reflect.TypeFor[Address]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	dec := r.GenerateDecoders()
	// Must not panic and must produce valid decoder
	if !strings.Contains(dec, "decodeAllKinds") {
		t.Fatalf("missing decodeAllKinds, got:\n%s", dec)
	}
	if !strings.Contains(dec, "decodeAddress") {
		t.Fatalf("missing decodeAddress, got:\n%s", dec)
	}
	// Verify specific decoder patterns
	if !strings.Contains(dec, "reqBool(o, \"bool\"") {
		t.Errorf("expected reqBool for bool field, got:\n%s", dec)
	}
	if !strings.Contains(dec, "reqNum(o, \"int\"") {
		t.Errorf("expected reqNum for int field, got:\n%s", dec)
	}
	if !strings.Contains(dec, "reqStr(o, \"string\"") {
		t.Errorf("expected reqStr for string field, got:\n%s", dec)
	}
	if !strings.Contains(dec, "decodeArray") {
		t.Errorf("expected decodeArray for slice fields, got:\n%s", dec)
	}
	if !strings.Contains(dec, "decodeRecord") {
		t.Errorf("expected decodeRecord for map fields, got:\n%s", dec)
	}
}

// --- Deeply nested mutual recursion (3-way cycle) ---

type TriA struct {
	Name string `json:"name"`
	B    *TriB  `json:"b,omitempty"`
}

type TriB struct {
	Value string `json:"value"`
	C     *TriC  `json:"c,omitempty"`
}

type TriC struct {
	Data string `json:"data"`
	A    *TriA  `json:"a,omitempty"`
}

func TestThreeWayCycle(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[TriA](), reflect.TypeFor[TriB](), reflect.TypeFor[TriC]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	for _, iface := range []string{"TriA", "TriB", "TriC"} {
		if !strings.Contains(out, "export interface "+iface) {
			t.Errorf("missing %s interface, got:\n%s", iface, out)
		}
	}
	dec := r.GenerateDecoders()
	for _, d := range []string{"decodeTriA", "decodeTriB", "decodeTriC"} {
		if !strings.Contains(dec, d) {
			t.Errorf("missing %s decoder, got:\n%s", d, dec)
		}
	}
}

// --- Self-referencing via map value ---

type RecursiveMap struct {
	Children map[string]*RecursiveMap `json:"children,omitempty"`
	Label    string                   `json:"label"`
}

func TestRecursiveMapValue(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[RecursiveMap]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "children?: Record<string, RecursiveMap>;") {
		t.Errorf("expected recursive map type, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeRecursiveMap") {
		t.Errorf("expected decoder for RecursiveMap, got:\n%s", dec)
	}
}

// --- Slice of slice (nested arrays) ---

type Matrix struct {
	Grid [][]int `json:"grid"`
}

func TestSliceOfSlice(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Matrix]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "grid: number[][];") {
		t.Errorf("expected number[][] for [][]int, got:\n%s", out)
	}
}

// --- Map of slices ---

type MapOfSlices struct {
	Data map[string][]int `json:"data"`
}

func TestMapOfSlices(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[MapOfSlices]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "data?: Record<string, number[]>;") {
		t.Errorf("expected Record<string, number[]>, got:\n%s", out)
	}
}

// --- Slice of maps ---

type SliceOfMaps struct {
	Items []map[string]string `json:"items"`
}

func TestSliceOfMaps(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[SliceOfMaps]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "items: Record<string, string>[];") {
		t.Errorf("expected Record<string, string>[], got:\n%s", out)
	}
}

// --- Name collision: TSNameOverride maps different types to same name, dedup works ---

type CollisionX struct {
	X string `json:"x"`
}

type CollisionY struct {
	Y string `json:"y"`
}

func TestTSNameOverrideDedup(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[CollisionX](), reflect.TypeFor[CollisionY]()},
		TSNameOverride:   map[string]string{"CollisionX": "Merged", "CollisionY": "Merged"},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	count := strings.Count(out, "export interface Merged")
	if count != 1 {
		t.Errorf("expected exactly 1 'export interface Merged', got %d in:\n%s", count, out)
	}
	dec := r.GenerateDecoders()
	countDec := strings.Count(dec, "export const decodeMerged")
	if countDec != 1 {
		t.Errorf("expected exactly 1 decodeMerged, got %d in:\n%s", countDec, dec)
	}
}

// --- EnumTSName override ---

type Color string

type HasColor struct {
	Color Color `json:"color"`
}

func TestEnumTSNameOverride(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[HasColor]()},
		Enums:            map[string]wiregen.EnumDef{"Color": {Values: []string{"red", "green", "blue"}}},
		EnumTSName:       map[string]string{"Color": "Colour"},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "export type Colour") {
		t.Errorf("expected Colour enum type, got:\n%s", out)
	}
	if !strings.Contains(out, "color: Colour;") {
		t.Errorf("expected field typed as Colour, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "COLOURS") {
		t.Errorf("expected COLOURS const in decoder, got:\n%s", dec)
	}
}

// --- Embedded struct with field name collision (outer wins in encoding/json) ---

type Base struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

type Override struct {
	Base
	Name string `json:"name"` // overrides Base.Name
}

func TestEmbeddedFieldOverride(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Override]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	// "name" should appear exactly once
	count := strings.Count(out, "name: string;")
	if count != 1 {
		t.Errorf("expected exactly 1 'name: string;' (override wins), got %d in:\n%s", count, out)
	}
	// "id" from Base should still be present
	if !strings.Contains(out, "id: number;") {
		t.Errorf("expected id from embedded Base, got:\n%s", out)
	}
}

// --- Unsafe.Pointer field (exotic Kind) should not panic ---

type HasUnsafePtr struct {
	Ptr  unsafe.Pointer `json:"ptr"`
	Name string         `json:"name"`
}

func TestUnsafePointerField(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[HasUnsafePtr]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic
	out := r.GenerateTypes()
	if !strings.Contains(out, "name: string;") {
		t.Errorf("expected name field, got:\n%s", out)
	}
	// unsafe.Pointer should map to unknown
	if !strings.Contains(out, "ptr: unknown;") {
		t.Errorf("expected ptr: unknown for unsafe.Pointer, got:\n%s", out)
	}
	// Decoder must not panic
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeHasUnsafePtr") {
		t.Errorf("expected decoder, got:\n%s", dec)
	}
}

// --- Channel field (exotic Kind) should not panic ---

type HasChan struct {
	Ch   chan int `json:"ch"`
	Name string   `json:"name"`
}

func TestChanField(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[HasChan]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "name: string;") {
		t.Errorf("expected name field, got:\n%s", out)
	}
	// chan should map to unknown
	if !strings.Contains(out, "ch: unknown;") {
		t.Errorf("expected ch: unknown for chan, got:\n%s", out)
	}
}

// --- Func field (exotic Kind) should not panic ---

type HasFunc struct {
	Fn   func() `json:"fn"`
	Name string `json:"name"`
}

func TestFuncField(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[HasFunc]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "fn: unknown;") {
		t.Errorf("expected fn: unknown for func, got:\n%s", out)
	}
}

// --- Array (not slice) field ---

type HasArray struct {
	Fixed [3]int `json:"fixed"`
	Name  string `json:"name"`
}

func TestArrayField(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[HasArray]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	// Arrays in Go JSON serialize like slices; should map to unknown or number[]
	// The library doesn't handle reflect.Array explicitly, so it falls to unknown
	if !strings.Contains(out, "fixed:") {
		t.Errorf("expected fixed field, got:\n%s", out)
	}
}

// --- Complex field (exotic Kind) should not panic ---

type HasComplex struct {
	C    complex128 `json:"c"`
	Name string     `json:"name"`
}

func TestComplexField(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[HasComplex]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic
	out := r.GenerateTypes()
	if !strings.Contains(out, "c: unknown;") {
		t.Errorf("expected c: unknown for complex128, got:\n%s", out)
	}
}

// --- Uintptr field ---

type HasUintptr struct {
	Addr uintptr `json:"addr"`
	Name string  `json:"name"`
}

func TestUintptrField(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[HasUintptr]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	// uintptr is numeric in JSON
	if !strings.Contains(out, "addr: number;") && !strings.Contains(out, "addr: unknown;") {
		t.Errorf("expected addr field, got:\n%s", out)
	}
}

// --- Output determinism: extended with many types, enums, overrides ---

func TestOutputDeterminismExtended(t *testing.T) {
	makeReg := func() *wiregen.Registry {
		return &wiregen.Registry{
			WireTypes: []reflect.Type{
				reflect.TypeFor[AllKinds](),
				reflect.TypeFor[TriA](),
				reflect.TypeFor[TriB](),
				reflect.TypeFor[TriC](),
				reflect.TypeFor[RecursiveMap](),
				reflect.TypeFor[Matrix](),
				reflect.TypeFor[MapOfSlices](),
				reflect.TypeFor[SliceOfMaps](),
				reflect.TypeFor[HasColor](),
				reflect.TypeFor[Address](),
				reflect.TypeFor[User](),
				reflect.TypeFor[Notification](),
			},
			Enums: map[string]wiregen.EnumDef{
				"Status": {Values: []string{"active", "inactive", "banned"}},
				"Color":  {Values: []string{"red", "green", "blue"}},
			},
			EnumTSName:       map[string]string{"Color": "Colour"},
			TSNameOverride:   map[string]string{"TriA": "CycleAlpha"},
			ValidatorsImport: "./v.js",
			BusImport:        "./b.js",
			SSEEvents: []wiregen.SSERegEntry{
				{EventType: "notification", TypeName: "Notification"},
				{EventType: "user_update", TypeName: "User"},
			},
		}
	}

	ref := makeReg()
	typesRef := ref.GenerateTypes()
	decodersRef := ref.GenerateDecoders()
	registryRef := ref.GenerateRegistry()

	for i := range 100 {
		r := makeReg()
		if got := r.GenerateTypes(); got != typesRef {
			t.Fatalf("iteration %d: GenerateTypes differs", i)
		}
		if got := r.GenerateDecoders(); got != decodersRef {
			t.Fatalf("iteration %d: GenerateDecoders differs", i)
		}
		if got := r.GenerateRegistry(); got != registryRef {
			t.Fatalf("iteration %d: GenerateRegistry differs", i)
		}
	}
}

// --- Verify round 1-2 fixes still hold ---

// Re-verify: visited-set cycles don't infinite loop
func TestRound1CycleFixStillHolds(t *testing.T) {
	// Self-embedding via pointer
	type Node struct {
		*Node
		Val string `json:"val"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Node]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "val: string;") {
		t.Errorf("expected val field, got:\n%s", out)
	}
}

// Re-verify: embedded interface skip
func TestRound1EmbeddedInterfaceFixStillHolds(t *testing.T) {
	type Writer interface{ Write([]byte) (int, error) }
	type HasWriter struct {
		Writer
		Name string `json:"name"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[HasWriter]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "name: string;") {
		t.Errorf("expected name field, got:\n%s", out)
	}
}

// Re-verify: json:"-," produces field named "-"
func TestRound2DashCommaFixStillHolds(t *testing.T) {
	type DC struct {
		Dash string `json:"-,"` //nolint:staticcheck // intentionally testing legacy json:"-," syntax
		Skip string `json:"-"`
		Name string `json:"name"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[DC]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "-: string;") {
		t.Errorf("json:\"-,\" should produce field named '-', got:\n%s", out)
	}
	if strings.Contains(out, "Skip") {
		t.Errorf("json:\"-\" field should be skipped, got:\n%s", out)
	}
}

// --- Enum in optional field ---

type OptionalEnum struct {
	Status *Status `json:"status,omitempty"`
}

func TestOptionalEnumField(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[OptionalEnum]()},
		Enums:            map[string]wiregen.EnumDef{"Status": {Values: []string{"active", "inactive"}}},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "status?: Status;") {
		t.Errorf("expected optional Status field, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "reqOneOf") {
		t.Errorf("expected reqOneOf for optional enum, got:\n%s", dec)
	}
}

// --- Empty struct ---

type Empty struct{}

func TestEmptyStruct(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Empty]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface Empty {\n}\n") {
		t.Errorf("expected empty interface, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeEmpty") {
		t.Errorf("expected decodeEmpty, got:\n%s", dec)
	}
}

// --- Struct with only optional fields ---

type AllOptional struct {
	A *string `json:"a,omitempty"`
	B *int    `json:"b,omitempty"`
	C *bool   `json:"c,omitempty"`
}

func TestAllOptionalFields(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[AllOptional]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "a?: string;") {
		t.Errorf("expected a?: string, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeAllOptional") {
		t.Errorf("expected decoder, got:\n%s", dec)
	}
}

// --- Slice of pointers to structs ---

type SlicePtrStruct struct {
	Items []*Address `json:"items"`
}

func TestSliceOfPtrToStruct(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[SlicePtrStruct](), reflect.TypeFor[Address]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "items: Address[];") {
		t.Errorf("expected items: Address[], got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeAddress") {
		t.Errorf("expected decodeAddress in slice decoder, got:\n%s", dec)
	}
}

// --- Map with struct values (required field) ---

type MapStructRequired struct {
	Data map[string]Address `json:"data"`
}

func TestMapStructRequired(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[MapStructRequired](), reflect.TypeFor[Address]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "data?: Record<string, Address>;") {
		t.Errorf("expected Record<string, Address>, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeRecord") {
		t.Errorf("expected decodeRecord, got:\n%s", dec)
	}
}

// --- PathNameOverride ---

func TestPathNameOverride(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Address]()},
		PathNameOverride: map[string]string{"Address": "custom_path"},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "$.custom_path") {
		t.Errorf("expected custom path in decoder, got:\n%s", dec)
	}
}

// --- HeaderComment override ---

func TestHeaderCommentOverride(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithHeaderComment("// CUSTOM HEADER\n\n"),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	out := r.GenerateTypes()
	if !strings.Contains(out, "// CUSTOM HEADER") {
		t.Errorf("expected custom header, got:\n%s", out)
	}
}
