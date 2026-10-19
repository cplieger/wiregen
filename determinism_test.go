package wiregen_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/basic"
	"github.com/cplieger/wiregen/v4/testdata/crossref"
	"github.com/cplieger/wiregen/v4/testdata/edges"
	"github.com/cplieger/wiregen/v4/testdata/unions"
)

// Tests that generated output is deterministic: byte-identical run to run, and
// independent of the (map- and slice-) ordering of registered types and enums.

// TestGenerate_DeterministicAcrossRuns writes every output file twice (ten
// times) for a rich registry and asserts byte identity, exercising the sorting
// of types, enums, decoder imports, union variants, SSE events, and constants.
func TestGenerate_DeterministicAcrossRuns(t *testing.T) {
	makeReg := func() *testRegistry {
		r := newTestRegistry(
			wiregen.WithValidatorsImport("./v.js"),
		)
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
			wiregen.TypeRef[unions.Holder](),
		}
		r.Enums = []wiregen.EnumRef{
			wiregen.StringEnum[basic.Status](),
			wiregen.StringEnum[basic.Priority](),
			wiregen.StringEnum[edges.MyEnum](),
			wiregen.TextEnum[unions.Kind](),
		}
		r.Unions = []wiregen.Union{timingUnion(), shapeUnion()}
		r.Events = []wiregen.Event{
			r.event("notification", "Notification"),
			r.event("user:updated", "User"),
			r.event("holder", "Holder"),
		}
		r.Constants = []wiregen.WireConst{
			{TSName: "MSG_A", Value: 1},
			{TSName: "MSG_B", Value: 2},
			{TSName: "MSG_C", Value: 3},
		}
		return r
	}

	dir1 := t.TempDir()
	if err := makeReg().generate(t.Context(), dir1); err != nil {
		t.Fatal(err)
	}
	for i := range 10 {
		dir2 := t.TempDir()
		if err := makeReg().generate(t.Context(), dir2); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"types.gen.ts", "decoders.gen.ts", "events.gen.ts", "constants.gen.ts"} {
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
	makeReg := func(types []wiregen.WireType) *testRegistry {
		r := newTestRegistry(
			wiregen.WithValidatorsImport("./v.js"),
		)
		r.Types = types
		r.Enums = []wiregen.EnumRef{wiregen.StringEnum[basic.Status]()}
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
	if mustGen(t, makeReg(order1).typesFile) != mustGen(t, makeReg(order2).typesFile) {
		t.Error("types output differs with shuffled type registration order")
	}
}

// TestGenerateDecoders_TypeOrderIndependent: the decoder output (body + sorted
// imports) does not depend on the order types are registered in.
func TestGenerateDecoders_TypeOrderIndependent(t *testing.T) {
	r := newTestRegistry(
		wiregen.WithValidatorsImport("./v.js"),
	)
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[basic.User](),
		wiregen.TypeRef[basic.Address](),
		wiregen.TypeRef[basic.HasBytes](),
		wiregen.TypeRef[basic.HasMap](),
	}
	r.Enums = []wiregen.EnumRef{wiregen.StringEnum[basic.Status]()}
	baseline := mustGen(t, r.decodersFile)

	for range 20 {
		r2 := newTestRegistry(
			wiregen.WithValidatorsImport("./v.js"),
		)
		r2.Types = []wiregen.WireType{
			wiregen.TypeRef[basic.HasMap](),
			wiregen.TypeRef[basic.HasBytes](),
			wiregen.TypeRef[basic.Address](),
			wiregen.TypeRef[basic.User](),
		}
		r2.Enums = []wiregen.EnumRef{wiregen.StringEnum[basic.Status]()}
		if mustGen(t, r2.decodersFile) != baseline {
			t.Fatal("decoder imports/body not deterministic when type order changes")
		}
	}
}

// TestGenerateTypes_EnumOrderIndependent: enum declarations sort by TS name
// whatever order the enums are registered in.
func TestGenerateTypes_EnumOrderIndependent(t *testing.T) {
	refs := []wiregen.EnumRef{
		wiregen.StringEnum[basic.Status](),
		wiregen.StringEnum[edges.MyEnum](),
		wiregen.StringEnum[crossref.Color](),
		wiregen.StringEnum[basic.Priority](),
	}
	gen := func(refs []wiregen.EnumRef) string {
		r := newTestRegistry()
		r.Enums = refs
		return mustGen(t, r.typesFile)
	}
	baseline := gen(refs)
	reversed := slices.Clone(refs)
	slices.Reverse(reversed)
	if gen(reversed) != baseline {
		t.Error("enum output depends on registration order")
	}
	if c, m := strings.Index(baseline, "export type Color"), strings.Index(baseline, "export type MyEnum"); c < 0 || m < c {
		t.Errorf("enums not sorted by TS name:\n%s", baseline)
	}
}
