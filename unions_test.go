package wiregen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v3"
	"github.com/cplieger/wiregen/v3/testdata/unions"
)

// Tests for //wiregen:union handling: the emitted `export type` alias and the
// runtime discriminator decoder, including the partial/empty/nil
// DiscriminatorMap behaviors and the interface-only registration case.

const unionsPkg = "github.com/cplieger/wiregen/v3/testdata/unions"

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
	out := mustGen(t, unionReg().GenerateTypes)
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
	dec := mustGen(t, r.GenerateDecoders)
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
	dec := mustGen(t, r.GenerateDecoders)
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
	dec := mustGen(t, r.GenerateDecoders)
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
	dec := mustGen(t, unionReg().GenerateDecoders)
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
	out := mustGen(t, r.GenerateTypes)
	if !strings.Contains(out, "export type EventData") {
		t.Errorf("interface with union directive should produce a type alias, got:\n%s", out)
	}
}

// --- 1-arg SSE payload adapter (P1) ---

// TestUnion_PayloadAdapterEmitted pins the 1-argument companion decoder: a
// plain Decoder<EventData> that reads the configured discriminator key off
// the payload object and dispatches through the 2-arg union decoder.
func TestUnion_PayloadAdapterEmitted(t *testing.T) {
	r := unionReg()
	r.DiscriminatorMap = map[string]map[string]string{
		"EventData": {"coverage": "CoverageEvent"},
	}
	dec := mustGen(t, r.GenerateDecoders)
	wants := []string{
		"export const decodeEventDataPayload: Decoder<EventData> = (v) => {",
		`const o = asObject(v, "$.event_data");`,
		`return decodeEventData(reqStr(o, "type", "$.event_data"), o);`,
	}
	for _, want := range wants {
		if !strings.Contains(dec, want) {
			t.Errorf("payload adapter missing %q, got:\n%s", want, dec)
		}
	}
}

// TestUnion_NoDiscriminatorMap_NoAdapter: without a DiscriminatorMap neither
// the 2-arg decoder nor the payload adapter is emitted.
func TestUnion_NoDiscriminatorMap_NoAdapter(t *testing.T) {
	dec := mustGen(t, unionReg().GenerateDecoders)
	if strings.Contains(dec, "decodeEventDataPayload") {
		t.Errorf("nil DiscriminatorMap should not produce a payload adapter, got:\n%s", dec)
	}
}

// TestUnion_SSERegistryBindsAdapter pins the registry binding: an SSE event
// whose type carries a DiscriminatorMap binds the 1-arg payload adapter (the
// 2-arg decoder cannot satisfy the registry's Decoder<T> shape), in both bus
// and self-contained registry modes; plain struct events keep their decoder.
func TestUnion_SSERegistryBindsAdapter(t *testing.T) {
	r := unionReg()
	r.DiscriminatorMap = map[string]map[string]string{
		"EventData": {"coverage": "CoverageEvent"},
	}
	r.SSEEvents = []wiregen.SSERegEntry{
		{EventType: "event", TypeName: "EventData"},
		{EventType: "scan", TypeName: "ScanEvent"},
	}
	reg := mustGenNoLoad(t, r.GenerateRegistry)
	for _, want := range []string{
		`registerSSEDecoder("event", decodeEventDataPayload);`,
		`registerSSEDecoder("scan", decodeScanEvent);`,
		"import { decodeEventDataPayload, decodeScanEvent } from \"./decoders.gen.js\";",
	} {
		if !strings.Contains(reg, want) {
			t.Errorf("bus registry missing %q, got:\n%s", want, reg)
		}
	}

	r.SelfContainedRegistry = true
	reg = mustGenNoLoad(t, r.GenerateRegistry)
	if want := `registry.set("event", decodeEventDataPayload as Decoder<unknown>);`; !strings.Contains(reg, want) {
		t.Errorf("self-contained registry missing %q, got:\n%s", want, reg)
	}
}

// TestUnion_SSEWithoutDiscriminatorMapErrors pins the Generate-time guard: a
// //wiregen:union type registered in SSEEvents without a DiscriminatorMap has
// no runtime decoder to bind, so Generate must reject the configuration
// instead of emitting a registry that references a nonexistent decoder.
func TestUnion_SSEWithoutDiscriminatorMapErrors(t *testing.T) {
	r := unionReg()
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "event", TypeName: "EventData"}}
	err := r.Generate(t.Context(), t.TempDir())
	if err == nil {
		t.Fatal("Generate should reject a union SSE event without a DiscriminatorMap")
	}
	if !strings.Contains(err.Error(), "DiscriminatorMap") {
		t.Errorf("error should name the missing DiscriminatorMap, got: %v", err)
	}
}

// TestUnion_GenerateEndToEnd: the P1 completion shape — a union registered in
// SSEEvents with a DiscriminatorMap generates all files, and regeneration is
// byte-identical for the existing consumers' no-union configuration (pinned
// by the golden tests, which carry no unions).
func TestUnion_GenerateEndToEnd(t *testing.T) {
	r := unionReg()
	r.DiscriminatorMap = map[string]map[string]string{
		"EventData": {
			"coverage": "CoverageEvent",
			"notify":   "NotifyEvent",
		},
	}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "event", TypeName: "EventData"}}
	dir := t.TempDir()
	if err := r.Generate(t.Context(), dir); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	reg, err := os.ReadFile(filepath.Join(dir, "registry.gen.ts"))
	if err != nil {
		t.Fatalf("registry.gen.ts not written: %v", err)
	}
	if !strings.Contains(string(reg), "decodeEventDataPayload") {
		t.Errorf("generated registry does not bind the payload adapter:\n%s", reg)
	}
}
