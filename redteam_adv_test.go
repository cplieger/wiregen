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
// (1) go/packages loading: build tags, type aliases, embedded structs,
//     json tags, pointers, []byte, time.Time, maps, nested/recursive
// ============================================================

func TestRT_TypeAlias(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasAliases]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "label: string;") {
		t.Errorf("type alias MyString should resolve to string, got:\n%s", out)
	}
	if !strings.Contains(out, "count: number;") {
		t.Errorf("type alias MyInt should resolve to number, got:\n%s", out)
	}
}

func TestRT_TimeAlias(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasTimeAlias]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "at: string;") {
		t.Errorf("time.Time alias should still resolve to string, got:\n%s", out)
	}
}

func TestRT_DoublePointer(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DoublePtr]())
	out := r.GenerateTypes()
	// Double pointer should be optional string (unwrapped)
	if !strings.Contains(out, "val?: string") {
		t.Errorf("double pointer should be optional string, got:\n%s", out)
	}
}

func TestRT_MapOfMap(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.MapOfMap]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "Record<string, Record<string, number>>") {
		t.Errorf("map of map should produce nested Record, got:\n%s", out)
	}
}

func TestRT_TagVariants(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TagVariants]())
	out := r.GenerateTypes()
	// "required" field is not omitempty => required
	if !strings.Contains(out, "required: string;") {
		t.Errorf("required field missing, got:\n%s", out)
	}
	// omitempty field is optional
	if !strings.Contains(out, "omitempty_field?: string;") {
		t.Errorf("omitempty field should be optional, got:\n%s", out)
	}
	// renamed field uses wire_name
	if !strings.Contains(out, "wire_name: string;") {
		t.Errorf("renamed field should use wire_name, got:\n%s", out)
	}
	// NoTag field uses Go field name
	if !strings.Contains(out, "NoTag: string;") {
		t.Errorf("untagged field should use Go name, got:\n%s", out)
	}
}

func TestRT_PointerMakesOptional(t *testing.T) {
	// User.Age is *int → optional number
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.User]()}
	r.Enums = map[string]wiregen.EnumDef{"Status": {Values: []string{"active"}}}
	out := r.GenerateTypes()
	if !strings.Contains(out, "age?: number;") {
		t.Errorf("*int should be optional number, got:\n%s", out)
	}
}

func TestRT_TimeToString(t *testing.T) {
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

func TestRT_ByteSliceToBase64String(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.HasBytes]()}
	types := r.GenerateTypes()
	if !strings.Contains(types, "data: string;") {
		t.Errorf("[]byte should map to string, got:\n%s", types)
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "reqStr(o, \"data\"") {
		t.Errorf("[]byte decoder should use reqStr, got:\n%s", dec)
	}
}

func TestRT_MapMakesOptional(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.HasMap]()}
	out := r.GenerateTypes()
	if !strings.Contains(out, "meta?: Record<string, string>;") {
		t.Errorf("map field should be optional, got:\n%s", out)
	}
}

func TestRT_EmbeddedStructFlatten(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.WithEmbedding]()}
	out := r.GenerateTypes()
	// Should have fields from embedded Base
	if !strings.Contains(out, "id: number;") {
		t.Errorf("embedded Base.ID should be flattened, got:\n%s", out)
	}
	if !strings.Contains(out, "created_at: string;") {
		t.Errorf("embedded Base.CreatedAt should be flattened as string, got:\n%s", out)
	}
	if !strings.Contains(out, "name: string;") {
		t.Errorf("own field Name should be present, got:\n%s", out)
	}
}

func TestRT_NestedRecursiveTypes(t *testing.T) {
	r := edgesReg(
		wiregen.TypeRef[edges.SelfSlice](),
		wiregen.TypeRef[edges.SelfMap](),
	)
	out := r.GenerateTypes()
	if !strings.Contains(out, "children?: SelfSlice[];") {
		t.Errorf("self-referential slice missing, got:\n%s", out)
	}
	if !strings.Contains(out, "children?: Record<string, SelfMap>;") {
		t.Errorf("self-referential map missing, got:\n%s", out)
	}
}

// ============================================================
// (2) DETERMINISM — generate twice, byte-identical; stable ordering
// ============================================================

func TestRT_Determinism_ByteIdentical(t *testing.T) {
	makeReg := func() *wiregen.Registry {
		r := wiregen.NewRegistry(
			wiregen.WithValidatorsImport("./v.js"),
			wiregen.WithBusImport("./b.js"),
		)
		r.PackagePaths = []string{
			"github.com/cplieger/wiregen/testdata/basic",
			"github.com/cplieger/wiregen/testdata/edges",
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
		}
		r.Enums = map[string]wiregen.EnumDef{
			"Status":   {Values: []string{"active", "inactive", "banned"}},
			"Priority": {Values: []string{"low", "med", "high"}},
		}
		r.SSEEvents = []wiregen.SSERegEntry{
			{EventType: "notification", TypeName: "Notification"},
			{EventType: "user:updated", TypeName: "User"},
		}
		r.Constants = []wiregen.WireConst{
			{TSName: "MSG_A", Value: 1},
			{TSName: "MSG_B", Value: 2},
		}
		return r
	}

	dir1 := t.TempDir()
	dir2 := t.TempDir()
	r1 := makeReg()
	if err := r1.Generate(dir1); err != nil {
		t.Fatal(err)
	}
	r2 := makeReg()
	if err := r2.Generate(dir2); err != nil {
		t.Fatal(err)
	}

	files := []string{"types.gen.ts", "decoders.gen.ts", "registry.gen.ts", "constants.gen.ts"}
	for _, f := range files {
		b1, err := os.ReadFile(filepath.Join(dir1, f))
		if err != nil {
			t.Fatal(err)
		}
		b2, err := os.ReadFile(filepath.Join(dir2, f))
		if err != nil {
			t.Fatal(err)
		}
		if string(b1) != string(b2) {
			t.Errorf("DETERMINISM FAILURE: %s differs between two runs", f)
		}
	}
}

func TestRT_Determinism_MapOrdering(t *testing.T) {
	// Maps in Go iterate nondeterministically — verify enum ordering is stable
	for range 10 {
		r := wiregen.NewRegistry(
			wiregen.WithValidatorsImport("./v.js"),
			wiregen.WithBusImport("./b.js"),
		)
		r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
		r.Types = []wiregen.WireType{wiregen.TypeRef[basic.User]()}
		r.Enums = map[string]wiregen.EnumDef{
			"A": {Values: []string{"x"}},
			"B": {Values: []string{"y"}},
			"C": {Values: []string{"z"}},
			"D": {Values: []string{"w"}},
		}
		out1 := r.GenerateTypes()

		r2 := wiregen.NewRegistry(
			wiregen.WithValidatorsImport("./v.js"),
			wiregen.WithBusImport("./b.js"),
		)
		r2.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
		r2.Types = []wiregen.WireType{wiregen.TypeRef[basic.User]()}
		r2.Enums = map[string]wiregen.EnumDef{
			"D": {Values: []string{"w"}},
			"C": {Values: []string{"z"}},
			"B": {Values: []string{"y"}},
			"A": {Values: []string{"x"}},
		}
		out2 := r2.GenerateTypes()

		if out1 != out2 {
			t.Fatalf("enum map ordering not deterministic:\n%s\nvs\n%s", out1, out2)
		}
	}
}

func TestRT_Determinism_ImportOrdering(t *testing.T) {
	// Verify import statements in decoders are deterministic
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[basic.User](),
		wiregen.TypeRef[basic.Address](),
		wiregen.TypeRef[basic.HasBytes](),
		wiregen.TypeRef[basic.HasMap](),
	}
	r.Enums = map[string]wiregen.EnumDef{"Status": {Values: []string{"active"}}}
	baseline := r.GenerateDecoders()

	for range 20 {
		r2 := wiregen.NewRegistry(
			wiregen.WithValidatorsImport("./v.js"),
			wiregen.WithBusImport("./b.js"),
		)
		r2.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
		r2.Types = []wiregen.WireType{
			wiregen.TypeRef[basic.HasMap](),
			wiregen.TypeRef[basic.HasBytes](),
			wiregen.TypeRef[basic.Address](),
			wiregen.TypeRef[basic.User](),
		}
		r2.Enums = map[string]wiregen.EnumDef{"Status": {Values: []string{"active"}}}
		got := r2.GenerateDecoders()
		if got != baseline {
			t.Fatal("decoder imports/body not deterministic when type order changes")
		}
	}
}

// ============================================================
// (3) //wiregen:union + DiscriminatorMap handling
// ============================================================

func TestRT_UnionDecoder_AllVariants(t *testing.T) {
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
			"coverage":   "CoverageEvent",
			"notify":     "NotifyEvent",
			"scan:start": "ScanEvent",
			"scan:done":  "ScanEvent",
		},
	}

	dec := r.GenerateDecoders()
	// Check union decoder signature
	if !strings.Contains(dec, "export const decodeEventData: (type: string, v: unknown) => EventData") {
		t.Errorf("missing union decoder signature, got:\n%s", dec)
	}
	// Check all variant cases
	if !strings.Contains(dec, `case "coverage": return decodeCoverageEvent(v);`) {
		t.Errorf("missing coverage case, got:\n%s", dec)
	}
	if !strings.Contains(dec, `case "notify": return decodeNotifyEvent(v);`) {
		t.Errorf("missing notify case, got:\n%s", dec)
	}
	if !strings.Contains(dec, `case "scan:start": return decodeScanEvent(v);`) {
		t.Errorf("missing scan:start case, got:\n%s", dec)
	}
	if !strings.Contains(dec, `case "scan:done": return decodeScanEvent(v);`) {
		t.Errorf("missing scan:done case, got:\n%s", dec)
	}
	// Check unknown variant throws
	if !strings.Contains(dec, "default: throw new TypeError(`unknown EventData variant:") {
		t.Errorf("missing unknown variant handling, got:\n%s", dec)
	}
}

func TestRT_UnionType_Shape(t *testing.T) {
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
	types := r.GenerateTypes()
	if !strings.Contains(types, "export type EventData = CoverageEvent | NotifyEvent | ScanEvent;") {
		t.Errorf("union type alias missing, got:\n%s", types)
	}
}

func TestRT_UnionNoDiscriminatorMap_NoDecoder(t *testing.T) {
	// When DiscriminatorMap is nil for a union, should not emit decoder
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
	// No DiscriminatorMap
	dec := r.GenerateDecoders()
	if strings.Contains(dec, "decodeEventData") {
		t.Errorf("should NOT emit union decoder without DiscriminatorMap, got:\n%s", dec)
	}
}

// ============================================================
// (4) Type-safe registration — compile-time checks
// ============================================================

func TestRT_TypeRefCompileTimeSafety(t *testing.T) {
	// This test just verifies that TypeRef[T] returns correct metadata
	wt := wiregen.TypeRef[basic.User]()
	if wt.Name != "User" {
		t.Errorf("TypeRef[User].Name = %q, want %q", wt.Name, "User")
	}
	if wt.PkgPath != "github.com/cplieger/wiregen/testdata/basic" {
		t.Errorf("TypeRef[User].PkgPath = %q", wt.PkgPath)
	}
}

func TestRT_TypeRefPointer(t *testing.T) {
	// TypeRef with a pointer type should unwrap
	wt := wiregen.TypeRef[*basic.Address]()
	if wt.Name != "Address" {
		t.Errorf("TypeRef[*Address].Name = %q, want %q", wt.Name, "Address")
	}
}

func TestRT_UnregisteredTypeErrors(t *testing.T) {
	// If you register a type by WireType literal with wrong PkgPath, it should error
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{
		{PkgPath: "github.com/cplieger/wiregen/testdata/basic", Name: "NonExistentType"},
	}
	dir := t.TempDir()
	err := r.Generate(dir)
	if err == nil {
		t.Fatal("expected error when referencing non-existent type, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestRT_WrongPackagePathErrors(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{
		{PkgPath: "github.com/nonexistent/pkg", Name: "Foo"},
	}
	dir := t.TempDir()
	err := r.Generate(dir)
	if err == nil {
		t.Fatal("expected error when referencing non-existent package, got nil")
	}
}

// ============================================================
// (5) Fuzz-adjacent: exercise known edge cases programmatically
// ============================================================

func TestRT_EmptyRegistry(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	// No types, no PackagePaths
	dir := t.TempDir()
	err := r.Generate(dir)
	if err != nil {
		t.Fatalf("empty registry should generate fine, got: %v", err)
	}
}

func TestRT_ConstantsOnly(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.Constants = []wiregen.WireConst{
		{TSName: "FOO", Value: 42},
		{TSName: "BAR", Value: -1},
	}
	dir := t.TempDir()
	err := r.Generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "constants.gen.ts"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, "export const FOO = 42;") {
		t.Errorf("missing FOO, got:\n%s", content)
	}
	if !strings.Contains(content, "export const BAR = -1;") {
		t.Errorf("missing BAR, got:\n%s", content)
	}
}

func TestRT_GenerateDoesNotPanic_EmptyPackagePaths(t *testing.T) {
	// This tests internal robustness: when PackagePaths is empty but
	// Types have PkgPath set, it should auto-derive package paths.
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	// PackagePaths left empty — should derive from Types[].PkgPath
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface Address") {
		t.Errorf("auto-derived package path should work, got:\n%s", out)
	}
}

func TestRT_DashCommaFieldName(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DashComma]())
	out := r.GenerateTypes()
	// json:"-" should be excluded
	if strings.Contains(out, "hidden") || strings.Contains(out, "Hidden") {
		t.Errorf("json:\"-\" field should be excluded, got:\n%s", out)
	}
}

// ============================================================
// (6) Golden test meaningfulness
// ============================================================

func TestRT_GoldenNonTrivial(t *testing.T) {
	goldenTypes, err := os.ReadFile("testdata/golden/types.gen.ts")
	if err != nil {
		t.Fatal(err)
	}
	goldenDec, err := os.ReadFile("testdata/golden/decoders.gen.ts")
	if err != nil {
		t.Fatal(err)
	}

	gt := string(goldenTypes)
	gd := string(goldenDec)

	// Types golden should have multiple interfaces
	count := strings.Count(gt, "export interface")
	if count < 5 {
		t.Errorf("golden types.gen.ts has only %d interfaces, expected at least 5", count)
	}

	// Should have enum
	if !strings.Contains(gt, "export type Status") {
		t.Error("golden types.gen.ts missing enum type")
	}

	// Should have embedded fields
	if !strings.Contains(gt, "export interface WithEmbedding") {
		t.Error("golden types.gen.ts missing WithEmbedding")
	}

	// Decoders golden should have multiple decoders
	decCount := strings.Count(gd, "export const decode")
	if decCount < 5 {
		t.Errorf("golden decoders.gen.ts has only %d decoders, expected at least 5", decCount)
	}

	// Should have nested struct call
	if !strings.Contains(gd, "decodeAddress(o[\"address\"])") {
		t.Error("golden decoders.gen.ts missing nested decoder call")
	}

	// Should have decodeRecord for maps
	if !strings.Contains(gd, "decodeRecord(") {
		t.Error("golden decoders.gen.ts missing decodeRecord call")
	}

	// Should have decodeArray for slices
	if !strings.Contains(gd, "decodeArray(") {
		t.Error("golden decoders.gen.ts missing decodeArray call")
	}

	// Should have reqOneOf for enums
	if !strings.Contains(gd, "reqOneOf(") {
		t.Error("golden decoders.gen.ts missing reqOneOf call")
	}

	// Lines should be > 50 (non-trivial)
	if strings.Count(gd, "\n") < 50 {
		t.Error("golden decoders.gen.ts is too short to be meaningful")
	}
}
