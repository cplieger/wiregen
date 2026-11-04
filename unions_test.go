package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/unions"
)

// timingUnion is the enum-tagged Timing fixture's Union.
func timingUnion() wiregen.Union {
	return wiregen.Union{
		Type: wiregen.TypeRef[unions.Timing](),
		Tag:  "kind",
		Cases: map[string][]string{
			"identity": {},
			"shift":    {"ms"},
			"linear":   {"scale", "offset_ms"},
		},
	}
}

// shapeUnion is the string-tagged Shape fixture's Union.
func shapeUnion() wiregen.Union {
	return wiregen.Union{
		Type: wiregen.TypeRef[unions.Shape](),
		Tag:  "type",
		Cases: map[string][]string{
			"circle": {"radius", "tags"},
			"square": {"side", "tags"},
		},
	}
}

// unionReg registers Holder, whose fields reach both unions.
func unionReg() *testRegistry {
	r := newTestRegistry()
	r.Types = []wiregen.WireType{wiregen.TypeRef[unions.Holder]()}
	r.Enums = []wiregen.EnumRef{wiregen.TextEnum[unions.Kind]()}
	r.Unions = []wiregen.Union{timingUnion(), shapeUnion()}
	return r
}

func TestUnion_TypeAlias(t *testing.T) {
	out := mustGen(t, unionReg().typesFile)
	for _, want := range []string{
		"export type Timing =\n" +
			"  | { kind: \"identity\" }\n" +
			"  | { kind: \"shift\"; ms?: number }\n" +
			"  | { kind: \"linear\"; scale?: number; offset_ms?: number };",
		"export type Shape =\n" +
			"  | { type: \"circle\"; radius?: number; tags: string[] }\n" +
			"  | { type: \"square\"; side?: number; tags: string[] };",
		"export interface Holder {\n  timing: Timing;\n  shapes: Shape[];\n}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("types missing %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "export interface Timing") {
		t.Errorf("a union type also emitted an interface:\n%s", out)
	}
}

func TestUnion_Decoder(t *testing.T) {
	out := mustGen(t, unionReg().decodersFile)
	for _, want := range []string{
		"export const decodeTiming: Decoder<Timing> = (v) => {",
		`const tag = reqOneOf(o, "kind", ["identity", "shift", "linear"] as const, "$.timing");`,
		`    case "identity": {`,
		`      refuseKeys(o, ["ms", "scale", "offset_ms"], "$.timing");`,
		`      const out: globalThis.Extract<Timing, { kind: "identity" }> = {`,
		`        kind: tag,`,
		`      refuseKeys(o, ["scale", "offset_ms"], "$.timing");`,
		`      const out: globalThis.Extract<Timing, { kind: "shift" }> = {`,
		`      refuseKeys(o, ["side"], "$.shape");`,
		"import { own, asObject, reqNum, reqOneOf, decodeArray, refuseKeys, type Decoder }",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("decoders missing %q, got:\n%s", want, out)
		}
	}
}

// TestUnion_CaseOrder: an enum tag orders the cases as the enum declares
// them, a string tag by value.
func TestUnion_CaseOrder(t *testing.T) {
	out := mustGen(t, unionReg().typesFile)
	if i, j := strings.Index(out, `"identity"`), strings.Index(out, `"linear"`); i < 0 || j < i {
		t.Errorf("Timing cases out of enum order:\n%s", out)
	}
	if i, j := strings.Index(out, `"circle"`), strings.Index(out, `"square"`); i < 0 || j < i {
		t.Errorf("Shape cases out of value order:\n%s", out)
	}
}

func TestUnion_Refusals(t *testing.T) {
	cases := []struct {
		name string
		edit func(*testRegistry)
		want string
	}{
		{"no such tag", func(r *testRegistry) { r.Unions[1].Tag = "kind" }, `has no field "kind"`},
		{"optional tag", func(r *testRegistry) {
			r.Types = append(r.Types, wiregen.TypeRef[unions.Untagged]())
			r.Unions = append(r.Unions, wiregen.Union{Type: wiregen.TypeRef[unions.Untagged](), Tag: "kind", Cases: map[string][]string{"a": {"name"}}})
		}, `tag "kind" must be a required string or enum field`},
		{"no case", func(r *testRegistry) { r.Unions[1].Cases = nil }, "declares no case"},
		{"enum cases differ", func(r *testRegistry) { delete(r.Unions[0].Cases, "linear") }, "must be exactly the members of tag enum Kind"},
		{"unknown field", func(r *testRegistry) { r.Unions[1].Cases["circle"] = []string{"diameter", "tags"} }, `lists "diameter", which is no field`},
		{"tag listed", func(r *testRegistry) { r.Unions[1].Cases["circle"] = []string{"type", "radius", "tags"} }, `lists the tag "type"`},
		{"field twice", func(r *testRegistry) { r.Unions[1].Cases["circle"] = []string{"radius", "radius", "tags"} }, `lists "radius" twice`},
		{"field in no case", func(r *testRegistry) { r.Unions[1].Cases["square"] = []string{"tags"} }, `field "side" is in no case`},
		{"always-written field in some cases", func(r *testRegistry) {
			r.Unions[1].Cases["square"] = []string{"side"}
		}, `field "tags" is not in every case, but encoding/json writes it in every case`},
		{"bad case name", func(r *testRegistry) {
			r.Unions[1].Cases["a b"] = r.Unions[1].Cases["circle"]
		}, `case "a b" must be letters`},
		{"declared twice", func(r *testRegistry) { r.Unions = append(r.Unions, shapeUnion()) }, "is declared twice"},
		{"no type", func(r *testRegistry) { r.Unions = append(r.Unions, wiregen.Union{Tag: "x"}) }, `union with tag "x" names no Type`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := unionReg()
			tc.edit(r)
			_, err := r.typesFile(t.Context())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestUnion_TypeJoinsTypes: a union's type is generated without being listed
// in Types.
func TestUnion_TypeJoinsTypes(t *testing.T) {
	r := newTestRegistry()
	r.Unions = []wiregen.Union{shapeUnion()}
	out := mustGen(t, r.typesFile)
	if !strings.Contains(out, "export type Shape =") {
		t.Errorf("union type not generated:\n%s", out)
	}
}

// TestUnion_RecursiveArbitraryCapped: a union reaching itself draws through a
// depth-capped oneof whose first branches are the leaf cases.
func TestUnion_RecursiveArbitraryCapped(t *testing.T) {
	r := newTestRegistry()
	r.Unions = []wiregen.Union{{
		Type: wiregen.TypeRef[unions.Tree](), Tag: "type",
		Cases: map[string][]string{"leaf": {"value"}, "node": {"children"}},
	}}
	out := mustGen(t, r.arbitrariesFile)
	entry := letrecEntry(t, out, "Tree")
	if !strings.HasPrefix(entry, "fc.oneof(") || !strings.Contains(out, "maxDepth: 5") {
		t.Fatalf("Tree arbitrary is not depth-capped:\n%s", out)
	}
	if !strings.Contains(out, `type: fc.constant("node" as const),`) {
		t.Errorf("tag not fixed per case:\n%s", out)
	}
}
