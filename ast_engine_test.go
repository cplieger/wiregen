package wiregen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

// White-box (package wiregen) unit tests for the unexported AST-walk helpers
// in ast_engine.go. Shared helpers and package-path constants live in
// wiregen_internal_test.go.

// TestParseUnionDirective_requiresVariants pins parseUnionDirective's guard
// that a //wiregen:union directive yields a union only when it has both a
// discriminator and at least one non-empty variant.
func TestParseUnionDirective_requiresVariants(t *testing.T) {
	noVariants := &ast.CommentGroup{List: []*ast.Comment{
		{Text: "//wiregen:union discriminator=type variants="},
	}}
	if got := parseUnionDirective(noVariants); got != nil {
		t.Errorf("parseUnionDirective(discriminator-only) = %+v, want nil", got)
	}

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

// TestNewASTEngine_discoversEnumsWithoutRegisteredTypes pins that the engine
// still loads packages and auto-discovers enum values when only PackagePaths
// is set (no registered Types) — the early-return guard must require BOTH
// Types and PackagePaths to be empty.
func TestNewASTEngine_discoversEnumsWithoutRegisteredTypes(t *testing.T) {
	r := &Registry{}
	r.PackagePaths = []string{crossrefPkg}
	r.Enums = map[string]EnumDef{"Color": {}} // empty Values -> discover

	out := r.GenerateTypes()
	mustContain(t, "enum-discovery", out, `export type Color = "red" | "green" | "blue";`)
	mustNotContain(t, "enum-discovery", out, "export type Color = ;")
}

// TestResolveStructFields_dashCommaFieldEmitted pins that a field tagged
// json:"-," (a field literally named "-") is emitted, while the bare json:"-"
// skip is handled separately. Because "-" is not a valid TS identifier it is
// emitted as a quoted property name ("-": ...); the bare form (-: ...) would be
// invalid TypeScript.
func TestResolveStructFields_dashCommaFieldEmitted(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js", BusImport: "./b.js"}
	r.PackagePaths = []string{edgesPkg}
	r.Types = []WireType{{PkgPath: edgesPkg, Name: "DashComma"}}
	out := r.GenerateTypes()

	mustContain(t, "dash-comma", out, "export interface DashComma {")
	mustContain(t, "dash-comma", out, "  name: string;")
	mustContain(t, "dash-comma", out, "  \"-\": string;")
	mustNotContain(t, "dash-comma", out, "  -: string;")
}

// TestFieldDoc_scopedToDeclaringField pins that each field's JSDoc comes from
// that field's own declaration, not the first same-named field elsewhere in
// the package: Alpha.path and Beta.path must carry their own docs.
func TestFieldDoc_scopedToDeclaringField(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js"}
	r.PackagePaths = []string{crossrefPkg}
	r.Types = []WireType{
		{PkgPath: crossrefPkg, Name: "Alpha"},
		{PkgPath: crossrefPkg, Name: "Beta"},
	}
	out := r.GenerateTypes()

	mustContain(t, "field-doc", out,
		"export interface Alpha {\n  /** AlphaPathDoc marks alpha. */\n  path: string;\n}")
	mustContain(t, "field-doc", out,
		"export interface Beta {\n  /** BetaPathDoc marks beta. */\n  path: string;\n}")
}

// TestTypeKey_fullyQualifiesNamedType pins that typeKey returns the full
// "pkgpath.Name" for a named type with a package: a TypeMapping keyed by the
// SHORT name must therefore NOT match, so the field falls through to unknown.
func TestTypeKey_fullyQualifiesNamedType(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js", BusImport: "./b.js"}
	r.PackagePaths = []string{basicPkg}
	r.Types = []WireType{{PkgPath: basicPkg, Name: "HasCustomMapped"}}
	// Keyed by the SHORT name; the full typeKey never matches, so the field
	// resolves to unknown rather than the short-keyed mapping.
	r.TypeMappings = map[string]string{"CustomID": "ShortMapped"}
	out := r.GenerateTypes()

	mustContain(t, "typekey", out, "id: unknown;")
	mustNotContain(t, "typekey", out, "ShortMapped")
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

// TestResolveTaggedField_taggedFieldDominatesUntaggedAtEqualDepth pins that
// resolveTaggedField marks whether a field's wire name came from an explicit
// json tag, and that encoding/json's equal-depth promotion tiebreak then keeps
// the tagged field over an untagged one. Two fields are promoted at the same
// depth under the same wire name "shared": a tagged string field and an
// untagged number field. The tagged field must win, so the surviving field
// carries the string type. (Existing fixture coverage only counts the survivor,
// not which field's type wins, so the tag-detection here is asserted via the
// distinct survivor type.)
func TestResolveTaggedField_taggedFieldDominatesUntaggedAtEqualDepth(t *testing.T) {
	e := &astEngine{r: &Registry{}}
	tagged := types.NewField(token.NoPos, nil, "Renamed", types.Typ[types.String], false)
	untagged := types.NewField(token.NoPos, nil, "shared", types.Typ[types.Int], false)

	tf, ok1 := e.resolveTaggedField(tagged, `json:"shared"`, 1, nil, nil)
	uf, ok2 := e.resolveTaggedField(untagged, "", 1, nil, nil)
	if !ok1 || !ok2 {
		t.Fatalf("resolveTaggedField ok = (%v, %v), want (true, true)", ok1, ok2)
	}

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

	e := &astEngine{}
	got := e.findFieldDoc(field, fallback, allPkgs)
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
