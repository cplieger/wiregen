package wiregen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/basic"
	"github.com/cplieger/wiregen/testdata/edges"
)

// ============================================================
// ADVERSARIAL CLOSEOUT — probes beyond prior red-team coverage
// ============================================================

// --- (1) Alias chains: Go 1.22+ types.Alias regression ---

func TestCloseout_AliasOfAlias(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasAliasOfAlias]())
	out := r.GenerateTypes()
	// AliasOfAlias -> MyString -> string
	if !strings.Contains(out, "val: string;") {
		t.Errorf("alias-of-alias should resolve to string, got:\n%s", out)
	}
}

func TestCloseout_PtrToAlias(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasPtrAlias]())
	out := r.GenerateTypes()
	// *MyString -> optional string
	if !strings.Contains(out, "name?: string;") {
		t.Errorf("*alias should be optional string, got:\n%s", out)
	}
}

func TestCloseout_DeepTimeAlias(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasDeepTimeAlias]())
	out := r.GenerateTypes()
	// MyTimeAlias -> MyTime -> time.Time -> string
	if !strings.Contains(out, "at: string;") {
		t.Errorf("deep time alias should resolve to string, got:\n%s", out)
	}
}

// --- (1) Embedded pointer struct with field override ---

func TestCloseout_EmbedPtrOverride(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasEmbedPtr]())
	out := r.GenerateTypes()
	// Direct Name field should win over embedded *EmbedPtrBase.Name
	if !strings.Contains(out, "name: string;") {
		t.Errorf("direct field should be present, got:\n%s", out)
	}
	// Should also have id from embedded struct
	if !strings.Contains(out, "id: number;") {
		t.Errorf("embedded ptr field 'id' should be present, got:\n%s", out)
	}
	// Only one 'name' line
	lines := strings.Split(out, "\n")
	nameCount := 0
	inType := false
	for _, l := range lines {
		if strings.Contains(l, "export interface HasEmbedPtr") {
			inType = true
			continue
		}
		if inType && strings.HasPrefix(strings.TrimSpace(l), "}") {
			break
		}
		if inType && strings.Contains(l, "name") {
			nameCount++
		}
	}
	if nameCount != 1 {
		t.Errorf("expected exactly 1 'name' field, got %d:\n%s", nameCount, out)
	}
}

// --- (1) Map of unregistered struct value (should be unknown/Record) ---

func TestCloseout_MapOfUnregisteredStruct(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfUnregistered]())
	out := r.GenerateTypes()
	// map[string]EmbedPtrBase where EmbedPtrBase is NOT registered
	// should produce Record<string, unknown> since it's an anonymous/unregistered struct
	if !strings.Contains(out, "data?:") {
		t.Errorf("map field should be present and optional, got:\n%s", out)
	}
}

// --- (1) Struct with only unexported fields ---

func TestCloseout_OnlyUnexported(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.OnlyUnexported]())
	out := r.GenerateTypes()
	// Should produce empty interface
	if !strings.Contains(out, "export interface OnlyUnexported") {
		t.Errorf("should still produce interface, got:\n%s", out)
	}
}

// --- (1) JSON tag with both omitempty and string options ---

func TestCloseout_ManyTagOptions(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.ManyOptions]())
	out := r.GenerateTypes()
	// json:"a,omitempty,string" → optional + string type
	if !strings.Contains(out, "a?: string;") {
		t.Errorf("multiple tag options should produce optional string, got:\n%s", out)
	}
}

// --- (1) Embedded struct with json:"-" field ---

func TestCloseout_EmbedWithDashField(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasEmbedWithDash]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "visible: string;") {
		t.Errorf("visible field from embed should be present, got:\n%s", out)
	}
	if strings.Contains(out, "Hidden") || strings.Contains(out, "hidden") {
		t.Errorf("json:\"-\" field from embed should be excluded, got:\n%s", out)
	}
	if !strings.Contains(out, "extra: string;") {
		t.Errorf("direct extra field should be present, got:\n%s", out)
	}
}

// --- (2) DETERMINISM: decoder body is byte-identical across 3 runs with maps ---

func TestCloseout_DeterminismDecoder_MapFields(t *testing.T) {
	makeReg := func() *wiregen.Registry {
		r := wiregen.NewRegistry(
			wiregen.WithValidatorsImport("./v.js"),
			wiregen.WithBusImport("./b.js"),
		)
		r.PackagePaths = []string{edgesPkg}
		r.Types = []wiregen.WireType{
			wiregen.TypeRef[edges.MapOfStructs](),
			wiregen.TypeRef[edges.MapVal](),
			wiregen.TypeRef[edges.AllKinds](),
			wiregen.TypeRef[edges.SelfMap](),
		}
		r.Enums = map[string]wiregen.EnumDef{
			"MyEnum": {Values: []string{"a", "b", "c"}},
			"Status": {Values: []string{"x", "y"}},
		}
		r.DiscriminatorMap = map[string]map[string]string{}
		return r
	}

	baseline := makeReg().GenerateDecoders()
	for i := range 3 {
		got := makeReg().GenerateDecoders()
		if got != baseline {
			t.Fatalf("decoder non-deterministic on iteration %d", i)
		}
	}
}

// --- (2) DETERMINISM: Full Generate to disk, compare files byte-by-byte ---

func TestCloseout_DeterminismFullDisk(t *testing.T) {
	makeReg := func() *wiregen.Registry {
		r := wiregen.NewRegistry(
			wiregen.WithValidatorsImport("./v.js"),
			wiregen.WithBusImport("./b.js"),
		)
		r.PackagePaths = []string{edgesPkg}
		r.Types = []wiregen.WireType{
			wiregen.TypeRef[edges.AllKinds](),
			wiregen.TypeRef[edges.MapOfStructs](),
			wiregen.TypeRef[edges.MapVal](),
			wiregen.TypeRef[edges.CycleA](),
			wiregen.TypeRef[edges.CycleB](),
		}
		r.Enums = map[string]wiregen.EnumDef{
			"Alpha": {Values: []string{"a1", "a2"}},
			"Beta":  {Values: []string{"b1", "b2"}},
		}
		r.SSEEvents = []wiregen.SSERegEntry{
			{EventType: "event_a", TypeName: "CycleA"},
		}
		r.Constants = []wiregen.WireConst{
			{TSName: "C1", Value: 10},
			{TSName: "C2", Value: 20},
		}
		return r
	}

	dir1 := t.TempDir()
	if err := makeReg().Generate(dir1); err != nil {
		t.Fatal(err)
	}

	dir2 := t.TempDir()
	if err := makeReg().Generate(dir2); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"types.gen.ts", "decoders.gen.ts", "registry.gen.ts", "constants.gen.ts"} {
		b1, _ := os.ReadFile(filepath.Join(dir1, f))
		b2, _ := os.ReadFile(filepath.Join(dir2, f))
		if string(b1) != string(b2) {
			t.Fatalf("DETERMINISM FAILURE file %s\n--- expected ---\n%s\n--- got ---\n%s", f, b1, b2)
		}
	}
}

// --- (3) Union: verify decoder is NOT emitted when DiscriminatorMap is nil for that type ---

func TestCloseout_UnionNilVsEmptyDiscriminatorMap(t *testing.T) {
	// nil DiscriminatorMap for union → no decoder
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/unions"}
	r.Types = []wiregen.WireType{
		{PkgPath: "github.com/cplieger/wiregen/testdata/unions", Name: "EventData"},
	}
	// DiscriminatorMap left nil
	dec := r.GenerateDecoders()
	if strings.Contains(dec, "decodeEventData") {
		t.Errorf("nil DiscriminatorMap should NOT produce decoder, got:\n%s", dec)
	}
}

// --- (4) TypeRef captures correct info for interface types ---

func TestCloseout_TypeRefInterface(t *testing.T) {
	// WireType literal for interface must work without panic
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/unions"}
	r.Types = []wiregen.WireType{
		{PkgPath: "github.com/cplieger/wiregen/testdata/unions", Name: "EventData"},
	}
	out := r.GenerateTypes()
	// Should emit union type alias (interface with wiregen:union directive)
	if !strings.Contains(out, "export type EventData") {
		t.Errorf("interface with union directive should produce type, got:\n%s", out)
	}
}

// --- (5) Fuzz-adjacent: very long field names, special characters in enum values ---

func TestCloseout_LongEnumValues(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{edgesPkg}
	r.Types = []wiregen.WireType{wiregen.TypeRef[edges.Inner]()}
	r.Enums = map[string]wiregen.EnumDef{
		"LongEnum": {Values: []string{
			strings.Repeat("a", 500),
			"normal",
			"with spaces",
			"with\"quotes",
			"with\nnewline",
		}},
	}
	// Must not panic
	out := r.GenerateTypes()
	if !strings.Contains(out, "export type LongEnum") {
		t.Errorf("long enum should be emitted, got:\n%s", out)
	}
}

func TestCloseout_EmptyEnumValues(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{edgesPkg}
	r.Types = []wiregen.WireType{wiregen.TypeRef[edges.Inner]()}
	r.Enums = map[string]wiregen.EnumDef{
		"EmptyEnum": {Values: []string{}},
	}
	// Must not panic
	out := r.GenerateTypes()
	if !strings.Contains(out, "export type EmptyEnum") {
		t.Errorf("empty enum should still be emitted, got:\n%s", out)
	}
}

// --- (6) Golden files re-generation: verify current code produces exact golden match ---

func TestCloseout_GoldenRegeneration(t *testing.T) {
	r := newRegistry()
	r.Types = append(r.Types,
		wiregen.TypeRef[basic.HasTime](),
		wiregen.TypeRef[basic.HasBytes](),
		wiregen.TypeRef[basic.HasMap](),
		wiregen.TypeRef[basic.WithEmbedding](),
	)

	gotTypes := r.GenerateTypes()
	wantTypes, err := os.ReadFile("testdata/golden/types.gen.ts")
	if err != nil {
		t.Fatal(err)
	}
	if gotTypes != string(wantTypes) {
		t.Errorf("generated types.gen.ts doesn't match golden file")
	}

	gotDec := r.GenerateDecoders()
	wantDec, err := os.ReadFile("testdata/golden/decoders.gen.ts")
	if err != nil {
		t.Fatal(err)
	}
	if gotDec != string(wantDec) {
		t.Errorf("generated decoders.gen.ts doesn't match golden file")
	}
}
