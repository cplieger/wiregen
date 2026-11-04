package wiregen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/basic"
)

// validatorsContract is the exact set of names the validators module MUST
// export: the helper functions plus the Decoder<T> type alias. The
// generated decoders import a referenced subset of these by name, so the
// starter must export the full set for any generated import to resolve.
var validatorsContract = []string{
	"own", "asObject", "asArray",
	"reqStr", "reqNum", "reqBool",
	"optStr", "optNum", "optBool",
	"reqOneOf",
	"decodeArray", "decodeRecord",
}

func validatorsRegistry() *testRegistry {
	r := newTestRegistry(
		wiregen.WithValidatorsImport("./test-validators.js"),
	)
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[basic.Address](),
		wiregen.TypeRef[basic.User](),
		wiregen.TypeRef[basic.Notification](),
	}
	r.Enums = []wiregen.EnumRef{wiregen.StringEnum[basic.Status]()}
	r.Events = []wiregen.Event{
		r.event("notification", "Notification"),
	}
	return r
}

// (a) The module exports all 11 contract functions plus the Decoder<T> type.
func TestGenerateValidators_ExportsFullContract(t *testing.T) {
	out := mustGen(t, newTestRegistry().validatorsFile)

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
	out := mustGen(t, newTestRegistry().validatorsFile)
	if !strings.Contains(out, "DO NOT EDIT") {
		t.Errorf("validators module must carry the DO-NOT-EDIT banner; got header:\n%s", firstLines(out, 4))
	}

	custom := "// custom banner\n\n"
	r := newTestRegistry(wiregen.WithHeaderComment(custom))
	if got := mustGen(t, r.validatorsFile); !strings.HasPrefix(got, custom) {
		t.Errorf("validators module must use the consumer's HeaderComment; got header:\n%s", firstLines(got, 3))
	}
}

// (c) Generate writes the validators module on every run, at
// validators.gen.ts by default and at WithValidatorsFile's path inside the TS
// directory otherwise; each module imports another by its relative path.
func TestGenerate_ValidatorsFile(t *testing.T) {
	dir := t.TempDir()
	r := validatorsRegistry()
	if err := r.generate(t.Context(), dir); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "validators.gen.ts")); err != nil {
		t.Errorf("Generate did not write the default validators.gen.ts: %v", err)
	}

	root := t.TempDir()
	r2 := newTestRegistry(wiregen.WithFilenames(wiregen.Filenames{Types: "wire/types.gen.ts", Decoders: "wire/decoders.gen.ts"}))
	r2.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	if err := r2.generate(t.Context(), root); err != nil {
		t.Fatalf("Generate with modules in a subdirectory: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "validators.gen.ts"))
	if err != nil {
		t.Fatalf("validators module not written: %v", err)
	}
	if want := mustGen(t, r2.validatorsFile); string(got) != want {
		t.Errorf("written validators module differs from the rendered one\n--- want (rendered) ---\n%s\n+++ got (written file) +++\n%s", want, got)
	}
	decoders, err := os.ReadFile(filepath.Join(root, "wire", "decoders.gen.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`from "../validators.gen.js";`, `from "./types.gen.js";`} {
		if !strings.Contains(string(decoders), want) {
			t.Errorf("decoders lack import %s; got:\n%s", want, firstLines(string(decoders), 4))
		}
	}
}

// Every TS output stays inside the TS directory: an absolute path or one
// leaving it is refused before anything is written.
func TestGenerate_TSOutputsStayInTSDir(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "escaped.gen.ts")
	for _, c := range []struct {
		name string
		opt  wiregen.Option
	}{
		{"parent", wiregen.WithValidatorsFile("../validators.gen.ts")},
		{"parent after a child", wiregen.WithFilenames(wiregen.Filenames{Types: "a/../../types.gen.ts"})},
		{"absolute", wiregen.WithArbitrariesFile(outside)},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := validatorsRegistry()
			r.opts = append(r.opts, c.opt)
			err := generateErr(t, r)
			if err == nil || !strings.Contains(err.Error(), "must be a relative path inside the TS directory") {
				t.Errorf("Generate = %v, want a refusal of the path", err)
			}
			if _, statErr := os.Stat(outside); statErr == nil {
				t.Errorf("Generate wrote %s outside the TS directory", outside)
			}
		})
	}
}

// A symlink inside the TS directory cannot redirect an output out of it.
func TestGenerate_SymlinkCannotEscapeTSDir(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "wire")); err != nil {
		t.Fatal(err)
	}
	r := validatorsRegistry()
	r.opts = append(r.opts, wiregen.WithFilenames(wiregen.Filenames{Types: "wire/types.gen.ts"}))
	if err := r.generate(t.Context(), dir); err == nil {
		t.Error("Generate wrote through a symlink leaving the TS directory")
	}
	if entries, _ := os.ReadDir(outside); len(entries) > 0 {
		t.Errorf("Generate wrote %d entries outside the TS directory", len(entries))
	}
}

// (d) The emitted helper names are a SUPERSET of the names a generated
// decoders.gen.ts imports, so any generated decoder import resolves against
// the starter.
func TestGenerateValidators_SupersetOfDecoderImports(t *testing.T) {
	r := validatorsRegistry()
	starter := mustGen(t, r.validatorsFile)
	decoders := mustGen(t, r.decodersFile)

	// Every contract helper referenced by the generated decoders must be
	// exported by the starter.
	for _, name := range validatorsContract {
		usedByDecoders := strings.Contains(decoders, name)
		exportedByStarter := strings.Contains(starter, "export function "+name)
		if usedByDecoders && !exportedByStarter {
			t.Errorf("generated decoders use helper %q but the starter does not export it", name)
		}
	}

	// And the starter is a strict superset: it exports all of them regardless of
	// which subset the current decoders happen to reference.
	for _, name := range validatorsContract {
		if !strings.Contains(starter, "export function "+name) {
			t.Errorf("starter must export every helper; missing %q", name)
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
