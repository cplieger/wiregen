package wiregen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// White-box (package wiregen) unit tests for the unexported AST-walk helpers
// in ast_engine.go. Shared helpers and package-path constants live in
// wiregen_internal_test.go.

// TestNewASTEngine_discoversEnumsWithoutRegisteredTypes pins that an enum
// registered with no type loads its own package and discovers its members.
func TestNewASTEngine_discoversEnumsWithoutRegisteredTypes(t *testing.T) {
	r := &Registry{}
	r.Enums = []EnumRef{{Type: WireType{PkgPath: crossrefPkg, Name: "Color"}}}

	out := mustGen(t, r.typesFile)
	checkContains(t, "enum-discovery", out, `export type Color = "red" | "green" | "blue";`)
}

// TestResolveStructFields_dashCommaFieldEmitted pins that a field tagged
// json:"-," (a field literally named "-") is emitted, while the bare json:"-"
// skip is handled separately. Because "-" is not a valid TS identifier it is
// emitted as a quoted property name ("-": ...); the bare form (-: ...) would be
// invalid TypeScript.
func TestResolveStructFields_dashCommaFieldEmitted(t *testing.T) {
	r := &Registry{}
	r.Types = []WireType{{PkgPath: edgesPkg, Name: "DashComma"}}
	out := mustGen(t, r.typesFile)

	checkContains(t, "dash-comma", out, "export interface DashComma {")
	checkContains(t, "dash-comma", out, "  name: string;")
	checkContains(t, "dash-comma", out, "  \"-\": string;")
	checkNotContains(t, "dash-comma", out, "  -: string;")
}

// TestFieldDoc_scopedToDeclaringField pins that each field's JSDoc comes from
// that field's own declaration, not the first same-named field elsewhere in
// the package: Alpha.path and Beta.path must carry their own docs.
func TestFieldDoc_scopedToDeclaringField(t *testing.T) {
	r := &Registry{}
	r.Types = []WireType{
		{PkgPath: crossrefPkg, Name: "Alpha"},
		{PkgPath: crossrefPkg, Name: "Beta"},
	}
	out := mustGen(t, r.typesFile)

	checkContains(t, "field-doc", out,
		"export interface Alpha {\n  /** AlphaPathDoc marks alpha. */\n  path: string;\n}")
	checkContains(t, "field-doc", out,
		"export interface Beta {\n  /** BetaPathDoc marks beta. */\n  path: string;\n}")
}

// TestTypeKey_fullyQualifiesNamedType pins that typeKey returns the full
// "pkgpath.Name" for a named type with a package: a mapping keyed by the
// SHORT name must therefore NOT match, so the field is refused as unmapped.
func TestTypeKey_fullyQualifiesNamedType(t *testing.T) {
	r := &Registry{}
	r.Types = []WireType{{PkgPath: basicPkg, Name: "HasCustomMapped"}}
	r.Mappings = map[string]Mapping{"CustomID": {TS: "ShortMapped"}}
	_, err := r.typesFile(t.Context())
	if err == nil || !strings.Contains(err.Error(), basicPkg+".HasCustomMapped.ID: struct "+basicPkg+".CustomID is neither registered nor mapped") {
		t.Errorf("a short-keyed mapping matched or the refusal is missing: %v", err)
	}
}

// TestPackagesError_formatsErrorsWithAndWithoutPosition pins how packagesError
// assembles a load/type-error message: an error carrying a source position is
// rendered "wiregen: package <path>: <pos>: <msg>" (the position lets a
// consumer jump to the offending source), while an error with no position is
// rendered "wiregen: package <path>: <msg>" with no stray ": " separator. Every
// error reported for the package is aggregated into the returned error.
func TestPackagesError_formatsErrorsWithAndWithoutPosition(t *testing.T) {
	pkgs := []*packages.Package{{
		PkgPath: "example.com/broken",
		Errors: []packages.Error{
			{Pos: "broken.go:3:9", Msg: "undefined: Foo"},
			{Pos: "", Msg: "could not import dep"},
		},
	}}
	err := packagesError(pkgs)
	if err == nil {
		t.Fatal("packagesError returned nil, want an aggregated error")
	}
	want := "wiregen: package example.com/broken: broken.go:3:9: undefined: Foo\n" +
		"wiregen: package example.com/broken: could not import dep"
	if got := err.Error(); got != want {
		t.Errorf("packagesError =\n%q\nwant\n%q", got, want)
	}
}

// TestResolveNamedField_taggedFieldDominatesUntaggedAtEqualDepth pins that
// resolveNamedField marks whether a field's wire name came from an explicit
// json tag, and that encoding/json's equal-depth promotion tiebreak then keeps
// the tagged field over an untagged one. Two fields are promoted at the same
// depth under the same wire name "shared": a tagged string field and an
// untagged number field. The tagged field must win, so the surviving field
// carries the string type.
func TestResolveNamedField_taggedFieldDominatesUntaggedAtEqualDepth(t *testing.T) {
	e := &astEngine{gen: mustGenerator(t, &Registry{})}
	tagged := types.NewField(token.NoPos, nil, "Renamed", types.Typ[types.String], false)
	untagged := types.NewField(token.NoPos, nil, "shared", types.Typ[types.Int], false)

	tf := e.resolveNamedField(tagged, parseJSONTag(`json:"shared"`), 1, nil, nil)
	uf := e.resolveNamedField(untagged, parseJSONTag(""), 1, nil, nil)

	got := dedupJSONFields([]fieldInfo{tf, uf})
	if len(got) != 1 {
		t.Fatalf("dedupJSONFields kept %d fields, want 1: %+v", len(got), got)
	}
	if got[0].TSType != tsString {
		t.Errorf("equal-depth survivor TSType = %q, want %q (the tagged field must dominate the untagged one)",
			got[0].TSType, tsString)
	}
}

// TestFindFieldDoc_resolvesFromDeclaringPackage pins that findFieldDoc reads a
// field's JSDoc from the package where the field is DECLARED — looked up in
// allPkgs by the field's own package path — not from the fallback package (the
// enclosing type's package). This is the cross-package embedded-field path: the
// field's declaration and its doc comment live in a different package than the
// type that embeds it, so the fallback package does not contain the field.
func TestFindFieldDoc_resolvesFromDeclaringPackage(t *testing.T) {
	const src = `package decl

type Embedded struct {
	// FieldComment documents Tag.
	Tag string
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "decl.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse fixture source: %v", err)
	}
	pos := fieldNamePos(t, file, "Tag")

	declPkg := types.NewPackage("example.com/decl", "decl")
	field := types.NewVar(pos, declPkg, "Tag", types.Typ[types.String])

	allPkgs := map[string]*packages.Package{
		"example.com/decl": {PkgPath: "example.com/decl", Syntax: []*ast.File{file}},
	}
	// fallback is the enclosing type's package; it does NOT declare Tag.
	fallback := &packages.Package{PkgPath: "example.com/outer"}

	got := findFieldDoc(field, fallback, allPkgs)
	want := "/** FieldComment documents Tag. */\n"
	if got != want {
		t.Errorf("findFieldDoc = %q, want %q (doc must come from the declaring package, not the fallback)", got, want)
	}
}

// fieldNamePos returns the position of the struct-field name identifier `name`
// in file, matching how fieldDocAtPos scopes a doc comment to its field.
func fieldNamePos(t *testing.T, file *ast.File, name string) token.Pos {
	t.Helper()
	var pos token.Pos
	ast.Inspect(file, func(n ast.Node) bool {
		field, ok := n.(*ast.Field)
		if !ok {
			return true
		}
		for _, id := range field.Names {
			if id.Name == name {
				pos = id.Pos()
				return false
			}
		}
		return true
	})
	if !pos.IsValid() {
		t.Fatalf("field %q not found in fixture source", name)
	}
	return pos
}
