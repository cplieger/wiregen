package wiregen_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
)

// =============================================================================
// Round 7: Post-refactor red-team round 2
// Verify round-1 fix (NewRegistry(nil) no panic), then final sweep:
// - WithFilenames partial empty strings
// - WithX with empty/zero values
// - NewRegistry vs zero-value parity across many payloads
// - Determinism
// - Empty-ident guard
// - Recursion guard
// - No hangs (enforced by test timeout)
// =============================================================================

// --- VERIFY ROUND-1 FIX: NewRegistry(nil) ---

func TestR7_NewRegistryNilNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewRegistry(nil) panicked: %v", r)
		}
	}()
	r := wiregen.NewRegistry(nil)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.ValidatorsImport = "./v.js"
	_ = r.GenerateTypes()
}

func TestR7_NewRegistryMultipleNilsNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewRegistry with multiple nils panicked: %v", r)
		}
	}()
	r := wiregen.NewRegistry(nil, nil, nil, wiregen.WithValidatorsImport("./v.js"), nil)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	_ = r.GenerateTypes()
	_ = r.GenerateDecoders()
}

// --- WithFilenames: partial empty strings ---

func TestR7_WithFilenames_AllEmpty(t *testing.T) {
	// All empty strings should keep ALL defaults
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithFilenames("", "", "", ""),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	r.Constants = []wiregen.WireConst{{TSName: "X", Value: 1}}
	dir := t.TempDir()
	if err := r.Generate(dir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	for _, f := range []string{"types.gen.ts", "decoders.gen.ts", "registry.gen.ts", "constants.gen.ts"} {
		if _, err := stat(dir + "/" + f); err != nil {
			t.Errorf("expected default file %s: %v", f, err)
		}
	}
}

func TestR7_WithFilenames_OnlyDecoders(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithFilenames("", "custom_dec.ts", "", ""),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	dir := t.TempDir()
	if err := r.Generate(dir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := stat(dir + "/types.gen.ts"); err != nil {
		t.Errorf("types.gen.ts should use default: %v", err)
	}
	if _, err := stat(dir + "/custom_dec.ts"); err != nil {
		t.Errorf("custom_dec.ts should exist: %v", err)
	}
	if _, err := stat(dir + "/registry.gen.ts"); err != nil {
		t.Errorf("registry.gen.ts should use default: %v", err)
	}
}

func TestR7_WithFilenames_OnlyConstants(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithFilenames("", "", "", "my_const.ts"),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	r.Constants = []wiregen.WireConst{{TSName: "V", Value: 42}}
	dir := t.TempDir()
	if err := r.Generate(dir); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if _, err := stat(dir + "/my_const.ts"); err != nil {
		t.Errorf("my_const.ts should exist: %v", err)
	}
	if _, err := stat(dir + "/types.gen.ts"); err != nil {
		t.Errorf("types.gen.ts should use default: %v", err)
	}
}

// --- WithX with empty/zero values ---

func TestR7_WithValidatorsImport_Empty(t *testing.T) {
	// Empty string should effectively clear it; GenerateDecoders should panic
	// because ValidatorsImport == ""
	r := wiregen.NewRegistry(wiregen.WithValidatorsImport(""))
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("expected panic when ValidatorsImport is empty")
		}
	}()
	_ = r.GenerateDecoders()
}

func TestR7_WithBusImport_Empty(t *testing.T) {
	// Empty BusImport + SelfContainedRegistry=false → should panic on GenerateRegistry
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport(""),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("expected panic when BusImport empty and SelfContainedRegistry false")
		}
	}()
	_ = r.GenerateRegistry()
}

func TestR7_WithHeaderComment_Empty(t *testing.T) {
	// Empty header → init() applies default header
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithHeaderComment(""),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	types := r.GenerateTypes()
	// Empty string means the option didn't set it, so init() default kicks in
	if !strings.HasPrefix(types, "// CODE-GENERATED by wiregen, DO NOT EDIT.\n\n") {
		t.Errorf("empty WithHeaderComment should fall back to default, got: %q", types[:min(80, len(types))])
	}
}

func TestR7_WithRegisterFuncName_Empty(t *testing.T) {
	// Empty → init() default should apply
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithRegisterFuncName(""),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	reg := r.GenerateRegistry()
	if !strings.Contains(reg, "registerSSEDecoder") {
		t.Errorf("empty WithRegisterFuncName should default to registerSSEDecoder, got:\n%s", reg)
	}
}

func TestR7_WithRegistryFuncName_Empty(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithRegistryFuncName(""),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	reg := r.GenerateRegistry()
	if !strings.Contains(reg, "export function registerAllSSEDecoders()") {
		t.Errorf("empty WithRegistryFuncName should default, got:\n%s", reg)
	}
}

func TestR7_WithTypesImportPath_Empty(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithTypesImportPath(""),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[User]()}
	r.Enums = map[string]wiregen.EnumDef{"Status": {Values: []string{"active"}}}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, `from "./types.gen.js"`) {
		t.Errorf("empty WithTypesImportPath should default to ./types.gen.js, got:\n%s", dec)
	}
}

// --- NewRegistry vs zero-value parity with many payloads ---

func TestR7_Parity_ManyPayloads(t *testing.T) {
	type Inner struct {
		Val string `json:"val"`
	}
	type Complex struct {
		Inner   Inner    `json:"inner"`
		Tags    []string `json:"tags,omitempty"`
		Score   float64  `json:"score"`
		Enabled bool     `json:"enabled"`
	}

	payloads := []struct {
		name  string
		setup func(r *wiregen.Registry)
	}{
		{
			name: "simple_struct",
			setup: func(r *wiregen.Registry) {
				r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
				r.ValidatorsImport = "./v.js"
				r.BusImport = "./b.js"
				r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
			},
		},
		{
			name: "with_enums",
			setup: func(r *wiregen.Registry) {
				r.WireTypes = []reflect.Type{reflect.TypeFor[User]()}
				r.Enums = map[string]wiregen.EnumDef{"Status": {Values: []string{"active", "banned"}}}
				r.ValidatorsImport = "./v.js"
				r.BusImport = "./b.js"
				r.SSEEvents = []wiregen.SSERegEntry{{EventType: "u", TypeName: "User"}}
			},
		},
		{
			name: "nested_struct",
			setup: func(r *wiregen.Registry) {
				r.WireTypes = []reflect.Type{reflect.TypeFor[Inner](), reflect.TypeFor[Complex]()}
				r.ValidatorsImport = "./v.js"
				r.BusImport = "./b.js"
				r.SSEEvents = []wiregen.SSERegEntry{{EventType: "c", TypeName: "Complex"}}
			},
		},
		{
			name: "multiple_events",
			setup: func(r *wiregen.Registry) {
				r.WireTypes = []reflect.Type{reflect.TypeFor[Address](), reflect.TypeFor[Notification]()}
				r.ValidatorsImport = "./v.js"
				r.BusImport = "./b.js"
				r.SSEEvents = []wiregen.SSERegEntry{
					{EventType: "addr", TypeName: "Address"},
					{EventType: "notif", TypeName: "Notification"},
				}
			},
		},
		{
			name: "self_contained",
			setup: func(r *wiregen.Registry) {
				r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
				r.ValidatorsImport = "./v.js"
				r.SelfContainedRegistry = true
				r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
			},
		},
	}

	for _, p := range payloads {
		t.Run(p.name, func(t *testing.T) {
			zv := &wiregen.Registry{}
			p.setup(zv)
			nr := wiregen.NewRegistry()
			p.setup(nr)

			if zv.GenerateTypes() != nr.GenerateTypes() {
				t.Error("GenerateTypes differs")
			}
			if zv.GenerateDecoders() != nr.GenerateDecoders() {
				t.Error("GenerateDecoders differs")
			}
			if zv.GenerateRegistry() != nr.GenerateRegistry() {
				t.Error("GenerateRegistry differs")
			}
		})
	}
}

// --- Determinism with stress ---

func TestR7_Determinism500(t *testing.T) {
	mk := func() *wiregen.Registry {
		r := wiregen.NewRegistry(
			wiregen.WithValidatorsImport("./v.js"),
			wiregen.WithBusImport("./b.js"),
		)
		r.WireTypes = []reflect.Type{
			reflect.TypeFor[Address](),
			reflect.TypeFor[User](),
			reflect.TypeFor[Notification](),
			reflect.TypeFor[HasBytes](),
			reflect.TypeFor[HasJSONString](),
		}
		r.Enums = map[string]wiregen.EnumDef{
			"Status": {Values: []string{"active", "inactive", "banned"}},
		}
		r.SSEEvents = []wiregen.SSERegEntry{
			{EventType: "notif", TypeName: "Notification"},
			{EventType: "user", TypeName: "User"},
			{EventType: "addr", TypeName: "Address"},
		}
		r.Constants = []wiregen.WireConst{
			{TSName: "MAX_RETRY", Value: 5},
			{TSName: "TIMEOUT_MS", Value: 3000},
		}
		return r
	}

	ref := mk()
	tRef := ref.GenerateTypes()
	dRef := ref.GenerateDecoders()
	rRef := ref.GenerateRegistry()
	cRef := ref.GenerateConstants()

	for i := range 500 {
		r := mk()
		if r.GenerateTypes() != tRef {
			t.Fatalf("iter %d: types differ", i)
		}
		if r.GenerateDecoders() != dRef {
			t.Fatalf("iter %d: decoders differ", i)
		}
		if r.GenerateRegistry() != rRef {
			t.Fatalf("iter %d: registry differ", i)
		}
		if r.GenerateConstants() != cRef {
			t.Fatalf("iter %d: constants differ", i)
		}
	}
}

// --- Empty-ident guard ---

func TestR7_EmptyIdentInTypeMapping(t *testing.T) {
	// TypeMappings with a type that maps to "" — should not infinite loop
	type Foo struct {
		X string `json:"x"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Foo]()},
		TypeMappings:     map[reflect.Type]string{reflect.TypeFor[Foo](): ""},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not hang
	_ = r.GenerateTypes()
	_ = r.GenerateDecoders()
}

func TestR7_EmptyEnumTSName(t *testing.T) {
	// EnumTSName maps to "" — isIdentReferenced("", ...) should be safe
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[User]()},
		Enums:            map[string]wiregen.EnumDef{"Status": {Values: []string{"a"}}},
		EnumTSName:       map[string]string{"Status": ""},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	// Must not hang or panic
	_ = r.GenerateDecoders()
}

// --- Recursion guard ---

func TestR7_DeepEmbeddingChain(t *testing.T) {
	type L3 struct {
		V string `json:"v"`
	}
	type L2 struct {
		L3
	}
	type L1 struct {
		L2
	}
	type L0 struct {
		L1
		Name string `json:"name"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[L0]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "v: string;") {
		t.Errorf("deeply embedded field v missing, got:\n%s", out)
	}
	if !strings.Contains(out, "name: string;") {
		t.Errorf("direct field name missing, got:\n%s", out)
	}
}

func TestR7_SelfReferentialPtrField(t *testing.T) {
	// Not embedding — a *Self field; should just be unknown since Self isn't WireTypes-registered
	type TreeNode struct {
		Children []*TreeNode `json:"children,omitempty"`
		Label    string      `json:"label"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[TreeNode]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "label: string;") {
		t.Errorf("label field missing, got:\n%s", out)
	}
	// children should appear (as TreeNode[] since it IS in WireTypes)
	if !strings.Contains(out, "children?:") {
		t.Errorf("children field missing, got:\n%s", out)
	}
}

func TestR7_MutualRecursionViaPtr(t *testing.T) {
	// Embedded mutual recursion via pointer
	type Alpha struct {
		Beta *struct {
			X int `json:"x"`
		} `json:"beta,omitempty"`
		Y string `json:"y"`
	}
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{reflect.TypeFor[Alpha]()},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "y: string;") {
		t.Errorf("field y missing in mutual recursion test, got:\n%s", out)
	}
}

// --- No hangs: large payloads ---

func TestR7_LargePayload_NoHang(t *testing.T) {
	// 20-field struct, ensure no perf regression
	fields := make([]reflect.StructField, 20)
	for i := range 20 {
		fields[i] = reflect.StructField{
			Name: "Field" + string(rune('A'+i)),
			Type: reflect.TypeFor[string](),
			Tag:  reflect.StructTag(`json:"field_` + string(rune('a'+i)) + `"`),
		}
	}
	bigType := reflect.StructOf(fields)
	r := &wiregen.Registry{
		WireTypes:        []reflect.Type{bigType},
		ValidatorsImport: "./v.js",
		BusImport:        "./b.js",
	}
	out := r.GenerateTypes()
	if !strings.Contains(out, "field_a: string;") {
		t.Errorf("first field missing in large payload test")
	}
	if !strings.Contains(out, "field_t: string;") {
		t.Errorf("last field missing in large payload test")
	}
	_ = r.GenerateDecoders()
}

// --- WithSelfContainedRegistry with empty ValidatorsImport (regression) ---

func TestR7_SelfContainedRegistry_EmptyValidators_Panics(t *testing.T) {
	// SelfContained registry needs ValidatorsImport for the Decoder type import.
	// Without it, should panic rather than emit invalid TS.
	r := wiregen.NewRegistry(wiregen.WithSelfContainedRegistry(true))
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("expected panic when ValidatorsImport empty and SelfContainedRegistry true")
		}
	}()
	_ = r.GenerateRegistry()
}

func TestR7_SelfContainedRegistry_WithValidators_OK(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithSelfContainedRegistry(true),
		wiregen.WithValidatorsImport("./v.js"),
	)
	r.WireTypes = []reflect.Type{reflect.TypeFor[Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	reg := r.GenerateRegistry()
	if !strings.Contains(reg, `from "./v.js"`) {
		t.Errorf("SelfContainedRegistry should reference ValidatorsImport, got:\n%s", reg)
	}
}

// --- Verify NewRegistry(nil) generates identical to NewRegistry() ---

func TestR7_NewRegistryNil_ParityWithNoArgs(t *testing.T) {
	setup := func(r *wiregen.Registry) {
		r.WireTypes = []reflect.Type{reflect.TypeFor[Address](), reflect.TypeFor[User]()}
		r.Enums = map[string]wiregen.EnumDef{"Status": {Values: []string{"active"}}}
		r.ValidatorsImport = "./v.js"
		r.BusImport = "./b.js"
		r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	}

	r1 := wiregen.NewRegistry()
	setup(r1)
	r2 := wiregen.NewRegistry(nil)
	setup(r2)

	if r1.GenerateTypes() != r2.GenerateTypes() {
		t.Error("NewRegistry() vs NewRegistry(nil): GenerateTypes differs")
	}
	if r1.GenerateDecoders() != r2.GenerateDecoders() {
		t.Error("NewRegistry() vs NewRegistry(nil): GenerateDecoders differs")
	}
	if r1.GenerateRegistry() != r2.GenerateRegistry() {
		t.Error("NewRegistry() vs NewRegistry(nil): GenerateRegistry differs")
	}
}

// --- helper ---

func stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
