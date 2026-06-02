package wiregen_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
)

// --- Round 4: encoding/json promotion ambiguity and multi-level embedding ---
//
// Host types that embed two structs sharing a json tag are built via
// reflect.StructOf rather than written as literal Go structs: a literal
// struct with two embeds exposing the same json tag trips `go vet`'s
// structtag check (and is itself questionable Go), yet it is exactly the
// ambiguity we must verify wiregen handles. wiregen receives the same
// reflect.Type either way, so the coverage is identical.

func r4embed(t reflect.Type) reflect.StructField {
	return reflect.StructField{Name: t.Name(), Type: t, Anonymous: true}
}

func r4field(goName, jsonName string, t reflect.Type) reflect.StructField {
	return reflect.StructField{Name: goName, Type: t, Tag: reflect.StructTag(`json:"` + jsonName + `"`)}
}

func r4reg(types ...reflect.Type) *wiregen.Registry {
	return &wiregen.Registry{WireTypes: types, ValidatorsImport: "./v.js", BusImport: "./b.js"}
}

// Case 1: Two embeds at same depth, same json field name -> omit both (ambiguity).
type R4EmbA struct {
	Name string `json:"name"`
}
type R4EmbB struct {
	Name string `json:"name"`
}

var r4Ambiguous = reflect.StructOf([]reflect.StructField{
	r4embed(reflect.TypeFor[R4EmbA]()),
	r4embed(reflect.TypeFor[R4EmbB]()),
	r4field("ID", "id", reflect.TypeFor[int]()),
})

func TestR4PromotionAmbiguityOmitsBoth(t *testing.T) {
	out := r4reg(r4Ambiguous).GenerateTypes()
	if strings.Contains(out, "name") {
		t.Errorf("ambiguous promoted field 'name' should be omitted, got:\n%s", out)
	}
	if !strings.Contains(out, "id: number;") {
		t.Errorf("expected id field to remain, got:\n%s", out)
	}
}

// Case 2: Multi-level embedding A embeds B embeds C, all same json field; direct wins.
type R4Deep struct {
	Val int `json:"val"`
}
type R4Mid struct {
	R4Deep
	Val string `json:"val"`
}
type R4Top struct {
	R4Mid
	Val bool `json:"val"`
}

func TestR4MultiLevelEmbedDirectWins(t *testing.T) {
	out := r4reg(reflect.TypeFor[R4Top]()).GenerateTypes()
	if !strings.Contains(out, "val: boolean;") {
		t.Errorf("expected direct bool field to win, got:\n%s", out)
	}
	if count := strings.Count(out, "val:"); count != 1 {
		t.Errorf("expected exactly 1 val field, got %d in:\n%s", count, out)
	}
}

// Case 3: Embedded + explicit at multiple depths; direct field wins over all embeds.
type R4BaseLevel2 struct {
	X string `json:"x"`
	Y string `json:"y"`
}
type R4BaseLevel1 struct {
	R4BaseLevel2
	X int `json:"x"`
}
type R4TopExplicit struct {
	R4BaseLevel1
	X bool `json:"x"`
}

func TestR4ExplicitOverridesAllDepths(t *testing.T) {
	out := r4reg(reflect.TypeFor[R4TopExplicit]()).GenerateTypes()
	if !strings.Contains(out, "x: boolean;") {
		t.Errorf("expected direct bool field x at depth 0 to win, got:\n%s", out)
	}
	if !strings.Contains(out, "y: string;") {
		t.Errorf("expected promoted y field from depth 2, got:\n%s", out)
	}
	if count := strings.Count(out, "x:"); count != 1 {
		t.Errorf("expected exactly 1 x field, got %d in:\n%s", count, out)
	}
}

// Case 4: Override with differing types — direct field type wins.
type R4TypeBase struct {
	Val int `json:"val"`
}
type R4TypeOverride struct {
	R4TypeBase
	Val string `json:"val"`
}

func TestR4OverrideWithDifferingTypes(t *testing.T) {
	out := r4reg(reflect.TypeFor[R4TypeOverride]()).GenerateTypes()
	if !strings.Contains(out, "val: string;") {
		t.Errorf("expected direct string type to win over embedded int, got:\n%s", out)
	}
	if strings.Contains(out, "val: number;") {
		t.Errorf("embedded int type should not appear, got:\n%s", out)
	}
}

// Case 5: Pointer-embedded override — direct field wins.
type R4PtrBase struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}
type R4PtrOverride struct {
	*R4PtrBase
	Name int `json:"name"`
}

func TestR4PointerEmbeddedOverride(t *testing.T) {
	out := r4reg(reflect.TypeFor[R4PtrOverride]()).GenerateTypes()
	if !strings.Contains(out, "name: number;") {
		t.Errorf("expected direct int (number) to win over ptr-embedded string, got:\n%s", out)
	}
	if !strings.Contains(out, "age: number;") {
		t.Errorf("expected promoted age field from ptr-embedded struct, got:\n%s", out)
	}
}

// Case 6: json-tag-renamed collision with a direct field.
type R4TagBase struct {
	Foo string `json:"collision"`
}
type R4TagDirect struct {
	R4TagBase
	Bar string `json:"collision"`
}

func TestR4JsonTagRenamedCollision(t *testing.T) {
	out := r4reg(reflect.TypeFor[R4TagDirect]()).GenerateTypes()
	if !strings.Contains(out, "collision: string;") {
		t.Errorf("expected direct field with json tag 'collision' to win, got:\n%s", out)
	}
	if count := strings.Count(out, "collision:"); count != 1 {
		t.Errorf("expected exactly 1 collision field, got %d in:\n%s", count, out)
	}
}

// Case 7: Promotion ambiguity with non-colliding survivors.
type R4AmbigX struct {
	Shared string `json:"shared"`
	OnlyX  string `json:"only_x"`
}
type R4AmbigY struct {
	Shared string `json:"shared"`
	OnlyY  int    `json:"only_y"`
}

var r4AmbigHost = reflect.StructOf([]reflect.StructField{
	r4embed(reflect.TypeFor[R4AmbigX]()),
	r4embed(reflect.TypeFor[R4AmbigY]()),
	r4field("Direct", "direct", reflect.TypeFor[string]()),
})

func TestR4AmbiguityWithSurvivors(t *testing.T) {
	out := r4reg(r4AmbigHost).GenerateTypes()
	if strings.Contains(out, "shared") {
		t.Errorf("ambiguous 'shared' field should be omitted, got:\n%s", out)
	}
	if !strings.Contains(out, "only_x: string;") {
		t.Errorf("expected promoted only_x field, got:\n%s", out)
	}
	if !strings.Contains(out, "only_y: number;") {
		t.Errorf("expected promoted only_y field, got:\n%s", out)
	}
	if !strings.Contains(out, "direct: string;") {
		t.Errorf("expected direct field, got:\n%s", out)
	}
}

// Case 8: Direct field resolves ambiguity at deeper levels.
type R4ResolveA struct {
	Field string `json:"field"`
}
type R4ResolveB struct {
	Field int `json:"field"`
}

var r4ResolveHost = reflect.StructOf([]reflect.StructField{
	r4embed(reflect.TypeFor[R4ResolveA]()),
	r4embed(reflect.TypeFor[R4ResolveB]()),
	r4field("Field", "field", reflect.TypeFor[bool]()),
})

func TestR4DirectResolvesAmbiguity(t *testing.T) {
	out := r4reg(r4ResolveHost).GenerateTypes()
	if !strings.Contains(out, "field: boolean;") {
		t.Errorf("expected direct bool field to resolve ambiguity, got:\n%s", out)
	}
	if count := strings.Count(out, "field:"); count != 1 {
		t.Errorf("expected exactly 1 field, got %d in:\n%s", count, out)
	}
}

// Case 9: Three-level ambiguity at the same promoted depth.
type R4Inner1 struct {
	Deep string `json:"deep"`
}
type R4Inner2 struct {
	Deep int `json:"deep"`
}
type R4Wrap1 struct {
	R4Inner1
}
type R4Wrap2 struct {
	R4Inner2
}

var r4DeepAmbig = reflect.StructOf([]reflect.StructField{
	r4embed(reflect.TypeFor[R4Wrap1]()),
	r4embed(reflect.TypeFor[R4Wrap2]()),
})

func TestR4DeepAmbiguityAtSamePromotedDepth(t *testing.T) {
	out := r4reg(r4DeepAmbig).GenerateTypes()
	if strings.Contains(out, "deep") {
		t.Errorf("ambiguous 'deep' at same promoted depth should be omitted, got:\n%s", out)
	}
}

// Case 10: Determinism across complex embedding scenarios.
func TestR4EmbeddingDeterminism(t *testing.T) {
	makeReg := func() *wiregen.Registry {
		return r4reg(
			r4Ambiguous,
			reflect.TypeFor[R4Top](),
			reflect.TypeFor[R4TopExplicit](),
			reflect.TypeFor[R4PtrOverride](),
			r4AmbigHost,
			r4ResolveHost,
			r4DeepAmbig,
		)
	}
	typesRef := makeReg().GenerateTypes()
	decodersRef := makeReg().GenerateDecoders()
	for i := range 50 {
		if got := makeReg().GenerateTypes(); got != typesRef {
			t.Fatalf("iteration %d: GenerateTypes differs", i)
		}
		if got := makeReg().GenerateDecoders(); got != decodersRef {
			t.Fatalf("iteration %d: GenerateDecoders differs", i)
		}
	}
}

// Case 11: Decoders are also correct for ambiguity cases.
func TestR4AmbiguityDecoderCorrectness(t *testing.T) {
	dec := r4reg(r4Ambiguous).GenerateDecoders()
	if strings.Contains(dec, `"name"`) {
		t.Errorf("decoder should not reference ambiguous 'name' field, got:\n%s", dec)
	}
	if !strings.Contains(dec, `"id"`) {
		t.Errorf("decoder should reference non-ambiguous 'id' field, got:\n%s", dec)
	}
}
