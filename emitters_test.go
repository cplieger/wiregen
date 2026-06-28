package wiregen

import (
	"strings"
	"testing"
)

// White-box (package wiregen) unit tests for the unexported emitters in
// emitters.go. Shared helpers and package-path constants live in
// wiregen_internal_test.go.

// TestGenerateTypes_enumSortOrder pins that enums are emitted in ascending
// TS-name order.
func TestGenerateTypes_enumSortOrder(t *testing.T) {
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

// TestGenerateTypes_unionDocEmitted pins that a documented union type emits its
// JSDoc before the `export type` line.
func TestGenerateTypes_unionDocEmitted(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js", BusImport: "./b.js"}
	r.PackagePaths = []string{unionsPkg}
	r.Types = []WireType{
		{PkgPath: unionsPkg, Name: "CoverageEvent"},
		{PkgPath: unionsPkg, Name: "NotifyEvent"},
		{PkgPath: unionsPkg, Name: "ScanEvent"},
		{PkgPath: unionsPkg, Name: "EventData"},
	}
	out := r.GenerateTypes()

	mustContain(t, "union-doc", out,
		"export type EventData = CoverageEvent | NotifyEvent | ScanEvent;")
	mustContain(t, "union-doc", out, "EventData is a sealed interface for event payloads")
}

// TestGenerateDecoders_emptyRegistryImports pins the import block for the
// no-types case: with an empty registry the decoder body is empty, so no
// helper list, no `import type` line, and no enum-const block are emitted, and
// the output ends with a single trailing blank line after the import.
func TestGenerateDecoders_emptyRegistryImports(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js"}
	out := r.GenerateDecoders()

	mustContain(t, "helpers-import", out, `import { type Decoder } from "./v.js";`)
	mustNotContain(t, "types-import", out, "import type {")
	if !strings.HasSuffix(out, "\";\n\n") {
		t.Errorf("empty-registry decoder output should end with import + blank line\n--- output ---\n%q", out)
	}
}

// TestEmitDecoder_structLiteralShape pins the out-literal shape in emitDecoder:
// a struct with no fields emits `= {};` (empty literal) while a struct with
// only optional fields emits the populated `= {\n  };` block.
func TestEmitDecoder_structLiteralShape(t *testing.T) {
	r := &Registry{ValidatorsImport: "./v.js", BusImport: "./b.js"}
	r.PackagePaths = []string{edgesPkg}
	r.Types = []WireType{
		{PkgPath: edgesPkg, Name: "EmptyStruct"},
		{PkgPath: edgesPkg, Name: "AllOptional"},
	}
	out := r.GenerateDecoders()

	mustContain(t, "empty-struct", out, "const out: EmptyStruct = {};")
	mustContain(t, "all-optional", out, "const out: AllOptional = {\n  };")
}

// TestEmitEnumTypes_zeroValuesEmitsNever pins the zero-values guard in
// emitEnumTypes: a registered enum that resolves to no values emits the bottom
// type `= never;` (syntactically valid TS) rather than the invalid `= ;`.
func TestEmitEnumTypes_zeroValuesEmitsNever(t *testing.T) {
	r := &Registry{}
	r.Enums = map[string]EnumDef{"Phantom": {}}
	out := r.GenerateTypes()
	mustContain(t, "never-enum", out, "export type Phantom = never;")
	mustNotContain(t, "never-enum", out, "export type Phantom = ;")
}

// TestEmitOptionalField_nonIdentWireNameUsesBracketRef pins the
// non-identifier branch of tsMemberRef (reached via emitOptionalField): an
// optional field whose JSON key is not a valid TS identifier assigns through
// bracket access (out["content-type"] = ...) so the generated decoder stays
// valid TypeScript.
func TestEmitOptionalField_nonIdentWireNameUsesBracketRef(t *testing.T) {
	r := &Registry{}
	var w strings.Builder
	r.emitOptionalField(&w, &fieldInfo{WireName: "content-type", TSType: tsString, Optional: true}, "$.x")
	out := w.String()
	mustContain(t, "opt-nonident", out, `const contenttype = optStr(o, "content-type", "$.x");`)
	mustContain(t, "opt-nonident", out, `if (contenttype !== undefined) out["content-type"] = contenttype;`)
}

// TestEmitOptionalField_emptyVarNameFallsBackToFieldVal pins the localVarName
// empty-guard reached via emitOptionalField: an optional field whose JSON key
// sanitizes to "" (e.g. json:"_", which strings.Split on "_" yields two empty
// parts) must not emit an empty `const  = ...` identifier with an empty RHS.
// localVarName substitutes the fixed "fieldVal" name so the generated decoder
// stays valid TypeScript. The member-access target (out._) is unaffected — a
// lone underscore is itself a valid TS identifier — so only the local name is
// repaired.
func TestEmitOptionalField_emptyVarNameFallsBackToFieldVal(t *testing.T) {
	r := &Registry{}
	var w strings.Builder
	r.emitOptionalField(&w, &fieldInfo{WireName: "_", TSType: tsString, Optional: true}, "$.x")
	out := w.String()
	mustContain(t, "opt-empty-varname", out, `const fieldVal = optStr(o, "_", "$.x");`)
	mustContain(t, "opt-empty-varname", out, `if (fieldVal !== undefined) out._ = fieldVal;`)
	mustNotContain(t, "opt-empty-varname", out, "const  =")
}
