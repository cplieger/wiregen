package wiregen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/basic"
	"github.com/cplieger/wiregen/testdata/edges"
	"github.com/cplieger/wiregen/testdata/unions"
)

// ============================================================
// RT2: ADVERSARIAL RED-TEAM TESTS — Phase 2
// ============================================================

// --- (1) go/packages: deeper pointer/slice/map combos ---

func TestRT2_TriplePointer(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TriplePtr]())
	out := r.GenerateTypes()
	// ***string should unwrap to optional string
	if !strings.Contains(out, "val?: string") {
		t.Errorf("***string should be optional string, got:\n%s", out)
	}
}

func TestRT2_PtrToSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.PtrToSlice]())
	out := r.GenerateTypes()
	// *[]string should be optional string[]
	if !strings.Contains(out, "items?: string[]") {
		t.Errorf("*[]string should be optional string[], got:\n%s", out)
	}
}

func TestRT2_PtrToMap(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.PtrToMap]())
	out := r.GenerateTypes()
	// *map[string]int should be optional Record<string, number>
	if !strings.Contains(out, "data?: Record<string, number>") {
		t.Errorf("*map[string]int should be optional Record<string, number>, got:\n%s", out)
	}
}

func TestRT2_SliceOfPtrs(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.SliceOfPtrs]())
	out := r.GenerateTypes()
	// []*string should be string[] (pointers in slices should just unwrap)
	if !strings.Contains(out, "names: string[]") {
		t.Errorf("[]*string should produce string[], got:\n%s", out)
	}
}

func TestRT2_MapOfPtrs(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfPtrs]())
	out := r.GenerateTypes()
	// map[string]*int should be Record<string, number>
	if !strings.Contains(out, "scores?: Record<string, number>") {
		t.Errorf("map[string]*int should produce Record<string, number>, got:\n%s", out)
	}
}

func TestRT2_TimeSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TimeSlice]())
	out := r.GenerateTypes()
	// []time.Time should produce string[]
	if !strings.Contains(out, "dates: string[]") {
		t.Errorf("[]time.Time should produce string[], got:\n%s", out)
	}
}

func TestRT2_OptionalByteSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.OptionalByteSlice]())
	out := r.GenerateTypes()
	// *[]byte → optional string (pointer + []byte = optional + base64)
	if !strings.Contains(out, "data?: string") {
		t.Errorf("*[]byte should produce optional string, got:\n%s", out)
	}
}

func TestRT2_MapOfBytes(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfBytes]())
	out := r.GenerateTypes()
	// map[string][]byte → Record<string, string> ([]byte = string)
	if !strings.Contains(out, "blobs?: Record<string, string>") {
		t.Errorf("map[string][]byte should produce Record<string, string>, got:\n%s", out)
	}
}

func TestRT2_DeeplyNestedMap(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DeeplyNestedMap]())
	out := r.GenerateTypes()
	// map[string]map[string]map[string]string → Record<string, Record<string, Record<string, string>>>
	if !strings.Contains(out, "Record<string, Record<string, Record<string, string>>>") {
		t.Errorf("deeply nested map should produce nested Records, got:\n%s", out)
	}
}

func TestRT2_SliceOfSliceOfSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.SliceOfSliceOfSlice]())
	out := r.GenerateTypes()
	// [][][]int → number[][][]
	if !strings.Contains(out, "cube: number[][][]") {
		t.Errorf("[][][]int should produce number[][][], got:\n%s", out)
	}
}

func TestRT2_RawSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.RawSlice]())
	out := r.GenerateTypes()
	// []json.RawMessage → unknown[]
	if !strings.Contains(out, "items: unknown[]") {
		t.Errorf("[]json.RawMessage should produce unknown[], got:\n%s", out)
	}
}

func TestRT2_InterfaceSlice(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.InterfaceSlice]())
	out := r.GenerateTypes()
	// []interface{} → unknown[]
	if !strings.Contains(out, "items: unknown[]") {
		t.Errorf("[]interface{} should produce unknown[], got:\n%s", out)
	}
}

// --- (1) Embedding with overrides ---

func TestRT2_EmbedOverride(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.EmbedOverride](), wiregen.TypeRef[edges.EmbedBase2]())
	out := r.GenerateTypes()
	// Direct field x: int should win over embedded x: string
	// But at the TS level, "x" should appear once in EmbedOverride with the direct type
	lines := strings.Split(out, "\n")
	inEmbedOverride := false
	xCount := 0
	for _, l := range lines {
		if strings.Contains(l, "export interface EmbedOverride") {
			inEmbedOverride = true
			continue
		}
		if inEmbedOverride && strings.HasPrefix(strings.TrimSpace(l), "}") {
			break
		}
		if inEmbedOverride && strings.Contains(l, "x:") {
			xCount++
		}
	}
	if xCount != 1 {
		t.Errorf("EmbedOverride should have exactly 1 'x' field, got %d in:\n%s", xCount, out)
	}
	// Direct x should be number (int)
	if !strings.Contains(out, "x: number;") {
		t.Errorf("direct 'x' in EmbedOverride should be number, got:\n%s", out)
	}
}

func TestRT2_TwoLevelEmbed(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.Level1](), wiregen.TypeRef[edges.Level2](), wiregen.TypeRef[edges.Level3]())
	out := r.GenerateTypes()
	// Level1 should have name (from Level2 at depth 1), email, age (from Level3 depth 2)
	lines := strings.Split(out, "\n")
	inLevel1 := false
	hasName := false
	hasEmail := false
	for _, l := range lines {
		if strings.Contains(l, "export interface Level1") {
			inLevel1 = true
			continue
		}
		if inLevel1 && strings.HasPrefix(strings.TrimSpace(l), "}") {
			break
		}
		if inLevel1 && strings.Contains(l, "name:") {
			hasName = true
		}
		if inLevel1 && strings.Contains(l, "email:") {
			hasEmail = true
		}
	}
	if !hasName {
		t.Errorf("Level1 should have 'name' from Level2, got:\n%s", out)
	}
	if !hasEmail {
		t.Errorf("Level1 should have 'email', got:\n%s", out)
	}
}

// --- (1) JSON tag edge cases ---

func TestRT2_TagEmptyWireName(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TagEmpty]())
	out := r.GenerateTypes()
	// json:",omitempty" → wire name = "Value" (Go field name), optional
	if !strings.Contains(out, "Value?: string") {
		t.Errorf("empty tag name should use Go field name 'Value', got:\n%s", out)
	}
}

func TestRT2_TagOnlyOptionsStringEncoding(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TagOnlyOptions]())
	out := r.GenerateTypes()
	// json:",string" → wire name = "Count", type = string
	if !strings.Contains(out, "Count: string") {
		t.Errorf("json:\",string\" should produce string type with Go name, got:\n%s", out)
	}
}

func TestRT2_AllPointerFieldsOptional(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllPointerFields]())
	out := r.GenerateTypes()
	// All pointer fields should be optional
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

func TestRT2_StructWithRawAndTime(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.StructWithRawAndTime]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "payload: unknown;") {
		t.Errorf("json.RawMessage should be unknown, got:\n%s", out)
	}
	if !strings.Contains(out, "when: string;") {
		t.Errorf("time.Time should be string, got:\n%s", out)
	}
	if !strings.Contains(out, "label: string;") {
		t.Errorf("string should be string, got:\n%s", out)
	}
}

func TestRT2_PrivateEmbedSkipped(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasPrivateEmbed]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "name: string;") {
		t.Errorf("exported field should be present, got:\n%s", out)
	}
	if strings.Contains(out, "secret") {
		t.Errorf("unexported embedded struct's fields should NOT appear, got:\n%s", out)
	}
}

// --- (2) DETERMINISM: byte-identity with shuffled enums + types ---

func TestRT2_DeterminismFullGenerate_10x(t *testing.T) {
	makeReg := func() *wiregen.Registry {
		r := wiregen.NewRegistry(
			wiregen.WithValidatorsImport("./v.js"),
			wiregen.WithBusImport("./b.js"),
		)
		r.PackagePaths = []string{
			"github.com/cplieger/wiregen/testdata/basic",
			"github.com/cplieger/wiregen/testdata/edges",
			"github.com/cplieger/wiregen/testdata/unions",
		}
		r.Types = []wiregen.WireType{
			wiregen.TypeRef[basic.User](),
			wiregen.TypeRef[basic.Address](),
			wiregen.TypeRef[basic.Notification](),
			wiregen.TypeRef[basic.HasBytes](),
			wiregen.TypeRef[basic.HasTime](),
			wiregen.TypeRef[basic.HasMap](),
			wiregen.TypeRef[basic.WithEmbedding](),
			wiregen.TypeRef[edges.AllKinds](),
			wiregen.TypeRef[edges.SelfSlice](),
			wiregen.TypeRef[edges.CycleA](),
			wiregen.TypeRef[edges.CycleB](),
			wiregen.TypeRef[edges.MapOfStructs](),
			wiregen.TypeRef[edges.MapVal](),
			wiregen.TypeRef[edges.DeepA](),
			wiregen.TypeRef[edges.DeepB](),
			wiregen.TypeRef[edges.DeepC](),
			wiregen.TypeRef[unions.CoverageEvent](),
			wiregen.TypeRef[unions.NotifyEvent](),
			wiregen.TypeRef[unions.ScanEvent](),
			{PkgPath: "github.com/cplieger/wiregen/testdata/unions", Name: "EventData"},
		}
		r.Enums = map[string]wiregen.EnumDef{
			"Status":   {Values: []string{"active", "inactive", "banned"}},
			"Priority": {Values: []string{"low", "medium", "high", "critical"}},
			"MyEnum":   {Values: []string{"a", "b", "c"}},
		}
		r.DiscriminatorMap = map[string]map[string]string{
			"EventData": {
				"coverage":   "CoverageEvent",
				"notify":     "NotifyEvent",
				"scan:start": "ScanEvent",
				"scan:done":  "ScanEvent",
			},
		}
		r.SSEEvents = []wiregen.SSERegEntry{
			{EventType: "notification", TypeName: "Notification"},
			{EventType: "user:updated", TypeName: "User"},
			{EventType: "coverage", TypeName: "CoverageEvent"},
		}
		r.Constants = []wiregen.WireConst{
			{TSName: "MSG_A", Value: 1},
			{TSName: "MSG_B", Value: 2},
			{TSName: "MSG_C", Value: 3},
		}
		return r
	}

	dir1 := t.TempDir()
	if err := makeReg().Generate(dir1); err != nil {
		t.Fatal(err)
	}

	for i := range 10 {
		dir2 := t.TempDir()
		if err := makeReg().Generate(dir2); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"types.gen.ts", "decoders.gen.ts", "registry.gen.ts", "constants.gen.ts"} {
			b1, _ := os.ReadFile(filepath.Join(dir1, f))
			b2, _ := os.ReadFile(filepath.Join(dir2, f))
			if string(b1) != string(b2) {
				t.Fatalf("DETERMINISM FAILURE on iteration %d, file %s", i, f)
			}
		}
	}
}

// --- (3) Union decoder: missing/empty DiscriminatorMap behavior ---

func TestRT2_UnionEmptyDiscriminatorMap(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/unions"}
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[unions.CoverageEvent](),
		wiregen.TypeRef[unions.NotifyEvent](),
		wiregen.TypeRef[unions.ScanEvent](),
		{PkgPath: "github.com/cplieger/wiregen/testdata/unions", Name: "EventData"},
	}
	// Empty DiscriminatorMap (map exists but has empty value for EventData)
	r.DiscriminatorMap = map[string]map[string]string{
		"EventData": {},
	}
	dec := r.GenerateDecoders()
	// Should still emit the union decoder but with no cases (only default)
	// OR should not emit at all (depends on implementation)
	// Verify it doesn't panic
	_ = dec
}

func TestRT2_UnionWithPartialDiscriminatorMap(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/unions"}
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[unions.CoverageEvent](),
		wiregen.TypeRef[unions.NotifyEvent](),
		wiregen.TypeRef[unions.ScanEvent](),
		{PkgPath: "github.com/cplieger/wiregen/testdata/unions", Name: "EventData"},
	}
	// Only partial discriminator map
	r.DiscriminatorMap = map[string]map[string]string{
		"EventData": {
			"coverage": "CoverageEvent",
		},
	}
	dec := r.GenerateDecoders()
	// Should emit only the mapped variant + unknown default
	if !strings.Contains(dec, `case "coverage"`) {
		t.Errorf("partial discriminator should have coverage case, got:\n%s", dec)
	}
	if strings.Contains(dec, `case "notify"`) {
		t.Errorf("unmapped variant should NOT appear, got:\n%s", dec)
	}
	if !strings.Contains(dec, "default: throw") {
		t.Errorf("should have unknown variant default, got:\n%s", dec)
	}
}

func TestRT2_UnionDecoderSignature(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/unions"}
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[unions.CoverageEvent](),
		wiregen.TypeRef[unions.NotifyEvent](),
		wiregen.TypeRef[unions.ScanEvent](),
		{PkgPath: "github.com/cplieger/wiregen/testdata/unions", Name: "EventData"},
	}
	r.DiscriminatorMap = map[string]map[string]string{
		"EventData": {
			"coverage": "CoverageEvent",
			"notify":   "NotifyEvent",
		},
	}
	dec := r.GenerateDecoders()
	// Verify the decoder uses the discriminator field name from the directive
	if !strings.Contains(dec, "(type: string, v: unknown) => EventData") {
		t.Errorf("union decoder should use discriminator 'type' from directive, got:\n%s", dec)
	}
}

// --- (4) Type-safe registration: error on removed/wrong type ---

func TestRT2_TypeRefGenericConstraint(t *testing.T) {
	// TypeRef[T] with a primitive should work
	wt := wiregen.TypeRef[basic.Address]()
	if wt.PkgPath == "" || wt.Name == "" {
		t.Error("TypeRef should capture pkg path and name")
	}
}

func TestRT2_NonStructTypeErrors(t *testing.T) {
	// Registering a non-struct type (interface) by WireType literal
	// should be handled gracefully
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/unions"}
	r.Types = []wiregen.WireType{
		{PkgPath: "github.com/cplieger/wiregen/testdata/unions", Name: "EventData"},
	}
	// EventData is an interface — should not panic, just produce union type if directive exists
	out := r.GenerateTypes()
	if !strings.Contains(out, "export type EventData") {
		t.Errorf("interface with wiregen:union should produce type alias, got:\n%s", out)
	}
}

func TestRT2_GenerateErrors_PackageLoadFailure(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/totally/nonexistent/package/path"}
	r.Types = []wiregen.WireType{
		{PkgPath: "github.com/totally/nonexistent/package/path", Name: "Foo"},
	}
	dir := t.TempDir()
	err := r.Generate(dir)
	if err == nil {
		t.Fatal("expected error for non-existent package")
	}
}

// --- (5) Fuzz-like: exercise corner cases programmatically ---

func TestRT2_EmptyFieldName(t *testing.T) {
	// Edge case: what if a registered type has no fields at all?
	r := edgesReg(wiregen.TypeRef[edges.EmptyStruct]())
	dec := r.GenerateDecoders()
	// Should not panic, should produce a valid decoder
	if !strings.Contains(dec, "decodeEmptyStruct") {
		t.Errorf("empty struct should still get a decoder, got:\n%s", dec)
	}
	if !strings.Contains(dec, "return out;") {
		t.Errorf("decoder should return out, got:\n%s", dec)
	}
}

func TestRT2_ManyEnums_Deterministic(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	// Create many enums to stress map ordering
	r.Enums = map[string]wiregen.EnumDef{}
	for _, name := range []string{"Zebra", "Alpha", "Mango", "Beta", "Delta", "Charlie", "Echo", "Foxtrot"} {
		r.Enums[name] = wiregen.EnumDef{Values: []string{"v1", "v2"}}
	}
	baseline := r.GenerateTypes()
	for range 20 {
		r2 := wiregen.NewRegistry(
			wiregen.WithValidatorsImport("./v.js"),
			wiregen.WithBusImport("./b.js"),
		)
		r2.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
		r2.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
		r2.Enums = map[string]wiregen.EnumDef{}
		for _, name := range []string{"Foxtrot", "Echo", "Charlie", "Delta", "Beta", "Mango", "Alpha", "Zebra"} {
			r2.Enums[name] = wiregen.EnumDef{Values: []string{"v1", "v2"}}
		}
		if r2.GenerateTypes() != baseline {
			t.Fatal("enum ordering is non-deterministic")
		}
	}
}

func TestRT2_LargeConstantsArray(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	var consts []wiregen.WireConst
	for i := range 100 {
		consts = append(consts, wiregen.WireConst{TSName: "CONST_" + strings.Repeat("A", 3) + string(rune('A'+i%26)), Value: i})
	}
	r.Constants = consts
	out := r.GenerateConstants()
	if strings.Count(out, "export const") != 100 {
		t.Errorf("expected 100 constants, got %d", strings.Count(out, "export const"))
	}
}

// --- (6) Golden file meaningfulness: verify structural properties ---

func TestRT2_GoldenDecoderImportsAreSorted(t *testing.T) {
	b, err := os.ReadFile("testdata/golden/decoders.gen.ts")
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	// Find the "import type { ... }" line
	for line := range strings.SplitSeq(content, "\n") {
		if strings.Contains(line, "import type {") {
			// Extract names between { and }
			start := strings.Index(line, "{")
			end := strings.Index(line, "}")
			if start < 0 || end < 0 {
				continue
			}
			names := strings.Split(strings.TrimSpace(line[start+1:end]), ", ")
			for i := 1; i < len(names); i++ {
				if strings.TrimSpace(names[i]) < strings.TrimSpace(names[i-1]) {
					t.Errorf("type imports not sorted: %q before %q", names[i-1], names[i])
				}
			}
		}
	}
}

func TestRT2_GoldenTypesHaveHeader(t *testing.T) {
	b, err := os.ReadFile("testdata/golden/types.gen.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "// CODE-GENERATED") {
		t.Error("golden types file should start with header comment")
	}
}

func TestRT2_GoldenDecodersHaveHeader(t *testing.T) {
	b, err := os.ReadFile("testdata/golden/decoders.gen.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "// CODE-GENERATED") {
		t.Error("golden decoders file should start with header comment")
	}
}

func TestRT2_DecodersHaveNoEmptyImportType(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.HasBytes]()}
	dec := r.GenerateDecoders()
	// HasBytes doesn't reference any other registered type, so type imports may be empty
	// Verify no "import type { } from" line (empty imports)
	if strings.Contains(dec, "import type {  }") || strings.Contains(dec, "import type {}") {
		t.Errorf("should not emit empty type imports, got:\n%s", dec)
	}
}
