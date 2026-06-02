package wiregen_test

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cplieger/wiregen"
)

// =============================================================================
// Round 8: Post-refactor convergence red-team (round 3)
// Final sweep of attack surfaces: options composition, concurrent generation,
// adversarial isIdentReferenced inputs, empty enum values, non-registered SSE
// types, duplicate WireTypes, WithSelfContainedRegistry(false), option override
// ordering.
// =============================================================================

// --- Options composition & ordering ---

func TestR8_OptionsOverrideOrder(t *testing.T) {
	// Later options override earlier ones
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./first.js"),
		wiregen.WithValidatorsImport("./second.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, `from "./second.js"`) {
		t.Errorf("last WithValidatorsImport should win, got:\n%s", dec)
	}
	if strings.Contains(dec, `from "./first.js"`) {
		t.Errorf("first WithValidatorsImport should be overridden")
	}
}

func TestR8_OptionsNilIntermixed(t *testing.T) {
	// Nils mixed between real options should not disrupt
	r := wiregen.NewRegistry(
		nil,
		wiregen.WithValidatorsImport("./v.js"),
		nil,
		wiregen.WithBusImport("./b.js"),
		nil,
		wiregen.WithHeaderComment("// custom\n\n"),
		nil,
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	types := r.GenerateTypes()
	if !strings.HasPrefix(types, "// custom\n\n") {
		t.Errorf("HeaderComment not applied with nils intermixed, got: %q", types[:min(40, len(types))])
	}
	reg := r.GenerateRegistry()
	if !strings.Contains(reg, `from "./b.js"`) {
		t.Errorf("BusImport not applied with nils intermixed")
	}
}

func TestR8_WithSelfContainedRegistryFalse(t *testing.T) {
	// Explicitly passing false should behave same as default
	r := wiregen.NewRegistry(
		wiregen.WithSelfContainedRegistry(false),
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	reg := r.GenerateRegistry()
	if strings.Contains(reg, "new Map") {
		t.Errorf("SelfContainedRegistry(false) should not use Map pattern")
	}
	if !strings.Contains(reg, "registerSSEDecoder") {
		t.Errorf("SelfContainedRegistry(false) should use bus import pattern")
	}
}

// --- Empty enum values slice ---

func TestR8_EnumWithEmptyValues(t *testing.T) {
	// An enum with no values — should not panic or infinite loop
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[User]()},
		Enums:            map[string]wiregen.EnumDef{"Status": {Values: []string{}}},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	types := r.GenerateTypes()
	// Should produce something like "export type Status = ;\n" — not ideal but must not panic
	_ = types
	dec := r.GenerateDecoders()
	_ = dec
}

func TestR8_EnumWithSingleValue(t *testing.T) {
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[User]()},
		Enums:            map[string]wiregen.EnumDef{"Status": {Values: []string{"only"}}},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	types := r.GenerateTypes()
	if !strings.Contains(types, `"only"`) {
		t.Errorf("single-value enum not emitted, got:\n%s", types)
	}
}

// --- SSEEvents referencing non-registered type ---

func TestR8_SSEEvent_NonRegisteredType(t *testing.T) {
	// TypeName references something not in WireTypes — should not panic
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Address]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
		SSEEvents:        []wiregen.SSERegEntry{{EventType: "ghost", TypeName: "NonExistent"}},
	}
	// GenerateRegistry builds decoder import names from TypeName; no panic expected
	reg := r.GenerateRegistry()
	if !strings.Contains(reg, "decodeNonExistent") {
		t.Errorf("expected decoder name for non-registered type, got:\n%s", reg)
	}
}

// --- Duplicate WireTypes ---

func TestR8_DuplicateWireTypes(t *testing.T) {
	// Same type registered multiple times — should not produce duplicate interfaces
	r := &wiregen.Registry{
		WireTypes: []reflect.Type{
			reflect.TypeFor[Address](),
			reflect.TypeFor[Address](),
			reflect.TypeFor[Address](),
		},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	types := r.GenerateTypes()
	count := strings.Count(types, "export interface Address {")
	if count != 1 {
		t.Errorf("expected exactly 1 Address interface, got %d", count)
	}
	dec := r.GenerateDecoders()
	decCount := strings.Count(dec, "export const decodeAddress")
	if decCount != 1 {
		t.Errorf("expected exactly 1 decodeAddress, got %d", decCount)
	}
}

// --- Concurrent generation (race detector surface) ---

func TestR8_ConcurrentGeneration(t *testing.T) {
	// Multiple goroutines calling Generate methods on separate registries
	// (sharing no state) — verifies no global mutable state causes races
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r := wiregen.NewRegistry(
				wiregen.WithValidatorsImport("./v.js"),
				wiregen.WithBusImport("./b.js"),
			)
			r.WireTypes = []reflect.Type{reflect.TypeFor[Address](), reflect.TypeFor[User]()}
			r.Enums = map[string]wiregen.EnumDef{"Status": {Values: []string{"a", "b"}}}
			r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
			r.Constants = []wiregen.WireConst{{TSName: "X", Value: idx}}
			_ = r.GenerateTypes()
			_ = r.GenerateDecoders()
			_ = r.GenerateRegistry()
			_ = r.GenerateConstants()
		}(i)
	}
	wg.Wait()
}

// --- isIdentReferenced adversarial inputs ---

func TestR8_TypeMappingToSubstringOfHelper(t *testing.T) {
	// TypeMapping value is "Str" — a substring of "reqStr". Should not cause
	// incorrect import inclusion or exclusion.
	type Tiny struct {
		X string `json:"x"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Tiny]()},
		TypeMappings:     map[reflect.Type]string{reflect.TypeFor[Tiny](): "Str"},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	dec := r.GenerateDecoders()
	// Must not panic or hang; "Str" as a mapped type name is valid
	_ = dec
}

func TestR8_EnumNameIsJSKeyword(t *testing.T) {
	// Enum name that looks like a JS keyword
	type export string //nolint:nolintlint // intentional lowercase for test
	type HasExport struct {
		Kind export `json:"kind"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[HasExport]()},
		Enums:            map[string]wiregen.EnumDef{"export": {Values: []string{"a", "b"}}},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Should not panic — the sanitizeVarName handles 'export' conflicts
	_ = r.GenerateTypes()
	_ = r.GenerateDecoders()
}

// --- Struct with only optional fields (empty required set) ---

func TestR8_AllOptionalFields(t *testing.T) {
	type AllOpt struct {
		A *string `json:"a,omitempty"`
		B *int    `json:"b,omitempty"`
		C *bool   `json:"c,omitempty"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[AllOpt]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	types := r.GenerateTypes()
	if !strings.Contains(types, "a?: string;") {
		t.Errorf("optional field a missing, got:\n%s", types)
	}
	dec := r.GenerateDecoders()
	// All-optional: the object literal is empty (may have whitespace formatting)
	if !strings.Contains(dec, "const out: AllOpt = {") {
		t.Errorf("all-optional struct should have initial object, got:\n%s", dec)
	}
	// The optional fields should still be decoded
	if !strings.Contains(dec, "optStr") || !strings.Contains(dec, "optNum") || !strings.Contains(dec, "optBool") {
		t.Errorf("all-optional struct should use opt helpers, got:\n%s", dec)
	}
}

// --- Struct with no fields ---

func TestR8_EmptyStruct(t *testing.T) {
	type Empty struct{}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Empty]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	types := r.GenerateTypes()
	if !strings.Contains(types, "export interface Empty {\n}\n") {
		t.Errorf("empty struct should produce empty interface, got:\n%s", types)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "const out: Empty = {};") {
		t.Errorf("empty struct decoder should produce empty object, got:\n%s", dec)
	}
}

// --- Option applied after non-nil: verify no stale defaults ---

func TestR8_OptionOverrideDefaults(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithHeaderComment("// mine\n\n"),
		wiregen.WithRegisterFuncName("myRegister"),
		wiregen.WithRegistryFuncName("myInit"),
		wiregen.WithTypesImportPath("./my-types.js"),
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}

	types := r.GenerateTypes()
	if !strings.HasPrefix(types, "// mine\n\n") {
		t.Errorf("custom header not applied")
	}

	dec := r.GenerateDecoders()
	if !strings.Contains(dec, `from "./my-types.js"`) {
		t.Errorf("custom types import path not applied, got:\n%s", dec)
	}

	reg := r.GenerateRegistry()
	if !strings.Contains(reg, "myRegister") {
		t.Errorf("custom register func name not applied")
	}
	if !strings.Contains(reg, "export function myInit()") {
		t.Errorf("custom registry func name not applied")
	}
}

// --- Map field with pointer values ---

func TestR8_MapWithPointerValues(t *testing.T) {
	type WithMap struct {
		Data map[string]*int `json:"data,omitempty"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[WithMap]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	types := r.GenerateTypes()
	if !strings.Contains(types, "data?: Record<string, number>;") {
		t.Errorf("map[string]*int should produce Record<string, number>, got:\n%s", types)
	}
	dec := r.GenerateDecoders()
	// Should reference decodeRecord
	if !strings.Contains(dec, "decodeRecord") {
		t.Errorf("map field should use decodeRecord, got:\n%s", dec)
	}
}

// --- Slice of pointers to structs ---

func TestR8_SliceOfPointerToRegisteredStruct(t *testing.T) {
	type Inner struct {
		V string `json:"v"`
	}
	type Outer struct {
		Items []*Inner `json:"items"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Inner](), reflect.TypeFor[Outer]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	types := r.GenerateTypes()
	if !strings.Contains(types, "items: Inner[];") {
		t.Errorf("slice of ptr to registered struct should produce Inner[], got:\n%s", types)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeInner") {
		t.Errorf("should use decodeInner for slice elements, got:\n%s", dec)
	}
}
