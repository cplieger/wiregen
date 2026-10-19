package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/crossref"
)

func crossrefRegistry() *testRegistry {
	r := newTestRegistry(wiregen.WithValidatorsImport("./validators.js"))
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[crossref.Container](),
		wiregen.TypeRef[crossref.Item](),
		wiregen.TypeRef[crossref.Alpha](),
		wiregen.TypeRef[crossref.Beta](),
	}
	r.Enums = []wiregen.EnumRef{wiregen.StringEnum[crossref.Status]()}
	return r
}

// Regression: slice/map elements that are registered structs or enums must
// resolve to the element decoder, not the identity cast. The bug keyed the
// element lookup by full importpath.Type while r.typeNames/r.enums are keyed by
// short name, so every []Struct / map[string]Struct / []Enum silently emitted
// `(v) => v as unknown`.
func TestDecoders_SliceMapElementCrossRef(t *testing.T) {
	out := mustGen(t, crossrefRegistry().decodersFile)
	if strings.Contains(out, wiregenIdentityCast) {
		t.Errorf("identity cast leaked into element decoders:\n%s", out)
	}
	for _, want := range []string{
		`decodeArray(own(o, "items"), decodeItem,`,
		`decodeRecord(own(o, "by_key"), decodeItem,`,
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
	out := mustGen(t, crossrefRegistry().typesFile)
	for _, want := range []string{"AlphaPathDoc marks alpha", "BetaPathDoc marks beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing per-struct field doc %q (cross-contaminated?) in:\n%s", want, out)
		}
	}
}
