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
	r.Enums = []EnumRef{
		{Type: WireType{PkgPath: basicPkg, Name: "Status"}},
		{Type: WireType{PkgPath: basicPkg, Name: "Priority"}},
	}
	out := mustGen(t, r.typesFile)

	pi := strings.Index(out, "export type Priority")
	si := strings.Index(out, "export type Status")
	if pi < 0 || si < 0 {
		t.Fatalf("missing enum declarations; Priority@%d Status@%d\n%s", pi, si, out)
	}
	if pi >= si {
		t.Errorf("enum order: Priority@%d should precede Status@%d (ascending)", pi, si)
	}
}

// TestGenerateDecoders_emptyRegistryImports pins the import block for the
// no-types case: with an empty registry the decoder body is empty, so no
// helper list, no `import type` line, and no enum-const block are emitted, and
// the output ends with a single trailing blank line after the import.
func TestGenerateDecoders_emptyRegistryImports(t *testing.T) {
	r := &Registry{}
	out := mustGen(t, r.decodersFile)

	checkContains(t, "helpers-import", out, `import { type Decoder } from "./validators.gen.js";`)
	checkNotContains(t, "types-import", out, "import type {")
	if !strings.HasSuffix(out, "\";\n\n") {
		t.Errorf("empty-registry decoder output should end with import + blank line\n--- output ---\n%q", out)
	}
}

// TestEmitDecoder_structLiteralShape pins the out-literal shape in emitDecoder:
// a struct with no fields emits `= {};` (empty literal) while a struct with
// only optional fields emits the populated `= {\n  };` block.
func TestEmitDecoder_structLiteralShape(t *testing.T) {
	r := &Registry{}
	r.Types = []WireType{
		{PkgPath: edgesPkg, Name: "EmptyStruct"},
		{PkgPath: edgesPkg, Name: "AllOptional"},
	}
	out := mustGen(t, r.decodersFile)

	checkContains(t, "empty-struct", out, "const out: EmptyStruct = {};")
	checkContains(t, "all-optional", out, "const out: AllOptional = {\n  };")
}

// TestEmitOptionalField_nonIdentWireNameUsesBracketRef pins the
// non-identifier branch of tsMemberRef (reached via emitOptionalField): an
// optional field whose JSON key is not a valid TS identifier assigns through
// bracket access (out["content-type"] = ...) so the generated decoder stays
// valid TypeScript.
func TestEmitOptionalField_nonIdentWireNameUsesBracketRef(t *testing.T) {
	r := mustGenerator(t, &Registry{})
	var w strings.Builder
	r.emitOptionalField(&w, &fieldInfo{WireName: "content-type", TSType: tsString, Kind: kindString, Omittable: true}, "$.x", newUsedIdents())
	out := w.String()
	checkContains(t, "opt-nonident", out, `const $v = own(o, "content-type");`)
	checkContains(t, "opt-nonident", out, `if ($v !== undefined && $v !== null) out["content-type"] = reqStr(o, "content-type", "$.x");`)
}
