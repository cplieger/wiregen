package wiregen_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/basic"
	"github.com/cplieger/wiregen/testdata/edges"
	"github.com/cplieger/wiregen/testdata/unions"
)

// Tests that generated output is deterministic: byte-identical run to run, and
// independent of the (map- and slice-) ordering of registered types and enums.

// TestGenerate_DeterministicAcrossRuns writes every output file twice (ten
// times) for a rich registry and asserts byte identity, exercising the sorting
// of types, enums, decoder imports, union variants, SSE events, and constants.
func TestGenerate_DeterministicAcrossRuns(t *testing.T) {
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

// TestGenerateTypes_TypeOrderIndependent: the types output does not depend on
// the order types are registered in.
func TestGenerateTypes_TypeOrderIndependent(t *testing.T) {
	makeReg := func(types []wiregen.WireType) *wiregen.Registry {
		r := wiregen.NewRegistry(
			wiregen.WithValidatorsImport("./v.js"),
			wiregen.WithBusImport("./b.js"),
		)
		r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
		r.Types = types
		r.Enums = map[string]wiregen.EnumDef{"Status": {Values: []string{"active"}}}
		return r
	}
	order1 := []wiregen.WireType{
		wiregen.TypeRef[basic.User](),
		wiregen.TypeRef[basic.Address](),
		wiregen.TypeRef[basic.Notification](),
	}
	order2 := []wiregen.WireType{
		wiregen.TypeRef[basic.Notification](),
		wiregen.TypeRef[basic.User](),
		wiregen.TypeRef[basic.Address](),
	}
	if makeReg(order1).GenerateTypes() != makeReg(order2).GenerateTypes() {
		t.Error("types output differs with shuffled type registration order")
	}
}

// TestGenerateDecoders_TypeOrderIndependent: the decoder output (body + sorted
// imports) does not depend on the order types are registered in.
func TestGenerateDecoders_TypeOrderIndependent(t *testing.T) {
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
		if r2.GenerateDecoders() != baseline {
			t.Fatal("decoder imports/body not deterministic when type order changes")
		}
	}
}

// TestGenerateTypes_EnumOrderIndependent: enum declarations sort stably
// regardless of Go map iteration order.
func TestGenerateTypes_EnumOrderIndependent(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
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
