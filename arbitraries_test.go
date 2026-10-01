package wiregen_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v3"
	"github.com/cplieger/wiregen/v3/testdata/basic"
	"github.com/cplieger/wiregen/v3/testdata/edges"
	"github.com/cplieger/wiregen/v3/testdata/unions"
)

// newArbRegistry is the arbitraries emitter's fixture: every field shape the
// emitter routes on (enum, bytes, time, raw, interface, slice, map, embedded
// struct, consumer-mapped type), the three recursion shapes (mutual cycle,
// self slice, self map) and a discriminated union.
func newArbRegistry() *wiregen.Registry {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./test-validators.js"),
		wiregen.WithBusImport("./test-bus.js"),
	)
	r.PackagePaths = []string{
		"github.com/cplieger/wiregen/v3/testdata/basic",
		"github.com/cplieger/wiregen/v3/testdata/edges",
		"github.com/cplieger/wiregen/v3/testdata/unions",
	}
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[basic.Address](),
		wiregen.TypeRef[basic.User](),
		wiregen.TypeRef[basic.HasBytes](),
		wiregen.TypeRef[basic.HasTime](),
		wiregen.TypeRef[basic.HasRaw](),
		wiregen.TypeRef[basic.HasInterface](),
		wiregen.TypeRef[basic.HasMap](),
		wiregen.TypeRef[basic.HasJSONString](),
		wiregen.TypeRef[basic.WithEmbedding](),
		wiregen.TypeRef[basic.HasCustomMapped](),
		wiregen.TypeRef[basic.HasMappedSlice](),
		wiregen.TypeRef[edges.CycleA](),
		wiregen.TypeRef[edges.CycleB](),
		wiregen.TypeRef[edges.SelfSlice](),
		wiregen.TypeRef[edges.SelfSliceRequired](),
		wiregen.TypeRef[edges.SelfMap](),
		wiregen.TypeRef[edges.SelfMapRequired](),
		wiregen.TypeRef[edges.MapOfStructs](),
		wiregen.TypeRef[edges.MapVal](),
		wiregen.TypeRef[unions.CoverageEvent](),
		wiregen.TypeRef[unions.NotifyEvent](),
		wiregen.TypeRef[unions.ScanEvent](),
		{PkgPath: "github.com/cplieger/wiregen/v3/testdata/unions", Name: "EventData"},
	}
	r.Enums = map[string]wiregen.EnumDef{
		"Status":   {Values: []string{"active", "inactive", "banned"}},
		"Priority": {Values: []string{"low", "medium", "high", "critical"}},
	}
	r.TypeMappings = map[string]string{
		// A mapping to a non-primitive TS type is the one shape wiregen has no
		// model for, so it is the shape the fixture has to carry.
		"github.com/cplieger/wiregen/v3/testdata/basic.CustomID": "Record<string, string>",
	}
	r.DiscriminatorMap = map[string]map[string]string{
		"EventData": {
			"coverage":   "CoverageEvent",
			"notify":     "NotifyEvent",
			"scan:start": "ScanEvent",
		},
	}
	return r
}

// TestGenerateArbitraries_DeterministicAcrossRuns holds the emitter to the
// contract determinism_test.go states for every other emitter: byte-identical
// run to run, and independent of registration and map iteration order.
func TestGenerateArbitraries_DeterministicAcrossRuns(t *testing.T) {
	baseline := mustGen(t, newArbRegistry().GenerateArbitraries)
	for i := range 5 {
		if got := mustGen(t, newArbRegistry().GenerateArbitraries); got != baseline {
			t.Fatalf("GenerateArbitraries is not deterministic (iteration %d)", i)
		}
	}
	shuffled := newArbRegistry()
	slices.Reverse(shuffled.Types)
	if got := mustGen(t, shuffled.GenerateArbitraries); got != baseline {
		t.Error("GenerateArbitraries output depends on type registration order")
	}
}

// TestGolden_Arbitraries pins the whole emitted artifact: the shape of a
// generated file IS its contract, so a change to it is a reviewable diff.
func TestGolden_Arbitraries(t *testing.T) {
	got := mustGen(t, newArbRegistry().GenerateArbitraries)
	goldenCompare(t, "testdata/golden/arbitraries.gen.ts", got)
}

// arbitraryTableKeys returns the keys of the emitted ARBITRARY_BY_TYPE table.
func arbitraryTableKeys(t *testing.T, out string) []string {
	t.Helper()
	const marker = "export const ARBITRARY_BY_TYPE: Record<string, fc.Arbitrary<unknown>> = {"
	_, body, found := strings.Cut(out, marker)
	if !found {
		t.Fatalf("no ARBITRARY_BY_TYPE table in output:\n%s", out)
	}
	if head, _, ok := strings.Cut(body, "};"); ok {
		body = head
	}
	keys := regexp.MustCompile(`(?m)^\s{2}(\w+):`).FindAllStringSubmatch(body, -1)
	out2 := make([]string, 0, len(keys))
	for _, k := range keys {
		out2 = append(out2, k[1])
	}
	return out2
}

// TestGenerateArbitraries_TableIsTotalOverTheRegistry is the invariant the whole
// reshape exists for: the table is derived from the registry, so it can neither
// miss a registered type nor carry one the registry dropped. The registry is the
// oracle, in both directions.
func TestGenerateArbitraries_TableIsTotalOverTheRegistry(t *testing.T) {
	r := newArbRegistry()
	got := arbitraryTableKeys(t, mustGen(t, r.GenerateArbitraries))

	want := make([]string, 0, len(r.Types)+len(r.Enums))
	for _, wt := range r.Types {
		want = append(want, wt.Name)
	}
	for name := range r.Enums {
		want = append(want, name)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("ARBITRARY_BY_TYPE keys = %v, want %v", got, want)
	}
}

// TestGenerateArbitraries_EnumDrawsOnlyFromEnumDefValues: the generated enum
// arbitrary is the registered value set and nothing else, so a value the
// decoder's reqOneOf would reject can never be drawn.
func TestGenerateArbitraries_EnumDrawsOnlyFromEnumDefValues(t *testing.T) {
	r := newArbRegistry()
	out := mustGen(t, r.GenerateArbitraries)
	for name, def := range r.Enums {
		want := "export const arb" + name + ": fc.Arbitrary<" + name + "> = fc.constantFrom("
		for i, v := range def.Values {
			if i > 0 {
				want += ", "
			}
			want += `"` + v + `"`
		}
		want += ");"
		if !strings.Contains(out, want) {
			t.Errorf("missing enum arbitrary for %s.\nwant line: %s\ngot:\n%s", name, want, out)
		}
	}
}

// TestGenerateArbitraries_RecursiveTypesAreTied: a mutual cycle and a self
// reference are emitted through fc.letrec's tie, never as a forward reference to
// a const declared later — which would be a temporal-dead-zone ReferenceError at
// module evaluation, not a type error any consumer's typecheck would catch.
func TestGenerateArbitraries_RecursiveTypesAreTied(t *testing.T) {
	out := mustGen(t, newArbRegistry().GenerateArbitraries)
	for _, want := range []string{
		`tie("CycleB")`,
		`tie("CycleA")`,
		`tie("SelfSlice")`,
		`tie("SelfMap")`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recursive reference %s not tied through fc.letrec; output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "export const arbCycleA: fc.Arbitrary<CycleA> = fc.record") {
		t.Error("CycleA emitted as a standalone const: a forward reference to arbCycleB is a TDZ ReferenceError")
	}
}

// letrecEntry returns one type's arbitrary expression from the emitted
// fc.letrec builder: the text after `  Name: ` up to the next two-space-indented
// key or the builder's close.
func letrecEntry(t *testing.T, out, name string) string {
	t.Helper()
	_, builder, found := strings.Cut(out, "}>((tie) => ({")
	if !found {
		t.Fatalf("no fc.letrec builder in output:\n%s", out)
	}
	_, body, found := strings.Cut(builder, "\n  "+name+": ")
	if !found {
		t.Fatalf("no letrec entry for %s in output:\n%s", name, out)
	}
	end := regexp.MustCompile(`\n  \w+: |\n}\)\);`).FindStringIndex(body)
	if end == nil {
		t.Fatalf("letrec entry for %s has no end", name)
	}
	return body[:end[0]]
}

// TestGenerateArbitraries_RecursionIsHardBounded: a depthIdentifier only BIASES
// fast-check toward shallow values, so a recursive type's generator terminated
// only probabilistically and a deep draw overflowed the stack. Every type that
// can reach itself is a depth-capped fc.oneof whose FIRST branch, the one forced
// at the cap, holds no recursive reference: a recursive collection is empty and
// a recursive optional field is absent.
func TestGenerateArbitraries_RecursionIsHardBounded(t *testing.T) {
	out := mustGen(t, newArbRegistry().GenerateArbitraries)
	const capped = "fc.oneof(\n    { maxDepth: "
	cases := []struct {
		name string
		ties []string
	}{
		{name: "SelfSlice", ties: []string{`tie("SelfSlice")`}},
		{name: "SelfSliceRequired", ties: []string{`tie("SelfSliceRequired")`}},
		{name: "SelfMap", ties: []string{`tie("SelfMap")`}},
		{name: "SelfMapRequired", ties: []string{`tie("SelfMapRequired")`}},
		{name: "CycleA", ties: []string{`tie("CycleB")`}},
		{name: "CycleB", ties: []string{`tie("CycleA")`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := letrecEntry(t, out, tc.name)
			if !strings.HasPrefix(entry, capped) {
				t.Fatalf("%s arbitrary is not a depth-capped oneof; got:\n%s", tc.name, entry)
			}
			branches := strings.Split(entry, "\n    fc.record(")
			if len(branches) != 3 {
				t.Fatalf("%s oneof has %d record branches, want 2 (leaf, full); got:\n%s", tc.name, len(branches)-1, entry)
			}
			leaf, full := branches[1], branches[2]
			for _, tie := range tc.ties {
				if strings.Contains(leaf, tie) {
					t.Errorf("%s leaf branch still recurses through %s; leaf:\n%s", tc.name, tie, leaf)
				}
				if !strings.Contains(full, tie) {
					t.Errorf("%s full branch lost %s; full:\n%s", tc.name, tie, full)
				}
			}
		})
	}
	for name, want := range map[string]string{
		"SelfSliceRequired": "children: fc.constant<SelfSliceRequired[]>([])",
		"SelfMapRequired":   "children: fc.constant<Record<string, SelfMapRequired>>({})",
	} {
		if leaf := letrecEntry(t, out, name); !strings.Contains(leaf, want) {
			t.Errorf("%s: a REQUIRED recursive collection must stay present and empty in the leaf (want %q); got:\n%s", name, want, leaf)
		}
	}
	for _, name := range []string{"Address", "User", "MapOfStructs"} {
		if entry := letrecEntry(t, out, name); strings.HasPrefix(entry, "fc.oneof(") {
			t.Errorf("non-recursive %s was wrapped in a depth cap; got:\n%s", name, entry)
		}
	}
}

// TestGenerate_ArbitrariesFileWrittenOnlyWhenNamed: the option is additive, so a
// consumer that does not name the file gets exactly the files it got before.
func TestGenerate_ArbitrariesFileWrittenOnlyWhenNamed(t *testing.T) {
	t.Run("named", func(t *testing.T) {
		dir := t.TempDir()
		r := newArbRegistry()
		r.ArbitrariesFilename = "arbitraries.gen.ts"
		if err := r.Generate(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.ReadFile(filepath.Join(dir, "arbitraries.gen.ts")); err != nil {
			t.Fatalf("arbitraries file not written: %v", err)
		}
	})
	t.Run("unnamed", func(t *testing.T) {
		dir := t.TempDir()
		if err := newArbRegistry().Generate(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.ReadFile(filepath.Join(dir, "arbitraries.gen.ts")); err == nil {
			t.Error("arbitraries file written with no ArbitrariesFilename set")
		}
	})
}

// TestWithArbitrariesOptions: the two option knobs land on the registry, and the
// import specifier defaults to fast-check.
func TestWithArbitrariesOptions(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithArbitrariesFile("arbs.ts"),
		wiregen.WithArbitrariesImport("fast-check/lite"),
	)
	if r.ArbitrariesFilename != "arbs.ts" {
		t.Errorf("ArbitrariesFilename = %q, want %q", r.ArbitrariesFilename, "arbs.ts")
	}
	if r.ArbitrariesImport != "fast-check/lite" {
		t.Errorf("ArbitrariesImport = %q, want %q", r.ArbitrariesImport, "fast-check/lite")
	}
	out := mustGen(t, newArbRegistry().GenerateArbitraries)
	if !strings.Contains(out, `import fc from "fast-check";`) {
		t.Errorf("default arbitraries import is not fast-check; got:\n%s", firstLines(out, 5))
	}
}
