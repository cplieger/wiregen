package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/unions"
)

// Tests for //wiregen:union handling: the emitted `export type` alias and the
// runtime discriminator decoder, including the partial/empty/nil
// DiscriminatorMap behaviors and the interface-only registration case.

const unionsPkg = "github.com/cplieger/wiregen/testdata/unions"

// unionReg registers the three event variants plus the EventData union
// interface. Callers set DiscriminatorMap to choose the decoder behavior.
func unionReg() *wiregen.Registry {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{unionsPkg}
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[unions.CoverageEvent](),
		wiregen.TypeRef[unions.NotifyEvent](),
		wiregen.TypeRef[unions.ScanEvent](),
		{PkgPath: unionsPkg, Name: "EventData"},
	}
	return r
}

func TestUnion_TypeAlias(t *testing.T) {
	out := unionReg().GenerateTypes()
	if !strings.Contains(out, "export type EventData = CoverageEvent | NotifyEvent | ScanEvent;") {
		t.Errorf("union type alias missing, got:\n%s", out)
	}
	if !strings.Contains(out, "export interface CoverageEvent {") {
		t.Errorf("variant interface missing, got:\n%s", out)
	}
}

func TestUnion_DecoderAllVariants(t *testing.T) {
	r := unionReg()
	r.DiscriminatorMap = map[string]map[string]string{
		"EventData": {
			"coverage":   "CoverageEvent",
			"notify":     "NotifyEvent",
			"scan:start": "ScanEvent",
			"scan:done":  "ScanEvent",
		},
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "export const decodeEventData: (type: string, v: unknown) => EventData") {
		t.Errorf("missing union decoder signature (discriminator name from directive), got:\n%s", dec)
	}
	for _, want := range []string{
		`case "coverage": return decodeCoverageEvent(v);`,
		`case "notify": return decodeNotifyEvent(v);`,
		`case "scan:start": return decodeScanEvent(v);`,
		`case "scan:done": return decodeScanEvent(v);`,
	} {
		if !strings.Contains(dec, want) {
			t.Errorf("missing variant case %q, got:\n%s", want, dec)
		}
	}
	if !strings.Contains(dec, "default: throw new TypeError(`unknown EventData variant:") {
		t.Errorf("missing unknown-variant default, got:\n%s", dec)
	}
}

func TestUnion_PartialDiscriminatorMap(t *testing.T) {
	r := unionReg()
	r.DiscriminatorMap = map[string]map[string]string{
		"EventData": {"coverage": "CoverageEvent"},
	}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, `case "coverage"`) {
		t.Errorf("partial discriminator should have coverage case, got:\n%s", dec)
	}
	if strings.Contains(dec, `case "notify"`) {
		t.Errorf("unmapped variant should NOT appear, got:\n%s", dec)
	}
	if !strings.Contains(dec, "default: throw") {
		t.Errorf("should have unknown-variant default, got:\n%s", dec)
	}
}

func TestUnion_EmptyDiscriminatorMap(t *testing.T) {
	r := unionReg()
	// A present-but-empty discriminator map still emits the decoder, but with
	// no variant cases — only the unknown-variant default.
	r.DiscriminatorMap = map[string]map[string]string{"EventData": {}}
	dec := r.GenerateDecoders()
	if !strings.Contains(dec, "export const decodeEventData: (type: string, v: unknown) => EventData") {
		t.Errorf("empty discriminator map should still emit the decoder, got:\n%s", dec)
	}
	if !strings.Contains(dec, "default: throw new TypeError(`unknown EventData variant:") {
		t.Errorf("empty discriminator map decoder should still throw on unknown, got:\n%s", dec)
	}
	if strings.Contains(dec, "case \"") {
		t.Errorf("empty discriminator map should emit no variant cases, got:\n%s", dec)
	}
}

func TestUnion_NoDiscriminatorMap_NoDecoder(t *testing.T) {
	// All variants registered but no DiscriminatorMap → only the type alias is
	// emitted, no runtime union decoder.
	dec := unionReg().GenerateDecoders()
	if strings.Contains(dec, "decodeEventData") {
		t.Errorf("nil DiscriminatorMap should NOT produce a union decoder, got:\n%s", dec)
	}
}

func TestUnion_InterfaceOnlyEmitsTypeAlias(t *testing.T) {
	// Registering only the union interface (no variants) must not panic and
	// still emits the type alias from the directive.
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./v.js"),
		wiregen.WithBusImport("./b.js"),
	)
	r.PackagePaths = []string{unionsPkg}
	r.Types = []wiregen.WireType{{PkgPath: unionsPkg, Name: "EventData"}}
	out := r.GenerateTypes()
	if !strings.Contains(out, "export type EventData") {
		t.Errorf("interface with union directive should produce a type alias, got:\n%s", out)
	}
}
