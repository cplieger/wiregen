package wiregen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/crossref"
)

// Enum members are discovered from the type's const block in source order.
func TestEnumValues_AutoDiscovered(t *testing.T) {
	r := newTestRegistry(wiregen.WithValidatorsImport("./validators.js"))
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Palette]()}
	r.Enums = []wiregen.EnumRef{wiregen.StringEnum[crossref.Color]()}

	out := mustGen(t, r.typesFile)
	if !strings.Contains(out, `export type Color = "red" | "green" | "blue";`) {
		t.Errorf("Color enum not auto-discovered in source order:\n%s", out)
	}
}

// Discovery scopes to root packages: a same-named enum in a transitive
// dependency (dep.Color) must not pollute crossref.Color's values.
func TestEnumValues_IgnoresDepCollision(t *testing.T) {
	r := newTestRegistry(wiregen.WithValidatorsImport("./validators.js"))
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Palette]()}
	r.Enums = []wiregen.EnumRef{wiregen.StringEnum[crossref.Color]()}

	out := mustGen(t, r.typesFile)
	if strings.Contains(out, "DEPRED") {
		t.Errorf("discovery leaked dep.Color values into crossref.Color:\n%s", out)
	}
	if !strings.Contains(out, `export type Color = "red" | "green" | "blue";`) {
		t.Errorf("expected crossref.Color values only:\n%s", out)
	}
}

// Generate omits events.gen.ts when there are no SSE events and
// constants.gen.ts when there are no constants.
func TestGenerate_SkipsEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	r := newTestRegistry(wiregen.WithValidatorsImport("./validators.js"))
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Item]()}
	// No Events, no Constants.

	if err := r.generate(t.Context(), dir); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	if !exists("types.gen.ts") || !exists("decoders.gen.ts") {
		t.Error("types.gen.ts and decoders.gen.ts should always be written")
	}
	if exists("events.gen.ts") {
		t.Error("events.gen.ts should be skipped when there are no SSE events")
	}
	if exists("constants.gen.ts") {
		t.Error("constants.gen.ts should be skipped when there are no constants")
	}
}
