package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/basic"
	"github.com/cplieger/wiregen/testdata/edges"
)

// Tests for decoder emission: the primitive validators chosen per field, the
// empty / all-optional struct shapes, the decoder path-segment override, and
// the absence of an empty `import type {}` line.

func TestAllKindsDecoders(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllKinds]())
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "reqBool(o, \"bool\"") {
		t.Errorf("missing reqBool, got:\n%s", dec)
	}
	if !strings.Contains(dec, "reqNum(o, \"int\"") {
		t.Errorf("missing reqNum, got:\n%s", dec)
	}
	if !strings.Contains(dec, "reqStr(o, \"string\"") {
		t.Errorf("missing reqStr, got:\n%s", dec)
	}
}

func TestAllOptionalFieldsDecoder(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.AllOptional]())
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "const out: AllOptional = {") {
		t.Errorf("expected empty required block, got:\n%s", dec)
	}
}

func TestEmptyStructDecoder(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.EmptyStruct]())
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "decodeEmptyStruct") {
		t.Errorf("empty struct should still get a decoder, got:\n%s", dec)
	}
	if !strings.Contains(dec, "return out;") {
		t.Errorf("decoder should return out, got:\n%s", dec)
	}
}

func TestPathNameOverride(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.Inner]())
	r.PathNameOverride = map[string]string{"Inner": "custom_path"}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "$.custom_path") {
		t.Errorf("expected custom path, got:\n%s", dec)
	}
}

func TestNoEmptyTypeImport(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.HasBytes]()}
	dec := r.GenerateDecoders()
	// HasBytes references no other registered type, so type imports are empty —
	// the import block must be omitted, not emitted as `import type {}`.
	if strings.Contains(dec, "import type {  }") || strings.Contains(dec, "import type {}") {
		t.Errorf("should not emit empty type imports, got:\n%s", dec)
	}
}
