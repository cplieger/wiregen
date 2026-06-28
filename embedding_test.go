package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/basic"
	"github.com/cplieger/wiregen/testdata/edges"
)

// Tests for embedded-struct flattening and encoding/json field-promotion
// semantics: deeper embeddings win at lower depth, equal-depth collisions are
// dropped, direct fields override embedded ones, and unexported embeds are
// skipped.

// ifaceBody returns the text of the named `export interface` block (from its
// opening line up to the closing brace), or "" when the interface is absent.
// It lets the promotion tests assert on a single interface without scanning
// lines inline.
func ifaceBody(out, name string) string {
	_, rest, ok := strings.Cut(out, "export interface "+name+" {")
	if !ok {
		return ""
	}
	if body, _, ok := strings.Cut(rest, "\n}"); ok {
		return body
	}
	return rest
}

func TestEmbeddedStructFlatten(t *testing.T) {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.WithEmbedding]()}
	out := r.GenerateTypes()
	if !strings.Contains(out, "id: number;") {
		t.Errorf("embedded Base.ID should be flattened, got:\n%s", out)
	}
	if !strings.Contains(out, "created_at: string;") {
		t.Errorf("embedded Base.CreatedAt should be flattened as string, got:\n%s", out)
	}
	if !strings.Contains(out, "name: string;") {
		t.Errorf("own field Name should be present, got:\n%s", out)
	}
}

func TestDeepEmbedding(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DeepA](), wiregen.TypeRef[edges.DeepB](), wiregen.TypeRef[edges.DeepC]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface DeepA") {
		t.Errorf("missing DeepA, got:\n%s", out)
	}
}

func TestPromotionAmbiguityOmitsBoth(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.Ambiguous](), wiregen.TypeRef[edges.AmbigLeft](), wiregen.TypeRef[edges.AmbigRight]())
	out := r.GenerateTypes()
	// "name" is promoted from both embeddings at equal depth → dropped; the
	// non-ambiguous direct "id" remains.
	if body := ifaceBody(out, "Ambiguous"); strings.Contains(body, "name") {
		t.Errorf("ambiguous 'name' field should be omitted in Ambiguous, got:\n%s", out)
	}
	if !strings.Contains(out, "id: number") {
		t.Errorf("non-ambiguous 'id' field should be present, got:\n%s", out)
	}
}

// TestDiamondEmbedOmitsSharedBase pins the diamond-embedding case: a common
// base (DiamondBase) reached through two sibling embeds (DiamondLeft and
// DiamondRight) is promoted via two equal-depth index paths, so encoding/json
// drops it as an ambiguous promotion. The shared base field must be omitted
// while the direct non-ambiguous "id" survives. A global visited set would
// walk the base once (first path wins) and wrongly emit its field.
func TestDiamondEmbedOmitsSharedBase(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.Diamond]())
	out := r.GenerateTypes()
	if body := ifaceBody(out, "Diamond"); strings.Contains(body, "base_field") {
		t.Errorf("diamond-shared base field should be omitted as ambiguous, got:\n%s", out)
	}
	if !strings.Contains(out, "id: number") {
		t.Errorf("non-ambiguous 'id' field should be present, got:\n%s", out)
	}
}

func TestDirectFieldWins(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DirectWins](), wiregen.TypeRef[edges.EmbBase]())
	out := r.GenerateTypes()
	// Direct field at depth 0 wins over embedded at depth 1.
	if !strings.Contains(out, "name: string") {
		t.Errorf("direct field should win, got:\n%s", out)
	}
}

func TestEmbedFieldOverride(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.EmbedOverride](), wiregen.TypeRef[edges.EmbedBase2]())
	out := r.GenerateTypes()
	body := ifaceBody(out, "EmbedOverride")
	// Direct x:int wins over embedded x:string — exactly one "x" field, typed number.
	if got := strings.Count(body, "x:"); got != 1 {
		t.Errorf("EmbedOverride should have exactly 1 'x' field, got %d in:\n%s", got, out)
	}
	if !strings.Contains(out, "x: number;") {
		t.Errorf("direct 'x' in EmbedOverride should be number, got:\n%s", out)
	}
}

func TestTwoLevelEmbedDepth(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.Level1](), wiregen.TypeRef[edges.Level2](), wiregen.TypeRef[edges.Level3]())
	out := r.GenerateTypes()
	body := ifaceBody(out, "Level1")
	// Level2.Name (depth 1) wins over Level3.Name (depth 2); email is direct.
	if !strings.Contains(body, "name:") {
		t.Errorf("Level1 should have 'name' from Level2, got:\n%s", out)
	}
	if !strings.Contains(body, "email:") {
		t.Errorf("Level1 should have 'email', got:\n%s", out)
	}
}

func TestEmbeddedPointerOverride(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasEmbedPtr]())
	out := r.GenerateTypes()
	body := ifaceBody(out, "HasEmbedPtr")
	// Direct Name wins over embedded *EmbedPtrBase.Name; id comes from the embed.
	if !strings.Contains(body, "name: string;") {
		t.Errorf("direct field should be present, got:\n%s", out)
	}
	if !strings.Contains(body, "id: number;") {
		t.Errorf("embedded ptr field 'id' should be present, got:\n%s", out)
	}
	if got := strings.Count(body, "name:"); got != 1 {
		t.Errorf("expected exactly 1 'name' field, got %d:\n%s", got, out)
	}
}

func TestPrivateEmbedSkipped(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasPrivateEmbed]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "name: string;") {
		t.Errorf("exported field should be present, got:\n%s", out)
	}
	if strings.Contains(out, "secret") {
		t.Errorf("unexported embedded struct's fields should NOT appear, got:\n%s", out)
	}
}

func TestOnlyUnexportedFields(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.OnlyUnexported]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface OnlyUnexported") {
		t.Errorf("should still produce interface, got:\n%s", out)
	}
}

func TestEmbeddedFieldWithDashTag(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.HasEmbedWithDash]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "visible: string;") {
		t.Errorf("visible field from embed should be present, got:\n%s", out)
	}
	if strings.Contains(out, "Hidden") || strings.Contains(out, "hidden") {
		t.Errorf("json:\"-\" field from embed should be excluded, got:\n%s", out)
	}
	if !strings.Contains(out, "extra: string;") {
		t.Errorf("direct extra field should be present, got:\n%s", out)
	}
}

// TestPromotion_TaggedDominatesUntaggedAtEqualDepth pins the cycle-1
// equalDepthWinner tiebreak: when two fields are promoted at the same depth
// with the same wire name, a tagged field dominates an untagged one (it is
// NOT dropped as ambiguous), regardless of embed declaration order.
func TestPromotion_TaggedDominatesUntaggedAtEqualDepth(t *testing.T) {
	for _, name := range []string{"TaggedDominatesA", "TaggedDominatesB"} {
		r := edgesReg(wiregen.WireType{PkgPath: "github.com/cplieger/wiregen/testdata/edges", Name: name})
		body := ifaceBody(r.GenerateTypes(), name)
		if got := strings.Count(body, "Name:"); got != 1 {
			t.Errorf("%s: tagged field should win at equal depth (exactly 1 Name), got %d:\n%s", name, got, body)
		}
	}
}

// TestTaggedEmbedBecomesNamedField pins the cycle-1 resolveEmbeddedField
// branch where an embed carrying an explicit json name is a NAMED nested
// field (encoding/json does not flatten a tagged embed).
func TestTaggedEmbedBecomesNamedField(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.TaggedEmbed](), wiregen.TypeRef[edges.Inner]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "meta: Inner;") {
		t.Errorf("tagged embed should become named nested field meta: Inner, got:\n%s", out)
	}
}

// TestDashEmbedSkipped pins the cycle-1 resolveEmbeddedField branch where an
// embed tagged json:"-" is dropped entirely (not flattened).
func TestDashEmbedSkipped(t *testing.T) {
	r := edgesReg(wiregen.TypeRef[edges.DashEmbed]())
	out := r.GenerateTypes()
	if !strings.Contains(out, "kept: string;") {
		t.Errorf("direct kept field should be present, got:\n%s", out)
	}
	if strings.Contains(out, "val") {
		t.Errorf("json:\"-\" embed should be excluded (no val), got:\n%s", out)
	}
}
