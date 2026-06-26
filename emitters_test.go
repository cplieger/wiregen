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
