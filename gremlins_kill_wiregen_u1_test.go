package wiregen

// Unit wiregen-u1: tests that kill surviving gremlins mutants in
// ast_engine.go, emitters.go, and wiregen.go. Internal test package so the
// unexported symbols under test (pathName, parseUnionDirective, generateTypes/
// generateDecoders via the exported Generate* wrappers, TypeRef, the Registry
// internals) are reachable. Every identifier defined here is prefixed
// gk_wiregen_u1_ so it never collides with the sibling units (u2/u3) that
// share this package directory.

import (
	"go/ast"
	"strings"
	"testing"
)

const (
	gk_wiregen_u1_basicPkg    = "github.com/cplieger/wiregen/testdata/basic"
	gk_wiregen_u1_edgesPkg    = "github.com/cplieger/wiregen/testdata/edges"
	gk_wiregen_u1_unionsPkg   = "github.com/cplieger/wiregen/testdata/unions"
	gk_wiregen_u1_crossrefPkg = "github.com/cplieger/wiregen/testdata/crossref"
)

func gk_wiregen_u1_eq(t *testing.T, fn, in, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s(%q) = %q, want %q", fn, in, got, want)
	}
}

func gk_wiregen_u1_contains(t *testing.T, label, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("%s: output is missing %q\n--- output ---\n%s", label, want, out)
	}
}

func gk_wiregen_u1_notContains(t *testing.T, label, out, bad string) {
	t.Helper()
	if strings.Contains(out, bad) {
		t.Errorf("%s: output unexpectedly contains %q\n--- output ---\n%s", label, bad, out)
	}
}

// gk_wiregen_u1_iface is a named interface used to exercise TypeRef's
// nil-reflect.TypeOf fallback path (wiregen.go:27).
type gk_wiregen_u1_iface interface{ gk_wiregen_u1_marker() }

// Test_gk_wiregen_u1_pathName pins (*Registry).pathName's snake-case
// conversion, focusing on the comparators u1 owns on wiregen.go lines
// 361/364/366 — the `ru <= 'Z'` outer-uppercase test and the `prev >= 'a'`,
// `prev <= 'z'`, `prev >= 'A'`, `prev <= 'Z'` neighbour tests. Each row is
// chosen so a single operator flip at exactly those columns changes the
// observable snake-case output. (Distinct from sibling u2, which owns the
// later-column runes[i+1] comparators on line 366.)
func Test_gk_wiregen_u1_pathName(t *testing.T) {
	r := &Registry{}
	cases := []struct{ in, want string }{
		// 361:22 `ru <= 'Z'` boundary: 'Z' is the upper bound of the outer
		// uppercase test. `< 'Z'` reclassifies 'Z' as non-uppercase and emits
		// it verbatim ("Z") instead of lowercasing it ("z").
		{"Z", "z"},
		// 364:13 `prev >= 'a'` boundary: prev 'a' is the lower bound of the
		// prev-lowercase test. `> 'a'` drops the camelCase underscore ("ab").
		{"aB", "a_b"},
		// 364:28 `prev <= 'z'` boundary: prev 'z' is the upper bound. `< 'z'`
		// drops the underscore ("zb").
		{"zB", "z_b"},
		// 366:20 `prev >= 'A'` (boundary `> 'A'` and negation `< 'A'`): prev
		// 'A' is the lower bound of the acronym-boundary else-if. With ru='B'
		// uppercase, prev='A', next 'c' lowercase the original inserts '_'.
		// `> 'A'` ('A' not > 'A') and `< 'A'` ('A' not < 'A') both drop it.
		{"ABc", "a_bc"},
		// 366:35 `prev <= 'Z'` (boundary `< 'Z'` and negation `> 'Z'`): prev
		// 'Z' is the upper bound. `< 'Z'` and `> 'Z'` ('Z' not > 'Z') both drop
		// the acronym '_'.
		{"ZBc", "z_bc"},
		// 366:20 negation false-side: prev '0' is below 'A'. The original
		// else-if is false (no '_'). Flipping `prev >= 'A'` to `prev < 'A'`
		// makes '0' satisfy it, so the mutant wrongly inserts '_' ("0_bc").
		{"0Bc", "0bc"},
		// 366:35 negation false-side: prev '[' (just above 'Z') passes
		// `prev >= 'A'` but the original `prev <= 'Z'` is false (no '_').
		// Flipping to `prev > 'Z'` makes '[' satisfy it, wrongly inserting '_'.
		{"[Bc", "[bc"},
	}
	for _, c := range cases {
		gk_wiregen_u1_eq(t, "pathName", c.in, r.pathName(c.in), c.want)
	}
}

// Test_gk_wiregen_u1_typeRefInterfaceFallback pins TypeRef's
// nil-reflect.TypeOf guard at wiregen.go:27 (`if t == nil`). For an interface
// type the zero value is a nil interface, so reflect.TypeOf returns nil and the
// code must fall back to reflect.TypeFor[T](). Negating the guard to `t != nil`
// skips that fallback, leaving t nil so the next t.Kind() call panics; the
// original returns the resolved WireType cleanly.
func Test_gk_wiregen_u1_typeRefInterfaceFallback(t *testing.T) {
	// error is a builtin interface: PkgPath "" and Name "error".
	got := TypeRef[error]()
	want := WireType{PkgPath: "", Name: "error"}
	if got != want {
		t.Errorf("TypeRef[error]() = %+v, want %+v", got, want)
	}

	// A named interface in this package: Name carries through.
	gotNamed := TypeRef[gk_wiregen_u1_iface]()
	if gotNamed.Name != "gk_wiregen_u1_iface" {
		t.Errorf("TypeRef[gk_wiregen_u1_iface]().Name = %q, want %q",
			gotNamed.Name, "gk_wiregen_u1_iface")
	}
}

// Test_gk_wiregen_u1_parseUnionDirective_zeroVariants pins the
// `len(ud.Variants) > 0` guard in parseUnionDirective (ast_engine.go:590). A
// directive with a discriminator but zero (all-empty) variants must return nil
// (no union). Flipping `> 0` to `>= 0` (always true) returns a bogus non-nil
// UnionDef with no variants.
func Test_gk_wiregen_u1_parseUnionDirective_zeroVariants(t *testing.T) {
	noVariants := &ast.CommentGroup{List: []*ast.Comment{
		{Text: "//wiregen:union discriminator=type variants="},
	}}
	if got := parseUnionDirective(noVariants); got != nil {
		t.Errorf("parseUnionDirective(discriminator-only) = %+v, want nil", got)
	}

	// Characterization: a directive WITH variants is accepted (so the test is
	// not trivially always-nil).
	withVariants := &ast.CommentGroup{List: []*ast.Comment{
		{Text: "//wiregen:union discriminator=type variants=A,B"},
	}}
	got := parseUnionDirective(withVariants)
	if got == nil {
		t.Fatalf("parseUnionDirective(with variants) = nil, want non-nil")
	}
	if got.Discriminator != "type" || len(got.Variants) != 2 ||
		got.Variants[0] != "A" || got.Variants[1] != "B" {
		t.Errorf("parseUnionDirective(with variants) = %+v, want {type,[A B]}", got)
	}
}

// Test_gk_wiregen_u1_enumDiscovery_emptyTypes pins the early-return guard in
// newASTEngine at ast_engine.go:55 (`len(r.Types) == 0 && len(r.PackagePaths)
// == 0`). With NO registered types but a real PackagePaths and an enum whose
// Values are empty, the original proceeds to load the package and
// auto-discover the enum's const values. Negating the second `== 0` to `!= 0`
// makes the guard true (Types empty AND PackagePaths non-empty), so the engine
// returns early and discovery never runs, leaving the enum value-less.
func Test_gk_wiregen_u1_enumDiscovery_emptyTypes(t *testing.T) {
	r := &Registry{}
	r.PackagePaths = []string{gk_wiregen_u1_crossrefPkg}
	r.Enums = map[string]EnumDef{"Color": {}} // empty Values -> discover
	out := r.GenerateTypes()

	// Original: discovery populates red/green/blue (source order).
	gk_wiregen_u1_contains(t, "55:enum-discovery", out,
		`export type Color = "red" | "green" | "blue";`)
	// Mutant early-returns -> no discovery -> empty union "Color = ;".
	gk_wiregen_u1_notContains(t, "55:enum-discovery", out, "export type Color = ;")
}

// Test_gk_wiregen_u1_dashCommaField pins the `len(parts) == 1` test in
// resolveStructFields at ast_engine.go:288. A field tagged `json:"-,"` (parts
// ["-",""]) is a field literally named "-" and must be EMITTED; the bare
// `json:"-"` skip is handled earlier (line 280), so line 288 only fires for the
// "-," form. Negating `== 1` to `!= 1` makes `wireName == "-" && len != 1` true
// for the "-," field, wrongly dropping it.
func Test_gk_wiregen_u1_dashCommaField(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js", BusImport: "./b.js"}
	r.PackagePaths = []string{gk_wiregen_u1_edgesPkg}
	r.Types = []WireType{{PkgPath: gk_wiregen_u1_edgesPkg, Name: "DashComma"}}
	out := r.GenerateTypes()

	gk_wiregen_u1_contains(t, "288:dash-comma", out, "export interface DashComma {")
	gk_wiregen_u1_contains(t, "288:dash-comma", out, "  name: string;")
	// The json:"-," field is named "-" and must appear; the mutant drops it.
	gk_wiregen_u1_contains(t, "288:dash-comma", out, "  -: string;")
}

// Test_gk_wiregen_u1_fieldDocAssociation pins the `name.Pos() == pos` match in
// fieldDocAtPos at ast_engine.go:519. The doc for each field must come from the
// field AT that position. Negating `==` to `!=` returns the FIRST other
// documented field's doc, swapping Alpha's and Beta's field docs. The existing
// suite only checks both doc strings are present (still true when swapped), so
// this asserts the exact Alpha block to detect the misassociation.
func Test_gk_wiregen_u1_fieldDocAssociation(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js"}
	r.PackagePaths = []string{gk_wiregen_u1_crossrefPkg}
	r.Types = []WireType{
		{PkgPath: gk_wiregen_u1_crossrefPkg, Name: "Alpha"},
		{PkgPath: gk_wiregen_u1_crossrefPkg, Name: "Beta"},
	}
	out := r.GenerateTypes()

	// Original: Alpha's path field carries Alpha's own field doc.
	gk_wiregen_u1_contains(t, "519:field-doc", out,
		"export interface Alpha {\n  /** AlphaPathDoc marks alpha. */\n  path: string;\n}")
	// Original: Beta's path field carries Beta's own field doc.
	gk_wiregen_u1_contains(t, "519:field-doc", out,
		"export interface Beta {\n  /** BetaPathDoc marks beta. */\n  path: string;\n}")
}

// Test_gk_wiregen_u1_typeKeyShortNameKey pins the `ut.Obj().Pkg() != nil` test
// in typeKey at ast_engine.go:650. For a named type with a package, typeKey
// must return the FULL "pkgpath.Name". Negating to `== nil` makes it return the
// short Name only. A TypeMapping keyed by the SHORT name "CustomID" is missed
// by the original full key (field falls through to unknown) but HIT by the
// mutant's short key.
func Test_gk_wiregen_u1_typeKeyShortNameKey(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js", BusImport: "./b.js"}
	r.PackagePaths = []string{gk_wiregen_u1_basicPkg}
	r.Types = []WireType{{PkgPath: gk_wiregen_u1_basicPkg, Name: "HasCustomMapped"}}
	// Keyed by the SHORT name; the original full typeKey ("...basic.CustomID")
	// never matches this, so the field resolves to unknown.
	r.TypeMappings = map[string]string{"CustomID": "GkU1ShortMapped"}
	out := r.GenerateTypes()

	gk_wiregen_u1_contains(t, "650:typekey", out, "id: unknown;")
	gk_wiregen_u1_notContains(t, "650:typekey", out, "GkU1ShortMapped")
}

// Test_gk_wiregen_u1_enumSortOrder pins the enum-name sort comparator in
// generateTypes at emitters.go:32 (the CONDITIONALS_NEGATION variant `<`->`>=`).
// Enums emit in ascending TS-name order; negating the comparator reverses it.
func Test_gk_wiregen_u1_enumSortOrder(t *testing.T) {
	r := &Registry{}
	r.Enums = map[string]EnumDef{
		"AaaEnum": {Values: []string{"a"}},
		"ZzzEnum": {Values: []string{"z"}},
	}
	out := r.GenerateTypes()

	ai := strings.Index(out, "export type AaaEnum")
	zi := strings.Index(out, "export type ZzzEnum")
	if ai < 0 || zi < 0 {
		t.Fatalf("missing enum declarations; AaaEnum@%d ZzzEnum@%d\n%s", ai, zi, out)
	}
	if ai >= zi {
		t.Errorf("enum order: AaaEnum@%d should precede ZzzEnum@%d (ascending)", ai, zi)
	}
}

// Test_gk_wiregen_u1_unionDocEmitted pins the `ti.Doc != ""` guard for union
// types in generateTypes at emitters.go:50. A documented union type must emit
// its JSDoc before the `export type` line. Negating `!= ""` to `== ""` skips
// the doc for any documented union.
func Test_gk_wiregen_u1_unionDocEmitted(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js", BusImport: "./b.js"}
	r.PackagePaths = []string{gk_wiregen_u1_unionsPkg}
	r.Types = []WireType{
		{PkgPath: gk_wiregen_u1_unionsPkg, Name: "CoverageEvent"},
		{PkgPath: gk_wiregen_u1_unionsPkg, Name: "NotifyEvent"},
		{PkgPath: gk_wiregen_u1_unionsPkg, Name: "ScanEvent"},
		{PkgPath: gk_wiregen_u1_unionsPkg, Name: "EventData"},
	}
	out := r.GenerateTypes()

	// Sanity: the union type itself is emitted.
	gk_wiregen_u1_contains(t, "50:union-doc", out,
		"export type EventData = CoverageEvent | NotifyEvent | ScanEvent;")
	// The union's doc text appears only via line 50; the mutant drops it.
	gk_wiregen_u1_contains(t, "50:union-doc", out,
		"EventData is a sealed interface for event payloads")
}

// Test_gk_wiregen_u1_emptyRegistryDecoderImports pins three len(...) > 0
// guards in generateDecoders for the no-types case: usedHelpers (131:22),
// used type names (161:15), and emitted enum consts (189:18). With an empty
// registry the decoder body is empty, so each guard is false in the original.
// Flipping any `> 0` to `>= 0` (always true) perturbs the import block.
func Test_gk_wiregen_u1_emptyRegistryDecoderImports(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js"}
	out := r.GenerateDecoders()

	// 131:22 — usedHelpers empty: original writes a clean "import { type
	// Decoder }". The mutant injects ", " -> "import { , type Decoder }".
	gk_wiregen_u1_contains(t, "131:helpers-import", out,
		`import { type Decoder } from "./v.js";`)

	// 161:15 — used type names empty: original emits NO `import type {` line.
	// The mutant emits `import type {  } from "./types.gen.js";`.
	gk_wiregen_u1_notContains(t, "161:types-import", out, "import type {")

	// 189:18 — emitted enum consts empty: original ends with `";\n\n` (import
	// line + the always-written blank line). The mutant appends one extra
	// newline, breaking the suffix.
	if !strings.HasSuffix(out, "\";\n\n") {
		t.Errorf("189:enum-const-spacing: output does not end with \"\\\";\\n\\n\"\n--- output ---\n%q", out)
	}
}

// Test_gk_wiregen_u1_structOutLiteralGuard pins the
// `len(reqFields) > 0 || len(optFields) > 0` guard in emitDecoder at
// emitters.go:211 (both `>` operators, boundary and negation). An empty struct
// emits `const out = {};` (the else branch); flipping either `> 0` to `>= 0`
// (always true) makes the populated `{ ... }` branch run instead, yielding
// `{\n  };`. A struct with only optional fields takes the populated branch in
// the original (`= {\n  };`); flipping the optFields `> 0` to `<= 0` (negation)
// makes the empty-literal else branch run (`= {};`).
func Test_gk_wiregen_u1_structOutLiteralGuard(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js", BusImport: "./b.js"}
	r.PackagePaths = []string{gk_wiregen_u1_edgesPkg}
	r.Types = []WireType{
		{PkgPath: gk_wiregen_u1_edgesPkg, Name: "EmptyStruct"},
		{PkgPath: gk_wiregen_u1_edgesPkg, Name: "AllOptional"},
	}
	out := r.GenerateDecoders()

	// 211:20 / 211:42 boundary + 211:42 negation (true-side): EmptyStruct has
	// no fields, so the original takes the else branch (`= {};`). Any of the
	// three mutations flips the guard true and emits `= {\n  };` instead.
	gk_wiregen_u1_contains(t, "211:empty-struct", out, "const out: EmptyStruct = {};")

	// 211:42 negation (false-side): AllOptional has only optional fields
	// (reqFields empty, optFields non-empty), so the original guard is true
	// and emits `= {\n  };`. The negation `len(optFields) <= 0` makes it false
	// -> `= {};`.
	gk_wiregen_u1_contains(t, "211:all-optional", out, "const out: AllOptional = {\n  };")
}
