package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/crossref"
)

func crossrefRegistry() *wiregen.Registry {
	r := wiregen.NewRegistry(wiregen.WithValidatorsImport("./validators.js"))
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/crossref"}
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[crossref.Container](),
		wiregen.TypeRef[crossref.Item](),
		wiregen.TypeRef[crossref.Alpha](),
		wiregen.TypeRef[crossref.Beta](),
		wiregen.TypeRef[crossref.Outer](),
	}
	r.Enums = map[string]wiregen.EnumDef{"Status": {Values: []string{"on", "off"}}}
	return r
}

// Regression: slice/map elements that are registered structs or enums must
// resolve to the element decoder, not the identity cast. The bug keyed the
// element lookup by full importpath.Type while r.typeNames/r.Enums are keyed by
// short name, so every []Struct / map[string]Struct / []Enum silently emitted
// `(v) => v as unknown`.
func TestDecoders_SliceMapElementCrossRef(t *testing.T) {
	out := crossrefRegistry().GenerateDecoders()
	if strings.Contains(out, wiregenIdentityCast) {
		t.Errorf("identity cast leaked into element decoders:\n%s", out)
	}
	for _, want := range []string{
		`decodeArray(o["items"], decodeItem,`,
		`decodeRecord(o["by_key"], decodeItem,`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing element decoder %q in:\n%s", want, out)
		}
	}
}

const wiregenIdentityCast = "(v) => v as unknown"

// Regression: each field's JSDoc must come from its own struct, not the first
// same-named field declared earlier in the package.
func TestTypes_RecurringFieldDocScoped(t *testing.T) {
	out := crossrefRegistry().GenerateTypes()
	for _, want := range []string{"AlphaPathDoc marks alpha", "BetaPathDoc marks beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing per-struct field doc %q (cross-contaminated?) in:\n%s", want, out)
		}
	}
}

// Regression: an unregistered nested struct field passes through as unknown,
// not mis-decoded as a number via the reqNum fallback.
func TestDecoders_UnresolvedFieldIsUnknown(t *testing.T) {
	out := crossrefRegistry().GenerateDecoders()
	if !strings.Contains(out, `nested: o["nested"] as unknown`) {
		t.Errorf("unresolved nested struct not emitted as unknown:\n%s", out)
	}
	if strings.Contains(out, `nested: reqNum(o, "nested"`) {
		t.Errorf("unresolved nested struct mis-decoded as number:\n%s", out)
	}
}
