package wiregen

import (
	"go/ast"
	"testing"
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
