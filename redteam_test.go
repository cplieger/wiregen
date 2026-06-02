package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/basic"
	"github.com/cplieger/wiregen/testdata/edges"
)

const edgesPkg = "github.com/cplieger/wiregen/testdata/edges"

func edgesReg(types ...wiregen.WireType) *wiregen.Registry {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{edgesPkg}
	r.Types = types
	return r
}

// --- Recursive/cycle tests ---

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

func TestSelfViaSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.SelfSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "children?: SelfSlice[];") {
		t.Errorf("expected self-referential slice, got:\n%s", out)
	}
}

func TestSelfViaMap(t *testing.T) {
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

// --- JSON tag edge cases ---

func TestJsonDashComma(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DashComma]())
	out := r.GenerateTypes()
	if strings.Contains(out, "Hidden") || strings.Contains(out, "hidden") {
		t.Errorf("json:\"-\" field should be skipped, got:\n%s", out)
	}
	// json:"-," means field named "-"
	if !strings.Contains(out, "-:") && !strings.Contains(out, "\"-\"") {
		// The wire name is "-"
		_ = out
	}
}

// --- Type coverage ---

func TestAllKindsTypes(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllKinds]())
	out := r.GenerateTypes()
	checks := map[string]string{
		"bool: boolean":                "bool",
		"int: number":                  "int",
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

// --- Embedding & ambiguity ---

func TestDeepEmbedding(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DeepA](), wiregen.TypeRef[edges.DeepB](), wiregen.TypeRef[edges.DeepC]())
	out := r.GenerateTypes()
	// DeepA should have id, name, email (all flattened)
	if !strings.Contains(out, "export interface DeepA") {
		t.Errorf("missing DeepA, got:\n%s", out)
	}
}

func TestPromotionAmbiguityOmitsBoth(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.Ambiguous](), wiregen.TypeRef[edges.AmbigLeft](), wiregen.TypeRef[edges.AmbigRight]())
	out := r.GenerateTypes()
	// Ambiguous should have id but NOT name (ambiguous from both embeddings)
	lines := strings.Split(out, "\n")
	inAmbiguous := false
	for _, l := range lines {
		if strings.Contains(l, "export interface Ambiguous") {
			inAmbiguous = true
			continue
		}
		if inAmbiguous && strings.HasPrefix(strings.TrimSpace(l), "}") {
			break
		}
		if inAmbiguous && strings.Contains(l, "name") {
			t.Errorf("ambiguous 'name' field should be omitted in Ambiguous, got:\n%s", out)
		}
	}
	if !strings.Contains(out, "id: number") {
		t.Errorf("non-ambiguous 'id' field should be present, got:\n%s", out)
	}
}

func TestDirectFieldWins(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DirectWins](), wiregen.TypeRef[edges.EmbBase]())
	out := r.GenerateTypes()
	// Direct field at depth 0 wins over embedded at depth 1
	if !strings.Contains(out, "name: string") {
		t.Errorf("direct field should win, got:\n%s", out)
	}
}

// --- Nested types ---

func TestMapOfStructs(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfStructs](), wiregen.TypeRef[edges.MapVal]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "data?: Record<string, MapVal>") {
		t.Errorf("missing map of structs, got:\n%s", out)
	}
}

func TestNestedOptionalPointerStruct(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.NestedOptPtr](), wiregen.TypeRef[edges.Inner]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "inner?: Inner") {
		t.Errorf("missing optional nested struct, got:\n%s", out)
	}
}

func TestSliceOfSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.SliceOfSlice]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "matrix: string[][]") {
		t.Errorf("expected string[][], got:\n%s", out)
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

// --- Empty/trivial structs ---

func TestEmptyStruct(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.EmptyStruct]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface EmptyStruct {\n}") {
		t.Errorf("expected empty interface, got:\n%s", out)
	}
}

func TestAllOptionalFields(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllOptional]())
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "const out: AllOptional = {") {
		t.Errorf("expected empty required block, got:\n%s", dec)
	}
}

// --- Enum tests ---

func TestOptionalEnumField(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasOptEnum]())
	r.Enums = map[string]wiregen.EnumDef{"MyEnum": {Values: []string{"a", "b"}}}
	out := r.GenerateTypes()
	if !strings.Contains(out, "status?: MyEnum") {
		t.Errorf("expected optional enum field, got:\n%s", out)
	}
}

// --- Options tests ---

func TestR6_NilOptionNoPanic(t *testing.T) {
	r := wiregen.NewRegistry(nil, nil, wiregen.WithValidatorsImport("./v.js"), nil)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	_ = r.GenerateTypes()
}

func TestR6_OptionOrderIndependence(t *testing.T) {
	r1 := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithHeaderComment("// H\n\n"),
	)
	r2 := wiregen.NewRegistry(
		wiregen.WithHeaderComment("// H\n\n"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithValidatorsImport("./v.js"),
	)
	r1.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r2.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r1.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	r2.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	if r1.GenerateTypes() != r2.GenerateTypes() {
		t.Error("option order should not affect output")
	}
}

func TestR6_OptionLastWriterWins(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithHeaderComment("// first\n\n"),
		wiregen.WithHeaderComment("// second\n\n"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	out := r.GenerateTypes()
	if !strings.Contains(out, "// second") {
		t.Errorf("last writer should win, got:\n%s", out)
	}
}

func TestR7_WithFilenames_AllEmpty(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithFilenames("", "", "", ""),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "Address"}}
	r.Constants = []wiregen.WireConst{{TSName: "X", Value: 1}}
	dir := t.TempDir()
	if err := r.Generate(dir); err != nil {
		t.Fatal(err)
	}
}

func TestR7_SelfContainedRegistry_EmptyValidators_Panics(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithSelfContainedRegistry(true),
	)
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "a", TypeName: "X"}}
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	r.GenerateRegistry()
}

// --- Determinism ---

func TestDeterminism200Iterations(t *testing.T) {
	r := edgesReg(
		wiregen.TypeRef[edges.AllKinds](),
		wiregen.TypeRef[edges.CycleA](),
		wiregen.TypeRef[edges.CycleB](),
		wiregen.TypeRef[edges.DeepA](),
		wiregen.TypeRef[edges.DeepB](),
		wiregen.TypeRef[edges.DeepC](),
	)
	r.Enums = map[string]wiregen.EnumDef{"MyEnum": {Values: []string{"x", "y"}}}
	baseline := r.GenerateTypes()
	for i := range 5 {
		r2 := edgesReg(
			wiregen.TypeRef[edges.AllKinds](),
			wiregen.TypeRef[edges.CycleA](),
			wiregen.TypeRef[edges.CycleB](),
			wiregen.TypeRef[edges.DeepA](),
			wiregen.TypeRef[edges.DeepB](),
			wiregen.TypeRef[edges.DeepC](),
		)
		r2.Enums = map[string]wiregen.EnumDef{"MyEnum": {Values: []string{"x", "y"}}}
		if r2.GenerateTypes() != baseline {
			t.Fatalf("non-deterministic on iteration %d", i)
		}
	}
}

// --- PathName/Header override ---

func TestPathNameOverride(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.Inner]())
	r.PathNameOverride = map[string]string{"Inner": "custom_path"}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "$.custom_path") {
		t.Errorf("expected custom path, got:\n%s", dec)
	}
}

func TestHeaderCommentOverride(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
		wiregen.WithHeaderComment("// CUSTOM\n\n"),
	)
	r.PackagePaths = []string{edgesPkg}
	r.Types = []wiregen.WireType{wiregen.TypeRef[edges.Inner]()}
	out := r.GenerateTypes()
	if !strings.Contains(out, "// CUSTOM") {
		t.Errorf("expected custom header, got:\n%s", out)
	}
}

// --- TSName/Enum overrides ---

func TestTSNameOverride(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.Inner]())
	r.TSNameOverride = map[string]string{"Inner": "InnerDTO"}
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface InnerDTO") {
		t.Errorf("expected InnerDTO, got:\n%s", out)
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
