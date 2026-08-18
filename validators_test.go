package wiregen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v3"
	"github.com/cplieger/wiregen/v3/testdata/basic"
)

// validatorsContract is the exact set of names the validators module MUST
// export: the 11 helper functions plus the Decoder<T> type alias. The
// generated decoders import a referenced subset of these by name, so the
// starter must export the full set for any generated import to resolve.
var validatorsContract = []string{
	"asObject", "asArray",
	"reqStr", "reqNum", "reqBool",
	"optStr", "optNum", "optBool",
	"reqOneOf",
	"decodeArray", "decodeRecord",
}

func validatorsRegistry() *wiregen.Registry {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./test-validators.js"),
		wiregen.WithBusImport("./test-bus.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/v3/testdata/basic"}
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[basic.Address](),
		wiregen.TypeRef[basic.User](),
		wiregen.TypeRef[basic.Notification](),
	}
	r.Enums = map[string]wiregen.EnumDef{
		"Status": {Values: []string{"active", "inactive", "banned"}},
	}
	r.SSEEvents = []wiregen.SSERegEntry{
		{EventType: "notification", TypeName: "Notification"},
	}
	return r
}

// (a) The module exports all 11 contract functions plus the Decoder<T> type.
func TestGenerateValidators_ExportsFullContract(t *testing.T) {
	out := wiregen.NewRegistry().GenerateValidators()

	for _, name := range validatorsContract {
		if !strings.Contains(out, "export function "+name+"(") &&
			!strings.Contains(out, "export function "+name+"<") {
			t.Errorf("validators module missing exported function %q", name)
		}
	}

	if !strings.Contains(out, "export type Decoder<T> = (v: unknown) => T;") {
		t.Errorf("validators module missing `export type Decoder<T> = (v: unknown) => T;`")
	}

	// reqOneOf must be generic over the enum value type.
	if !strings.Contains(out, "export function reqOneOf<T extends string>(") {
		t.Errorf("reqOneOf must be declared as `reqOneOf<T extends string>`")
	}
	// decodeArray / decodeRecord must be generic.
	if !strings.Contains(out, "export function decodeArray<T>(") {
		t.Errorf("decodeArray must be declared as `decodeArray<T>`")
	}
	if !strings.Contains(out, "export function decodeRecord<T>(") {
		t.Errorf("decodeRecord must be declared as `decodeRecord<T>`")
	}
}

// (b) The module is library-owned: it carries the generated-file banner
// (default or the consumer's WithHeaderComment).
func TestGenerateValidators_GeneratedBanner(t *testing.T) {
	out := wiregen.NewRegistry().GenerateValidators()
	if !strings.Contains(out, "DO NOT EDIT") {
		t.Errorf("validators module must carry the DO-NOT-EDIT banner; got header:\n%s", firstLines(out, 4))
	}

	custom := "// custom banner\n\n"
	r := wiregen.NewRegistry(wiregen.WithHeaderComment(custom))
	if got := r.GenerateValidators(); !strings.HasPrefix(got, custom) {
		t.Errorf("validators module must use the consumer's HeaderComment; got header:\n%s", firstLines(got, 3))
	}
}

// (c) Default Generate(outDir) without WithValidatorsFile writes no
// validators file; with it, the module lands at the configured path (which
// may be outside outDir) on every run, banner + constant body.
func TestGenerate_ValidatorsFile(t *testing.T) {
	dir := t.TempDir()
	r := validatorsRegistry()
	if err := r.Generate(dir); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, fn := range []string{"validators.ts", "validators.gen.ts"} {
		if _, err := os.Stat(filepath.Join(dir, fn)); !os.IsNotExist(err) {
			t.Errorf("Generate without WithValidatorsFile must not create %q (stat err=%v)", fn, err)
		}
	}

	// Opt in, targeting a parent-relative path (the consumer layout: the
	// validators module lives beside hand-written source, one level above
	// the wire/ output dir).
	root := t.TempDir()
	wireDir := filepath.Join(root, "wire")
	if err := os.MkdirAll(wireDir, 0o750); err != nil {
		t.Fatal(err)
	}
	r2 := validatorsRegistry()
	r2.ValidatorsFilename = "../validators.ts"
	if err := r2.Generate(wireDir); err != nil {
		t.Fatalf("Generate with ValidatorsFilename: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "validators.ts"))
	if err != nil {
		t.Fatalf("validators module not written at ../validators.ts: %v", err)
	}
	if want := r2.GenerateValidators(); string(got) != want {
		// Print both sides with a direction legend: the written file and the
		// generator's own output are two computations of one artifact, and a
		// bare "they differ" leaves the reader no way to tell WHICH drifted.
		t.Errorf("written validators module differs from GenerateValidators() output\n--- want (GenerateValidators) ---\n%s\n+++ got (written file) +++\n%s", want, got)
	}
	if !strings.Contains(string(got), "DO NOT EDIT") {
		t.Errorf("written validators module missing the DO-NOT-EDIT banner")
	}

	// Regeneration overwrites deterministically (library-owned, never a
	// clobber hazard).
	if err := r2.Generate(wireDir); err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	got2, err := os.ReadFile(filepath.Join(root, "validators.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got2) != string(got) {
		t.Errorf("regenerated validators module is not byte-identical")
	}
}

// (d) The emitted helper names are a SUPERSET of the names a generated
// decoders.gen.ts imports, so any generated decoder import resolves against
// the starter.
func TestGenerateValidators_SupersetOfDecoderImports(t *testing.T) {
	r := validatorsRegistry()
	starter := r.GenerateValidators()
	decoders := mustGen(t, r.GenerateDecoders)

	// Every contract helper referenced by the generated decoders must be
	// exported by the starter.
	for _, name := range validatorsContract {
		usedByDecoders := strings.Contains(decoders, name)
		exportedByStarter := strings.Contains(starter, "export function "+name)
		if usedByDecoders && !exportedByStarter {
			t.Errorf("generated decoders use helper %q but the starter does not export it", name)
		}
	}

	// And the starter is a strict superset: it exports all 11 regardless of
	// which subset the current decoders happen to reference.
	for _, name := range validatorsContract {
		if !strings.Contains(starter, "export function "+name) {
			t.Errorf("starter must export all 11 helpers; missing %q", name)
		}
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
