package wiregen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/crossref"
)

// Enum Values are auto-discovered from the type's const block (source order)
// when left empty.
func TestEnumValues_AutoDiscovered(t *testing.T) {
	r := wiregen.NewRegistry(wiregen.WithValidatorsImport("./validators.js"))
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/crossref"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Palette]()}
	r.Enums = map[string]wiregen.EnumDef{"Color": {}} // empty → discover

	out := r.GenerateTypes()
	if !strings.Contains(out, `export type Color = "red" | "green" | "blue";`) {
		t.Errorf("Color enum not auto-discovered in source order:\n%s", out)
	}
}

// Discovery scopes to root packages: a same-named enum in a transitive
// dependency (dep.Color) must not pollute crossref.Color's values.
func TestEnumValues_IgnoresDepCollision(t *testing.T) {
	r := wiregen.NewRegistry(wiregen.WithValidatorsImport("./validators.js"))
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/crossref"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Palette]()}
	r.Enums = map[string]wiregen.EnumDef{"Color": {}}

	out := r.GenerateTypes()
	if strings.Contains(out, "DEPRED") {
		t.Errorf("discovery leaked dep.Color values into crossref.Color:\n%s", out)
	}
	if !strings.Contains(out, `export type Color = "red" | "green" | "blue";`) {
		t.Errorf("expected crossref.Color values only:\n%s", out)
	}
}

// Explicit Values always win over discovery.
func TestEnumValues_ExplicitWins(t *testing.T) {
	r := wiregen.NewRegistry(wiregen.WithValidatorsImport("./validators.js"))
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/crossref"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Palette]()}
	r.Enums = map[string]wiregen.EnumDef{"Color": {Values: []string{"red", "blue"}}}

	out := r.GenerateTypes()
	if !strings.Contains(out, `export type Color = "red" | "blue";`) {
		t.Errorf("explicit Values should override discovery:\n%s", out)
	}
}

// PackagePaths is derived from the registered types' PkgPaths when omitted.
func TestPackagePaths_AutoDerived(t *testing.T) {
	r := wiregen.NewRegistry(wiregen.WithValidatorsImport("./validators.js"))
	// No PackagePaths set — engine derives it from TypeRef PkgPath.
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Item]()}

	out := r.GenerateTypes()
	if !strings.Contains(out, "export interface Item {") {
		t.Errorf("type not generated with auto-derived PackagePaths:\n%s", out)
	}
}

// Generate omits registry.gen.ts when there are no SSE events and
// constants.gen.ts when there are no constants.
func TestGenerate_SkipsEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	r := wiregen.NewRegistry(wiregen.WithValidatorsImport("./validators.js"))
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Item]()}
	// No SSEEvents, no Constants.

	if err := r.Generate(dir); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	if !exists("types.gen.ts") || !exists("decoders.gen.ts") {
		t.Error("types.gen.ts and decoders.gen.ts should always be written")
	}
	if exists("registry.gen.ts") {
		t.Error("registry.gen.ts should be skipped when there are no SSE events")
	}
	if exists("constants.gen.ts") {
		t.Error("constants.gen.ts should be skipped when there are no constants")
	}
}
