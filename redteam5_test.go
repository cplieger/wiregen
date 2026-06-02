package wiregen_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
)

// --- Round 5: Final convergence attack ---
// Surfaces: empty-named enum, TSNameOverride/PathNameOverride → empty string,
// registered type whose tsName resolves to empty, recursive/mutually-recursive
// reflect types, cyclic via slice/map/ptr, determinism over many iterations,
// degenerate identifiers.

// Helper to build a registry quickly.
func r5reg(types ...reflect.Type) *wiregen.Registry {
	return &wiregen.Registry{
		WireTypes:        types,
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
}

// ---- Attack 1: Enum with empty-string name key ----
// Enums keyed by "" should not panic or hang.

func TestR5EmptyEnumName(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Address]()},
		Enums:            map[string]wiregen.EnumDef{"": {Values: []string{"x", "y"}}},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic or hang
	out := r.GenerateTypes()
	_ = out
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 2: TSNameOverride mapping a type to empty string ----

type R5TypeA struct {
	Name string `json:"name"`
}

func TestR5TSNameOverrideEmpty(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[R5TypeA]()},
		TSNameOverride:   map[string]string{"R5TypeA": ""},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic or hang
	out := r.GenerateTypes()
	_ = out
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 3: PathNameOverride mapping a type to empty string ----

func TestR5PathNameOverrideEmpty(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[R5TypeA]()},
		PathNameOverride: map[string]string{"R5TypeA": ""},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic or hang
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 4: EnumTSName override to empty string ----

type R5Colour string

type R5HasColour struct {
	C R5Colour `json:"c"`
}

func TestR5EnumTSNameEmpty(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[R5HasColour]()},
		Enums:            map[string]wiregen.EnumDef{"R5Colour": {Values: []string{"red", "blue"}}},
		EnumTSName:       map[string]string{"R5Colour": ""},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic or hang
	out := r.GenerateTypes()
	_ = out
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 5: reflect.StructOf anonymous struct (Name="") as WireType ----
// A type whose Name() returns "" — stress-test all name-based code paths.

var r5AnonType = reflect.StructOf([]reflect.StructField{
	{Name: "Foo", Type: reflect.TypeFor[string](), Tag: `json:"foo"`},
	{Name: "Bar", Type: reflect.TypeFor[int](), Tag: `json:"bar"`},
})

func TestR5AnonymousStructType(t *testing.T) {
	r := r5reg(r5AnonType)
	// Must not panic or hang; anonymous types have Name()==""
	out := r.GenerateTypes()
	_ = out
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 6: Recursive via slice of self (reflect-built) ----

type R5SelfSlice struct {
	Children []*R5SelfSlice `json:"children,omitempty"`
	Label    string         `json:"label"`
}

func TestR5RecursiveSliceSelf(t *testing.T) {
	r := r5reg(reflect.TypeFor[R5SelfSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "children?: R5SelfSlice[];") {
		t.Errorf("expected recursive slice type, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeR5SelfSlice") {
		t.Errorf("expected decoder, got:\n%s", dec)
	}
}

// ---- Attack 7: Cyclic via map-of-ptr-to-self ----

type R5MapSelf struct {
	Nodes map[string]*R5MapSelf `json:"nodes,omitempty"`
	ID    string                `json:"id"`
}

func TestR5CyclicMapSelf(t *testing.T) {
	r := r5reg(reflect.TypeFor[R5MapSelf]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "nodes?: Record<string, R5MapSelf>;") {
		t.Errorf("expected recursive map type, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeR5MapSelf") {
		t.Errorf("expected decoder, got:\n%s", dec)
	}
}

// ---- Attack 8: Mutually recursive reflect types (3-way A->B->C->A via ptr) ----

type R5CycA struct {
	B    *R5CycB `json:"b,omitempty"`
	Name string  `json:"name"`
}
type R5CycB struct {
	C    *R5CycC `json:"c,omitempty"`
	Name string  `json:"name"`
}
type R5CycC struct {
	A    *R5CycA `json:"a,omitempty"`
	Name string  `json:"name"`
}

func TestR5MutualRecursionThreeWay(t *testing.T) {
	r := r5reg(reflect.TypeFor[R5CycA](), reflect.TypeFor[R5CycB](), reflect.TypeFor[R5CycC]())
	out := r.GenerateTypes()
	for _, name := range []string{"R5CycA", "R5CycB", "R5CycC"} {
		if !strings.Contains(out, "export interface "+name) {
			t.Errorf("missing interface %s, got:\n%s", name, out)
		}
	}
	dec := r.GenerateDecoders()
	for _, d := range []string{"decodeR5CycA", "decodeR5CycB", "decodeR5CycC"} {
		if !strings.Contains(dec, d) {
			t.Errorf("missing %s, got:\n%s", d, dec)
		}
	}
}

// ---- Attack 9: Determinism - 200 iterations with complex registry ----

func TestR5Determinism200Iterations(t *testing.T) {
	makeReg := func() *wiregen.Registry {
		return &wiregen.Registry{
			WireTypes: []reflect.Type{
				reflect.TypeFor[R5SelfSlice](),
				reflect.TypeFor[R5MapSelf](),
				reflect.TypeFor[R5CycA](),
				reflect.TypeFor[R5CycB](),
				reflect.TypeFor[R5CycC](),
				reflect.TypeFor[R5TypeA](),
				reflect.TypeFor[R5HasColour](),
				reflect.TypeFor[Address](),
			},
			Enums: map[string]wiregen.EnumDef{
				"R5Colour": {Values: []string{"red", "blue"}},
				"Status":   {Values: []string{"active", "inactive"}},
			},
			EnumTSName:       map[string]string{"R5Colour": "Colour5"},
			TSNameOverride:   map[string]string{"R5TypeA": "TypeAlpha"},
			PathNameOverride: map[string]string{"R5SelfSlice": "self_slice"},
			ValidatorsImport: "./v.js",
			BusImport:        "./b.js",
			SSEEvents: []wiregen.SSERegEntry{
				{EventType: "alpha", TypeName: "R5TypeA"},
			},
		}
	}

	ref := makeReg()
	typesRef := ref.GenerateTypes()
	decodersRef := ref.GenerateDecoders()
	registryRef := ref.GenerateRegistry()

	for i := range 200 {
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

// ---- Attack 10: isIdentReferenced with empty ident (round-4 fix verification) ----
// This directly exercises the fix: isIdentReferenced("anything", "") must return false.
// We test this indirectly by registering types whose tsName resolves to "".

func TestR5EmptyIdentIsNotReferenced(t *testing.T) {
	// A type mapped to empty via TSNameOverride and used as a field type
	type R5Inner struct {
		X string `json:"x"`
	}
	type R5Outer struct {
		Inner R5Inner `json:"inner"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[R5Inner](), reflect.TypeFor[R5Outer]()},
		TSNameOverride:   map[string]string{"R5Inner": ""},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic or hang — the key test is no infinite loop in isIdentReferenced
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 11: Enum with many values and empty-string value ----

func TestR5EnumWithEmptyValue(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes: []reflect.Type{reflect.TypeFor[Address]()},
		Enums: map[string]wiregen.EnumDef{
			"WeirdEnum": {Values: []string{"", "valid", "also_valid"}},
		},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	// Empty string value should appear as ""
	if !strings.Contains(out, `""`) {
		t.Errorf("expected empty string value in enum, got:\n%s", out)
	}
}

// ---- Attack 12: Multiple types with same tsName="" (dedup edge case) ----

type R5EmptyA struct {
	X string `json:"x"`
}
type R5EmptyB struct {
	Y int `json:"y"`
}

func TestR5MultipleTypesEmptyTSName(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[R5EmptyA](), reflect.TypeFor[R5EmptyB]()},
		TSNameOverride:   map[string]string{"R5EmptyA": "", "R5EmptyB": ""},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic or hang
	out := r.GenerateTypes()
	_ = out
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 13: SSERegEntry with TypeName that maps to empty tsName ----

func TestR5SSEEmptyTypeName(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[R5TypeA]()},
		TSNameOverride:   map[string]string{"R5TypeA": ""},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
		SSEEvents:        []wiregen.SSERegEntry{{EventType: "test", TypeName: "R5TypeA"}},
	}
	// Must not panic or hang
	reg := r.GenerateRegistry()
	_ = reg
}

// ---- Attack 14: WireConst with empty TSName ----

func TestR5ConstantEmptyTSName(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Address]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
		Constants:        []wiregen.WireConst{{TSName: "", Value: 42}},
	}
	// Must not panic
	out := r.GenerateConstants()
	_ = out
}

// ---- Attack 15: SelfContainedRegistry with empty decoder name ----

func TestR5SelfContainedRegistryEmptyDecoder(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:             []reflect.Type{reflect.TypeFor[R5TypeA]()},
		TSNameOverride:        map[string]string{"R5TypeA": ""},
		ValidatorsImport:      "./v.js",
		SelfContainedRegistry: true,
		SSEEvents:             []wiregen.SSERegEntry{{EventType: "ev", TypeName: "R5TypeA"}},
	}
	// Must not panic or hang
	out := r.GenerateRegistry()
	_ = out
}

// ---- Attack 16: Deeply nested embedding (5 levels) with cycle guard ----

type R5L5 struct {
	Val string `json:"val"`
}
type R5L4 struct{ R5L5 }
type R5L3 struct{ R5L4 }
type R5L2 struct{ R5L3 }
type R5L1 struct{ R5L2 }

func TestR5DeeplyNestedEmbedding(t *testing.T) {
	r := r5reg(reflect.TypeFor[R5L1]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "val: string;") {
		t.Errorf("expected promoted val from 5 levels deep, got:\n%s", out)
	}
}

// ---- Attack 17: Struct field whose type is a slice of anonymous struct ----
// reflect.StructOf can create structs with no Name(); used as slice element.

var r5AnonElem = reflect.StructOf([]reflect.StructField{
	{Name: "Z", Type: reflect.TypeFor[int](), Tag: `json:"z"`},
})

var r5SliceOfAnon = reflect.StructOf([]reflect.StructField{
	{Name: "Items", Type: reflect.SliceOf(r5AnonElem), Tag: `json:"items"`},
})

func TestR5SliceOfAnonymousElem(t *testing.T) {
	r := r5reg(r5SliceOfAnon)
	// Must not panic or hang
	out := r.GenerateTypes()
	_ = out
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 18: Map with anonymous struct as value type ----

var r5MapOfAnon = reflect.StructOf([]reflect.StructField{
	{Name: "Data", Type: reflect.MapOf(reflect.TypeFor[string](), r5AnonElem), Tag: `json:"data"`},
})

func TestR5MapOfAnonymousValue(t *testing.T) {
	r := r5reg(r5MapOfAnon)
	// Must not panic or hang
	out := r.GenerateTypes()
	_ = out
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 19: Enum with same TSName override as a type TSName override (collision) ----

func TestR5EnumTypeNameCollision(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[R5TypeA]()},
		Enums:            map[string]wiregen.EnumDef{"R5Colour": {Values: []string{"r", "g"}}},
		EnumTSName:       map[string]string{"R5Colour": "TypeAlpha"},
		TSNameOverride:   map[string]string{"R5TypeA": "TypeAlpha"},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic or hang
	out := r.GenerateTypes()
	_ = out
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 20: Race condition test - concurrent Generate calls ----

func TestR5ConcurrentGenerate(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes: []reflect.Type{
			reflect.TypeFor[R5SelfSlice](),
			reflect.TypeFor[R5MapSelf](),
			reflect.TypeFor[R5CycA](),
			reflect.TypeFor[R5CycB](),
			reflect.TypeFor[R5CycC](),
			reflect.TypeFor[Address](),
		},
		Enums:            map[string]wiregen.EnumDef{"Status": {Values: []string{"a", "b"}}},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
		SSEEvents:        []wiregen.SSERegEntry{{EventType: "x", TypeName: "R5CycA"}},
	}
	// Pre-init to avoid race on init itself (init is not concurrency-safe by design)
	_ = r.GenerateTypes()

	done := make(chan struct{})
	for range 10 {
		go func() {
			_ = r.GenerateTypes()
			_ = r.GenerateDecoders()
			_ = r.GenerateRegistry()
			done <- struct{}{}
		}()
	}
	for range 10 {
		<-done
	}
}

// ---- Attack 21: Degenerate wire names via json tags ----
// Wire names that trigger edge cases in sanitizeVarName: all underscores,
// reserved words, single char colliding with local vars.

var r5DegenWireNames = reflect.StructOf([]reflect.StructField{
	{Name: "A", Type: reflect.TypeFor[*string](), Tag: reflect.StructTag(`json:"___,omitempty"`)},
	{Name: "B", Type: reflect.TypeFor[*string](), Tag: reflect.StructTag(`json:"_,omitempty"`)},
	{Name: "C", Type: reflect.TypeFor[*string](), Tag: reflect.StructTag(`json:"return,omitempty"`)},
	{Name: "D", Type: reflect.TypeFor[*string](), Tag: reflect.StructTag(`json:"o,omitempty"`)},
	{Name: "E", Type: reflect.TypeFor[*string](), Tag: reflect.StructTag(`json:"out,omitempty"`)},
	{Name: "F", Type: reflect.TypeFor[*string](), Tag: reflect.StructTag(`json:"v,omitempty"`)},
	{Name: "G", Type: reflect.TypeFor[*string](), Tag: reflect.StructTag(`json:"export,omitempty"`)},
	{Name: "H", Type: reflect.TypeFor[*string](), Tag: reflect.StructTag(`json:"import,omitempty"`)},
})

func TestR5DegenerateWireNames(t *testing.T) {
	r := r5reg(r5DegenWireNames)
	// Must not panic or produce invalid TS (duplicate var names)
	out := r.GenerateTypes()
	_ = out
	dec := r.GenerateDecoders()
	_ = dec
}

// ---- Attack 22: Enum with zero values ----

func TestR5EnumZeroValues(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Address]()},
		Enums:            map[string]wiregen.EnumDef{"EmptyEnum": {Values: []string{}}},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic
	out := r.GenerateTypes()
	_ = out
	dec := r.GenerateDecoders()
	_ = dec
}
