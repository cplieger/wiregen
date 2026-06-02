package wiregen_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
)

// --- Cycle shapes: mutual recursion A->B->A ---

type CycleA struct {
	Name string  `json:"name"`
	B    *CycleB `json:"b,omitempty"`
}

type CycleB struct {
	Value string  `json:"value"`
	A     *CycleA `json:"a,omitempty"`
}

func TestMutualRecursion(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[CycleA](), reflect.TypeFor[CycleB]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not infinite-loop
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface CycleA") {
		t.Errorf("missing CycleA, got:\n%s", out)
	}
	if !strings.Contains(out, "export interface CycleB") {
		t.Errorf("missing CycleB, got:\n%s", out)
	}
}

// --- Self via slice ---

type SelfSlice struct {
	Children []SelfSlice `json:"children"`
	Name     string      `json:"name"`
}

func TestSelfViaSlice(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[SelfSlice]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "children: SelfSlice[];") {
		t.Errorf("expected self-referencing slice field, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeSelfSlice") {
		t.Errorf("expected decoder for SelfSlice, got:\n%s", dec)
	}
}

// --- Self via map ---

type SelfMap struct {
	Nested map[string]SelfMap `json:"nested,omitempty"`
	Name   string             `json:"name"`
}

func TestSelfViaMap(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[SelfMap]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "nested?: Record<string, SelfMap>;") {
		t.Errorf("expected self-referencing map field, got:\n%s", out)
	}
}

// --- json:"-," (field named literally "-") ---

type DashCommaField struct {
	Dash string `json:"-,"` //nolint:staticcheck // intentionally testing legacy json:"-," syntax
	Name string `json:"name"`
	Skip string `json:"-"`
}

func TestJsonDashComma(t *testing.T) {
	// json:"-," means the field is named "-" (not skipped)
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[DashCommaField]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	// The field with tag `json:"-,"` should appear with wire name "-"
	if !strings.Contains(out, "-: string;") {
		t.Errorf("json:\"-,\" should produce field named '-', got:\n%s", out)
	}
	// The field with tag `json:"-"` should be skipped
	if strings.Contains(out, "Skip") {
		t.Errorf("json:\"-\" field should be skipped, got:\n%s", out)
	}
}

// --- Embedded interface ---

type Stringer interface {
	String() string
}

type HasEmbeddedInterface struct {
	Stringer
	Name string `json:"name"`
}

func TestEmbeddedInterface(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[HasEmbeddedInterface]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not panic
	out := r.GenerateTypes()
	if !strings.Contains(out, "name: string;") {
		t.Errorf("expected name field, got:\n%s", out)
	}
}

// --- Field named same as type ---

type Foo struct {
	Foo string `json:"Foo"`
}

func TestFieldNamedSameAsType(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Foo]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface Foo") {
		t.Errorf("expected Foo interface, got:\n%s", out)
	}
	if !strings.Contains(out, "Foo: string;") {
		t.Errorf("expected Foo field, got:\n%s", out)
	}
}

// --- Reserved TS keywords as field names ---

type ReservedFields struct {
	Class   string `json:"class"`
	Delete  string `json:"delete"`
	Export  string `json:"export"`
	Import  string `json:"import"`
	Return  string `json:"return"`
	Default string `json:"default"`
}

func TestReservedTSKeywordsAsFieldNames(t *testing.T) {
	// TS interfaces allow reserved words as property names, so this should work fine
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[ReservedFields]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	for _, kw := range []string{"class", "delete", "export", "import", "return", "default"} {
		if !strings.Contains(out, kw+": string;") {
			t.Errorf("expected field %q, got:\n%s", kw, out)
		}
	}
	// Decoders: the sanitizeVarName should handle reserved words in variable names
	dec := r.GenerateDecoders()
	if dec == "" {
		t.Fatal("empty decoder output")
	}
}

// --- Decoder correctness for map values ---

type MapOfStructs struct {
	Items map[string]Address `json:"items,omitempty"`
}

func TestDecoderMapOfStructs(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[MapOfStructs](), reflect.TypeFor[Address]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	dec := r.GenerateDecoders()
	// Map of structs should use decodeRecord with the struct decoder
	if !strings.Contains(dec, "decodeRecord") {
		t.Errorf("expected decodeRecord for map field, got:\n%s", dec)
	}
	if !strings.Contains(dec, "decodeAddress") {
		t.Errorf("expected decodeAddress as element decoder for map, got:\n%s", dec)
	}
}

// --- Nested optional pointer to struct ---

type Inner struct {
	X int `json:"x"`
}

type Outer struct {
	Inner *Inner `json:"inner,omitempty"`
}

func TestNestedOptionalPointerStruct(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Outer](), reflect.TypeFor[Inner]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "inner?: Inner;") {
		t.Errorf("expected optional Inner field, got:\n%s", out)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeInner(o[\"inner\"])") {
		t.Errorf("expected decodeInner call for optional struct, got:\n%s", dec)
	}
}

// --- Duplicate TS names via TSNameOverride collision ---

type TypeA struct {
	A string `json:"a"`
}

type TypeB struct {
	B string `json:"b"`
}

func TestTSNameOverrideCollision(t *testing.T) {
	// If two Go types map to the same TS name, we get duplicate declarations
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[TypeA](), reflect.TypeFor[TypeB]()},
		TSNameOverride:   map[string]string{"TypeA": "Shared", "TypeB": "Shared"},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	// Count occurrences of "export interface Shared"
	count := strings.Count(out, "export interface Shared")
	if count > 1 {
		t.Errorf("duplicate TS interface declarations (count=%d), got:\n%s", count, out)
	}
}

// --- sanitizeVarName collision: two fields that sanitize to same var ---

type VarNameCollision struct {
	FooBar  string `json:"foo_bar,omitempty"`
	FooBar2 string `json:"foo_bar_2,omitempty"` // different wire name
}

func TestSanitizeVarNameNoCollision(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[VarNameCollision]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	dec := r.GenerateDecoders()
	// Both fields should appear in decoder
	if !strings.Contains(dec, "foo_bar") {
		t.Errorf("expected foo_bar field in decoder, got:\n%s", dec)
	}
	if !strings.Contains(dec, "foo_bar_2") {
		t.Errorf("expected foo_bar_2 field in decoder, got:\n%s", dec)
	}
}

// --- Deep nesting (stack overflow check) ---

// We can't easily create 1000-deep struct nesting at compile time,
// but we can test that the visited set doesn't cause issues with
// multiple levels of embedding.

type Level1 struct {
	Level2
	L1 string `json:"l1"`
}

type Level2 struct {
	Level3
	L2 string `json:"l2"`
}

type Level3 struct {
	Level4
	L3 string `json:"l3"`
}

type Level4 struct {
	L4 string `json:"l4"`
}

func TestDeepEmbedding(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Level1]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	for _, f := range []string{"l1", "l2", "l3", "l4"} {
		if !strings.Contains(out, f+": string;") {
			t.Errorf("expected field %q from deep embedding, got:\n%s", f, out)
		}
	}
}

// --- Pointer to slice of pointers ---

type PtrSlicePtr struct {
	Items *[]*Address `json:"items,omitempty"`
}

func TestPointerToSliceOfPointers(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[PtrSlicePtr](), reflect.TypeFor[Address]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	// *[]*Address should become Address[]
	if !strings.Contains(out, "items?: Address[];") {
		t.Errorf("expected items?: Address[], got:\n%s", out)
	}
}
