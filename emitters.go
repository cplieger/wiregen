package wiregen

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	tsUnknown      = "unknown"
	tsIdentityCast = "(v) => v as unknown"
	tsBoolean      = "boolean"
	tsString       = "string"
	tsNumber       = "number"
)

// --- types generation ---

func (r *Registry) generateTypes(w *strings.Builder, engine *astEngine) {
	w.WriteString(r.HeaderComment)
	r.emitEnumTypes(w)
	r.emitUnionTypes(w, engine)
	r.emitStructInterfaces(w, engine)
}

// emitEnumTypes writes the `export type X = "a" | "b";` string-union aliases,
// deduplicated and sorted by TS name. Dedup iterates Go names in sorted order
// so a TS-name collision has a deterministic winner (the first Go name in
// sort order), matching emitEnumConsts' dedup order.
func (r *Registry) emitEnumTypes(w *strings.Builder) {
	enumNames := make([]string, 0, len(r.Enums))
	seenEnumTS := map[string]bool{}
	for _, name := range enumNamesSlice(r.Enums) {
		tn := r.tsEnumName(name)
		if seenEnumTS[tn] {
			continue
		}
		seenEnumTS[tn] = true
		enumNames = append(enumNames, name)
	}
	slices.SortStableFunc(enumNames, func(a, b string) int {
		return cmp.Compare(r.tsEnumName(a), r.tsEnumName(b))
	})
	for _, name := range enumNames {
		def := r.Enums[name]
		w.WriteString("export type " + r.tsEnumName(name) + " = ")
		if len(def.Values) == 0 {
			// A registered enum that resolved to zero values (no explicit
			// Values and no string const block discovered in a loaded
			// package) must never emit "= ;" (invalid TS — see the
			// checkNotContains guard in
			// TestNewASTEngine_discoversEnumsWithoutRegisteredTypes). Emit the
			// bottom type so output stays syntactically valid; the empty
			// reqOneOf(...) membership check then fails clearly at decode time.
			w.WriteString("never;\n\n")
			continue
		}
		for i, v := range def.Values {
			if i > 0 {
				w.WriteString(" | ")
			}
			w.WriteString("\"" + tsStringLiteral(v) + "\"")
		}
		w.WriteString(";\n\n")
	}
}

// emitUnionTypes writes the `export type X = A | B | C;` aliases for the
// //wiregen:union types.
func (r *Registry) emitUnionTypes(w *strings.Builder, engine *astEngine) {
	for _, ti := range engine.types {
		if ti.Union == nil {
			continue
		}
		if ti.Doc != "" {
			w.WriteString(ti.Doc)
		}
		w.WriteString("export type " + r.tsName(ti.Name) + " = ")
		for i, v := range ti.Union.Variants {
			if i > 0 {
				w.WriteString(" | ")
			}
			w.WriteString(r.tsName(v))
		}
		w.WriteString(";\n\n")
	}
}

// emitStructInterfaces writes the `export interface X { … }` declarations for
// the non-union types.
func (r *Registry) emitStructInterfaces(w *strings.Builder, engine *astEngine) {
	for _, ti := range engine.types {
		if ti.Union != nil {
			continue
		}
		if ti.Doc != "" {
			w.WriteString(ti.Doc)
		}
		w.WriteString("export interface " + r.tsName(ti.Name) + " {\n")
		for i := range ti.Fields {
			emitInterfaceField(w, &ti.Fields[i])
		}
		w.WriteString("}\n\n")
	}
}

// emitInterfaceField writes one `name: type;` (or `name?: type;`) interface
// member, prefixed by its JSDoc when present.
func emitInterfaceField(w *strings.Builder, f *fieldInfo) {
	if f.Doc != "" {
		w.WriteString("  " + f.Doc)
	}
	ts := f.TSType
	if f.JSONString {
		ts = tsString
	}
	if f.Optional {
		w.WriteString("  " + tsPropName(f.WireName) + "?: " + ts + ";\n")
	} else {
		w.WriteString("  " + tsPropName(f.WireName) + ": " + ts + ";\n")
	}
}

// --- decoders generation ---

// usedIdents accumulates the identifiers decoder emission actually uses, so
// the import/const header lines are derived from what was emitted rather than
// re-discovered by scanning the emitted text (a heuristic that could match an
// identifier inside an emitted string literal). The one place text scanning
// survives is `opaque`: Type/DecoderMappings values are consumer-supplied TS
// expressions emitted verbatim, so any contract helper, type name, or enum
// const array they reference is found by scanning those (small) expressions —
// never the generated body.
type usedIdents struct {
	helpers  map[string]bool // validators-contract helpers called
	types    map[string]bool // TS type / enum-type names referenced
	enums    map[string]bool // Go enum names whose const value array is referenced
	decoders map[string]bool // generated decoder functions referenced (client emission)
	opaque   []string        // consumer-supplied mapping expressions emitted verbatim
}

func newUsedIdents() *usedIdents {
	return &usedIdents{
		helpers:  map[string]bool{},
		types:    map[string]bool{},
		enums:    map[string]bool{},
		decoders: map[string]bool{},
	}
}

func (u *usedIdents) helper(name string)     { u.helpers[name] = true }
func (u *usedIdents) typeRef(tsName string)  { u.types[tsName] = true }
func (u *usedIdents) enumUse(goName string)  { u.enums[goName] = true }
func (u *usedIdents) decoderRef(name string) { u.decoders[name] = true }
func (u *usedIdents) opaqueExpr(expr string) {
	u.opaque = append(u.opaque, expr)
}

// opaqueRefs reports whether any consumer-supplied mapping expression
// references ident.
func (u *usedIdents) opaqueRefs(ident string) bool {
	for _, expr := range u.opaque {
		if isIdentReferenced(expr, ident) {
			return true
		}
	}
	return false
}

// generateDecoders writes the decoders file. Callers (Generate,
// GenerateDecoders) have already rejected a missing ValidatorsImport.
func (r *Registry) generateDecoders(w *strings.Builder, engine *astEngine) {
	body, used := r.decoderBodies(engine)

	w.WriteString(r.HeaderComment)
	r.emitHelperImports(w, used)
	r.emitTypeImports(w, used, engine)
	r.emitEnumConsts(w, used)
	w.WriteString(body)
}

// decoderBodies emits the struct decoders followed by the union decoders and
// returns the concatenated body plus the identifiers it used (which decide
// the import/const header lines).
func (r *Registry) decoderBodies(engine *astEngine) (string, *usedIdents) {
	var bodies strings.Builder
	used := newUsedIdents()
	for _, ti := range engine.types {
		if ti.Union == nil {
			r.emitDecoder(&bodies, ti, used)
		}
	}
	for _, ti := range engine.types {
		if ti.Union != nil {
			r.emitUnionDecoder(&bodies, ti, used)
		}
	}
	return bodies.String(), used
}

// emitHelperImports writes the validators-module import, listing (in the
// contract's canonical order) only the helpers the emitted decoders call.
func (r *Registry) emitHelperImports(w *strings.Builder, used *usedIdents) {
	allHelpers := []string{
		"asObject", "asArray", "reqStr", "reqNum", "reqBool",
		"optStr", "optNum", "optBool", "reqOneOf",
		"decodeArray", "decodeRecord",
	}
	var usedHelpers []string
	for _, h := range allHelpers {
		if used.helpers[h] || used.opaqueRefs(h) {
			usedHelpers = append(usedHelpers, h)
		}
	}
	w.WriteString("import { ")
	if len(usedHelpers) > 0 {
		w.WriteString(strings.Join(usedHelpers, ", "))
		w.WriteString(", ")
	}
	w.WriteString("type Decoder } from \"" + tsStringLiteral(r.ValidatorsImport) + "\";\n")
}

// emitTypeImports writes the `import type { … }` line for the type/enum names
// the emitted decoders reference, sorted; it emits nothing when none are used.
func (r *Registry) emitTypeImports(w *strings.Builder, used *usedIdents, engine *astEngine) {
	candidateNames := make([]string, 0)
	for _, ti := range engine.types {
		candidateNames = append(candidateNames, r.tsName(ti.Name))
	}
	enumSeen := map[string]bool{}
	for name := range r.Enums {
		tn := r.tsEnumName(name)
		if !enumSeen[tn] {
			enumSeen[tn] = true
			candidateNames = append(candidateNames, tn)
		}
	}
	usedSet := map[string]bool{}
	for _, n := range candidateNames {
		if used.types[n] || used.opaqueRefs(n) {
			usedSet[n] = true
		}
	}
	sorted := slices.Sorted(maps.Keys(usedSet))
	if len(sorted) > 0 {
		w.WriteString("import type { ")
		w.WriteString(strings.Join(sorted, ", "))
		w.WriteString(" } from \"" + tsStringLiteral(r.TypesImportPath) + "\";\n")
	}
	w.WriteString("\n")
}

// emitEnumConsts writes the `const XS = [...] as const;` value arrays for the
// enums the emitted decoders reference (deduped), then a trailing blank line.
func (r *Registry) emitEnumConsts(w *strings.Builder, used *usedIdents) {
	emitted := map[string]bool{}
	for _, name := range enumNamesSlice(r.Enums) {
		constN := r.enumConstName(name)
		if emitted[constN] || (!used.enums[name] && !used.opaqueRefs(constN)) {
			continue
		}
		emitted[constN] = true
		def := r.Enums[name]
		w.WriteString("const " + constN + " = [")
		for i, v := range def.Values {
			if i > 0 {
				w.WriteString(", ")
			}
			w.WriteString("\"" + tsStringLiteral(v) + "\"")
		}
		w.WriteString("] as const;\n")
	}
	if len(emitted) > 0 {
		w.WriteString("\n")
	}
}

func (r *Registry) emitDecoder(w *strings.Builder, ti *typeInfo, used *usedIdents) {
	tn := r.tsName(ti.Name)
	path := "$." + r.pathName(tn)
	used.helper("asObject")
	used.typeRef(tn)
	w.WriteString("export const " + r.decoderName(ti.Name) + ": Decoder<" + tn + "> = (v) => {\n")
	w.WriteString("  const o = asObject(v, \"" + path + "\");\n")

	var reqFields, optFields []fieldInfo
	for _, f := range ti.Fields {
		if f.Optional {
			optFields = append(optFields, f)
		} else {
			reqFields = append(reqFields, f)
		}
	}

	if len(reqFields) > 0 || len(optFields) > 0 {
		w.WriteString("  const out: " + tn + " = {\n")
		for _, f := range reqFields {
			w.WriteString("    " + tsPropName(f.WireName) + ": " + r.reqExpr(&f, path, used) + ",\n")
		}
		w.WriteString("  };\n")
	} else {
		w.WriteString("  const out: " + tn + " = {};\n")
	}

	for _, f := range optFields {
		r.emitOptionalField(w, &f, path, used)
	}

	w.WriteString("  return out;\n")
	w.WriteString("};\n\n")
}

func (r *Registry) emitUnionDecoder(w *strings.Builder, ti *typeInfo, used *usedIdents) {
	tn := r.tsName(ti.Name)
	dm := r.DiscriminatorMap[ti.Name]
	if dm == nil {
		return // No discriminator map → only type alias emitted
	}
	used.typeRef(tn)

	disc := sanitizeVarName(ti.Union.Discriminator)
	if disc == "" {
		disc = "disc"
	}
	w.WriteString("export const " + r.decoderName(ti.Name) + ": (" + disc + ": string, v: unknown) => " + tn + " = (" + disc + ", v) => {\n")
	w.WriteString("  switch (" + disc + ") {\n")

	for _, k := range slices.Sorted(maps.Keys(dm)) {
		variant := dm[k]
		w.WriteString("    case \"" + tsStringLiteral(k) + "\": return " + r.decoderName(variant) + "(v);\n")
	}
	w.WriteString("    default: throw new TypeError(`unknown " + tn + " variant: ${" + disc + "}`);\n")
	w.WriteString("  }\n")
	w.WriteString("};\n\n")

	r.emitUnionPayloadAdapter(w, ti, tn, used)
}

// emitUnionPayloadAdapter writes the 1-argument companion of a union's
// 2-argument decoder: a plain Decoder<X> that reads the configured
// discriminator key off the payload object itself and dispatches. This is the
// form the SSE registry (and any other Decoder<T>-shaped consumer) can bind —
// the 2-argument decoder cannot enter the registry, which was the gap that
// blocked registering a //wiregen:union type in SSEEvents.
func (r *Registry) emitUnionPayloadAdapter(w *strings.Builder, ti *typeInfo, tn string, used *usedIdents) {
	path := "$." + r.pathName(tn)
	key := tsStringLiteral(ti.Union.Discriminator)
	used.helper("asObject")
	used.helper("reqStr")
	w.WriteString("export const " + r.unionPayloadDecoderName(ti.Name) + ": Decoder<" + tn + "> = (v) => {\n")
	w.WriteString("  const o = asObject(v, \"" + path + "\");\n")
	w.WriteString("  return " + r.decoderName(ti.Name) + "(reqStr(o, \"" + key + "\", \"" + path + "\"), o);\n")
	w.WriteString("};\n\n")
}

func (r *Registry) reqExpr(f *fieldInfo, path string, used *usedIdents) string {
	wn := tsStringLiteral(f.WireName)
	if f.JSONString {
		used.helper("reqStr")
		return "reqStr(o, \"" + wn + "\", \"" + path + "\")"
	}
	if f.IsRaw || f.IsIface {
		return "o[\"" + wn + "\"] as unknown"
	}
	// []byte marshals as a base64 string, but a nil non-omitempty []byte
	// marshals as null — accept null as the empty string (the same
	// null-as-zero-value contract as collections).
	if f.IsBytes {
		used.helper("reqStr")
		return "o[\"" + wn + "\"] === null ? \"\" : reqStr(o, \"" + wn + "\", \"" + path + "\")"
	}

	// Collections are routed before the mapping checks: a slice/map field's
	// GoTypeName holds its ELEMENT's mapping key (see resolveSliceType), so
	// Type/DecoderMappings apply per element inside elemDecoderExpr, never to
	// the collection as a whole. encoding/json marshals a nil non-omitempty
	// slice/map to null, so a required collection accepts null as empty —
	// nullable-vs-optional is a documented non-goal, and rejecting
	// encoding/json's own nil-value output would be a fidelity bug.
	if f.IsSlice {
		used.helper("decodeArray")
		return "o[\"" + wn + "\"] === null ? [] : decodeArray(o[\"" + wn + "\"], " + r.elemDecoderExpr(f.Elem, wn, path, used) + ", \"" + path + "." + wn + "\")"
	}
	if f.IsMap {
		used.helper("decodeRecord")
		return "o[\"" + wn + "\"] === null ? {} : decodeRecord(o[\"" + wn + "\"], " + r.elemDecoderExpr(f.Elem, wn, path, used) + ", \"" + path + "." + wn + "\")"
	}

	// Custom decoder mapping (scalar field of a mapped type)
	if expr, ok := r.DecoderMappings[f.GoTypeName]; ok {
		used.opaqueExpr(expr)
		return expr + "(o, \"" + wn + "\", \"" + path + "\")"
	}
	// Custom type mapping without decoder
	if _, ok := r.TypeMappings[f.GoTypeName]; ok {
		used.opaqueExpr(f.TSType)
		return "o[\"" + wn + "\"] as " + f.TSType
	}

	if f.IsEnum {
		used.helper("reqOneOf")
		used.enumUse(f.GoTypeName)
		return "reqOneOf(o, \"" + wn + "\", " + r.enumConstName(f.GoTypeName) + ", \"" + path + "\")"
	}
	if f.IsStruct {
		return r.decoderName(f.GoTypeName) + "(o[\"" + wn + "\"])"
	}

	// Unresolved type (e.g. an unregistered nested struct) — pass through as
	// unknown rather than mis-decoding it as a number.
	if f.TSType == tsUnknown {
		return "o[\"" + wn + "\"] as unknown"
	}

	helper := primHelperAST(f.TSType, false)
	used.helper(helper)
	return helper + "(o, \"" + wn + "\", \"" + path + "\")"
}

func (r *Registry) emitOptionalField(w *strings.Builder, f *fieldInfo, path string, used *usedIdents) {
	wn := tsStringLiteral(f.WireName)
	// A present-null field decodes as absent in every branch below except the
	// raw/interface/unknown pass-throughs, where null is data. encoding/json
	// marshals a nil pointer/slice/map to null, and the library's
	// optional-only model (nullable-vs-optional is a documented non-goal)
	// maps null and a missing key to the same TS undefined.
	guard := "o[\"" + wn + "\"] !== undefined && o[\"" + wn + "\"] !== null"
	if f.JSONString {
		used.helper("optStr")
		varName := localVarName(f.WireName)
		w.WriteString("  const " + varName + " = o[\"" + wn + "\"] === null ? undefined : optStr(o, \"" + wn + "\", \"" + path + "\");\n")
		w.WriteString("  if (" + varName + " !== undefined) out" + tsMemberRef(f.WireName) + " = " + varName + ";\n")
		return
	}
	if f.IsRaw || f.IsIface {
		w.WriteString("  if (o[\"" + wn + "\"] !== undefined) out" + tsMemberRef(f.WireName) + " = o[\"" + wn + "\"] as unknown;\n")
		return
	}
	// Collections before the mapping checks — see reqExpr.
	if f.IsSlice {
		used.helper("decodeArray")
		w.WriteString("  if (" + guard + ") out" + tsMemberRef(f.WireName) + " = decodeArray(o[\"" + wn + "\"], " + r.elemDecoderExpr(f.Elem, wn, path, used) + ", \"" + path + "." + wn + "\");\n")
		return
	}
	if f.IsMap {
		used.helper("decodeRecord")
		w.WriteString("  if (" + guard + ") out" + tsMemberRef(f.WireName) + " = decodeRecord(o[\"" + wn + "\"], " + r.elemDecoderExpr(f.Elem, wn, path, used) + ", \"" + path + "." + wn + "\");\n")
		return
	}
	if expr, ok := r.DecoderMappings[f.GoTypeName]; ok {
		used.opaqueExpr(expr)
		varName := localVarName(f.WireName)
		w.WriteString("  const " + varName + " = o[\"" + wn + "\"] === null ? undefined : " + expr + "(o, \"" + wn + "\", \"" + path + "\");\n")
		w.WriteString("  if (" + varName + " !== undefined) out" + tsMemberRef(f.WireName) + " = " + varName + ";\n")
		return
	}
	if _, ok := r.TypeMappings[f.GoTypeName]; ok {
		used.opaqueExpr(f.TSType)
		w.WriteString("  if (" + guard + ") out" + tsMemberRef(f.WireName) + " = o[\"" + wn + "\"] as " + f.TSType + ";\n")
		return
	}
	if f.IsEnum {
		used.helper("reqOneOf")
		used.enumUse(f.GoTypeName)
		w.WriteString("  if (" + guard + ") out" + tsMemberRef(f.WireName) + " = reqOneOf(o, \"" + wn + "\", " + r.enumConstName(f.GoTypeName) + ", \"" + path + "\");\n")
		return
	}
	if f.IsStruct {
		w.WriteString("  if (" + guard + ") out" + tsMemberRef(f.WireName) + " = " + r.decoderName(f.GoTypeName) + "(o[\"" + wn + "\"]);\n")
		return
	}

	// Unresolved type — pass through as unknown rather than optNum.
	if f.TSType == tsUnknown {
		w.WriteString("  if (o[\"" + wn + "\"] !== undefined) out" + tsMemberRef(f.WireName) + " = o[\"" + wn + "\"] as unknown;\n")
		return
	}

	helper := primHelperAST(f.TSType, true)
	used.helper(helper)
	varName := localVarName(f.WireName)
	w.WriteString("  const " + varName + " = o[\"" + wn + "\"] === null ? undefined : " + helper + "(o, \"" + wn + "\", \"" + path + "\");\n")
	w.WriteString("  if (" + varName + " !== undefined) out" + tsMemberRef(f.WireName) + " = " + varName + ";\n")
}

// elemDecoderExpr returns the per-element decoder expression for the slice
// element or map value described by elem. wn and path are the owning field's
// wire name and type-level path: a DecoderMappings element decoder keeps its
// (obj, key, path) contract by being called through a synthesized single-key
// object carrying the real wire name, so its error messages locate the actual
// field (decodeArray/decodeRecord prefix the element index/key themselves).
//
// A collection element recurses BEFORE the mapping checks (the same routing
// rule as reqExpr — GoTypeName carries the LEAF element's mapping key, so a
// mapped leaf inside a nested collection applies at its own level, never to
// the collection value). Each nested level accepts null as its empty value
// (encoding/json marshals a nil slice/map to null) and validates its own
// elements, so [][]T and map[string][]T decode with real per-level checks
// instead of an identity cast.
func (r *Registry) elemDecoderExpr(elem *fieldInfo, wn, path string, used *usedIdents) string {
	if elem.IsSlice {
		used.helper("decodeArray")
		return "(v) => v === null ? [] : decodeArray(v, " + r.elemDecoderExpr(elem.Elem, wn, path, used) + ", \"" + path + "." + wn + "\")"
	}
	if elem.IsMap {
		used.helper("decodeRecord")
		return "(v) => v === null ? {} : decodeRecord(v, " + r.elemDecoderExpr(elem.Elem, wn, path, used) + ", \"" + path + "." + wn + "\")"
	}

	if expr, ok := r.DecoderMappings[elem.GoTypeName]; ok {
		used.opaqueExpr(expr)
		return "(v) => " + expr + "({\"" + wn + "\": v} as Record<string, unknown>, \"" + wn + "\", \"" + path + "\")"
	}
	if mapped, ok := r.TypeMappings[elem.GoTypeName]; ok {
		used.opaqueExpr(mapped)
		return "(v) => v as " + mapped
	}
	if r.typeNames[elem.GoTypeName] {
		return r.decoderName(elem.GoTypeName)
	}
	if _, ok := r.Enums[elem.GoTypeName]; ok {
		constName := r.enumConstName(elem.GoTypeName)
		used.enumUse(elem.GoTypeName)
		used.typeRef(r.tsEnumName(elem.GoTypeName))
		return "(v) => { const s = v as string; if (!" + constName + ".includes(s as never)) throw new TypeError(\"invalid enum value: \" + s); return s as " + r.tsEnumName(elem.GoTypeName) + "; }"
	}
	// []byte element: a base64 string on the wire, and a nil element
	// marshals to null — accept null as the empty string (null-as-zero).
	if elem.IsBytes {
		return "(v) => { if (v === null) return \"\"; if (typeof v !== \"string\") throw new TypeError(\"expected string\"); return v; }"
	}

	switch elem.TSType {
	case tsString:
		return "(v) => { if (typeof v !== \"string\") throw new TypeError(\"expected string\"); return v as string; }"
	case tsNumber:
		return "(v) => { if (typeof v !== \"number\") throw new TypeError(\"expected number\"); return v as number; }"
	case tsBoolean:
		return "(v) => { if (typeof v !== \"boolean\") throw new TypeError(\"expected boolean\"); return v as boolean; }"
	}

	return tsIdentityCast
}

func primHelperAST(tsType string, optional bool) string {
	prefix := "req"
	if optional {
		prefix = "opt"
	}
	switch tsType {
	case tsString:
		return prefix + "Str"
	case tsBoolean:
		return prefix + "Bool"
	default:
		return prefix + "Num"
	}
}

// --- arbitraries generation ---

// The emitted collections all carry one shared depth identifier, so fast-check
// counts nesting across the whole model instead of per arbitrary instance: a
// self-referential slice inside a self-referential map is one recursion to the
// generator, which is what keeps a cyclic schema's generated values small.
const (
	arbDepthIdentifier   = "wiregen"
	arbArrayConstraints  = `{ maxLength: 2, size: "small", depthIdentifier: "` + arbDepthIdentifier + `" }`
	arbRecordConstraints = `{ maxKeys: 2, size: "small", depthIdentifier: "` + arbDepthIdentifier + `" }`
	// The shared identifier only biases fast-check toward shallow values, so a
	// recursive type terminates with high probability rather than always, and a
	// deep draw overflows the stack. At this depth on the shared counter a
	// recursive type's oneof is forced to its non-recursive first branch.
	arbRecursionCap = `{ maxDepth: 5, depthIdentifier: "` + arbDepthIdentifier + `" }`
	arbJSON         = "fc.jsonValue()"
	arbString       = "fc.string()"
	arbBool         = "fc.boolean()"
	arbBase64       = "fc.base64String()"
	arbFiniteNumber = "fc.double({ noNaN: true, noDefaultInfinity: true })"
)

// arbMapKeyDecl declares the key arbitrary every emitted dictionary uses.
const arbMapKeyDecl = `// A map key is never the string "__proto__". JSON.parse materializes such a key
// as an own property, while the generated decodeRecord assigns out[k], and
// out["__proto__"] sets the prototype instead of adding a key — so the entry
// would vanish from the decoded value and no round trip could hold.
const arbMapKey: fc.Arbitrary<string> = fc.string().filter((k) => k !== "__proto__");

`

// arbUses records what the emitted arbitraries referenced, so a declaration is
// written only when something uses it (an unused const is a lint error in the
// consumer, and the consumer cannot edit generated output).
type arbUses struct{ mapKey bool }

// arbName is the exported arbitrary's name for a TS type or enum name.
func arbName(tsName string) string { return "arb" + tsName }

// generateArbitraries writes the arbitraries file: one fast-check arbitrary per
// registered enum and per registered type, plus the ARBITRARY_BY_TYPE lookup
// table a consumer's property test iterates. Callers have already rejected a
// missing ArbitrariesImport by defaulting it in initDefaults.
func (r *Registry) generateArbitraries(w *strings.Builder, engine *astEngine) {
	var body strings.Builder
	used := &arbUses{}
	r.emitEnumArbitraries(&body)
	r.emitTypeArbitraries(&body, engine, used)
	r.emitArbitraryTable(&body, engine)

	w.WriteString(r.HeaderComment)
	w.WriteString("import fc from \"" + tsStringLiteral(r.ArbitrariesImport) + "\";\n")
	r.emitArbTypeImports(w, engine)
	if used.mapKey {
		w.WriteString(arbMapKeyDecl)
	}
	w.WriteString(body.String())
}

// emitArbTypeImports writes the `import type { … }` line naming every type and
// enum the emitted arbitraries annotate, sorted.
func (r *Registry) emitArbTypeImports(w *strings.Builder, engine *astEngine) {
	names := map[string]bool{}
	for _, ti := range engine.types {
		names[r.tsName(ti.Name)] = true
	}
	for name := range r.Enums {
		names[r.tsEnumName(name)] = true
	}
	sorted := slices.Sorted(maps.Keys(names))
	if len(sorted) > 0 {
		w.WriteString("import type { " + strings.Join(sorted, ", ") + " } from \"" + tsStringLiteral(r.TypesImportPath) + "\";\n")
	}
	w.WriteString("\n")
}

// emitEnumArbitraries writes one fc.constantFrom per registered enum, drawing
// exactly the registered value set — the vocabulary the decoder's reqOneOf
// membership check accepts, from the same EnumDef the check was emitted from.
func (r *Registry) emitEnumArbitraries(w *strings.Builder) {
	emitted := 0
	seen := map[string]bool{}
	for _, name := range enumNamesSlice(r.Enums) {
		tn := r.tsEnumName(name)
		if seen[tn] {
			continue
		}
		seen[tn] = true
		emitted++
		def := r.Enums[name]
		w.WriteString("export const " + arbName(tn) + ": fc.Arbitrary<" + tn + "> = ")
		if len(def.Values) == 0 {
			// A zero-value enum's TS type is `never` (see emitEnumTypes), so no
			// value inhabits it. fc.constantFrom() with no argument throws at
			// module evaluation, which would break every import of this file, so
			// emit a constant that can be drawn and never decoded — the decoder's
			// empty membership check rejects it, which is the honest outcome.
			w.WriteString("fc.constant(undefined as unknown as never);\n")
			continue
		}
		w.WriteString("fc.constantFrom(")
		for i, v := range def.Values {
			if i > 0 {
				w.WriteString(", ")
			}
			w.WriteString("\"" + tsStringLiteral(v) + "\"")
		}
		w.WriteString(");\n")
	}
	if emitted > 0 {
		w.WriteString("\n")
	}
}

// emitTypeArbitraries writes the struct and union arbitraries inside one
// fc.letrec, then one exported const per type.
func (r *Registry) emitTypeArbitraries(w *strings.Builder, engine *astEngine, used *arbUses) {
	if len(engine.types) == 0 {
		return
	}
	// fc.letrec ties every reference by name and resolves it when a value is
	// generated, so a mutual cycle (A holds a B, B holds an A) and a self
	// reference both work. A plain const referring to a const declared later
	// would be a temporal-dead-zone ReferenceError at module evaluation, which
	// no consumer's typecheck reports.
	w.WriteString("const arbModel = fc.letrec<{\n")
	for _, ti := range engine.types {
		tn := r.tsName(ti.Name)
		w.WriteString("  " + tn + ": " + tn + ";\n")
	}
	w.WriteString("}>((tie) => ({\n")
	reaches := r.arbReachability(engine)
	for _, ti := range engine.types {
		w.WriteString("  " + r.tsName(ti.Name) + ": " + r.arbForType(ti, used, reaches) + ",\n")
	}
	w.WriteString("}));\n\n")
	for _, ti := range engine.types {
		tn := r.tsName(ti.Name)
		w.WriteString("export const " + arbName(tn) + ": fc.Arbitrary<" + tn + "> = arbModel." + tn + ";\n")
	}
	w.WriteString("\n")
}

// arbForType renders one registered type's arbitrary: a oneof over the variants
// for a //wiregen:union type, otherwise a record over the fields. A struct that
// can reach itself is a depth-capped oneof of a leaf record and the full one.
func (r *Registry) arbForType(ti *typeInfo, used *arbUses, reaches map[string]map[string]bool) string {
	if ti.Union != nil {
		return r.arbForUnion(ti)
	}
	if len(ti.Fields) == 0 {
		return "fc.record({})"
	}
	if !reaches[ti.Name][ti.Name] {
		return r.arbRecord(ti, used, "  ", nil)
	}
	recursive := func(f *fieldInfo) bool {
		for _, ref := range r.arbRefs(f) {
			if ref == ti.Name || reaches[ref][ti.Name] {
				return true
			}
		}
		return false
	}
	return "fc.oneof(\n    " + arbRecursionCap + ",\n    " +
		r.arbRecord(ti, used, "    ", recursive) + ",\n    " +
		r.arbRecord(ti, used, "    ", nil) + ",\n  )"
}

// arbRecord renders a struct's fc.record at the given closing indent. With a
// non-nil recursive predicate it renders the LEAF record a depth cap forces:
// a recursive optional field is absent and a recursive collection is empty,
// while a recursive required struct field keeps its tie, because that type is
// capped too and Go admits no unbroken chain of required struct fields.
func (r *Registry) arbRecord(ti *typeInfo, used *arbUses, indent string, recursive func(*fieldInfo) bool) string {
	var b strings.Builder
	var required []string
	b.WriteString("fc.record(\n" + indent + "  {\n")
	for i := range ti.Fields {
		f := &ti.Fields[i]
		leaf := recursive != nil && recursive(f)
		if leaf && f.Optional {
			continue
		}
		if !f.Optional {
			required = append(required, "\""+tsStringLiteral(f.WireName)+"\"")
		}
		expr, note := r.arbFieldExpr(f, used)
		switch {
		case leaf && f.IsSlice:
			expr, note = "fc.constant<"+f.TSType+">([])", ""
		case leaf && f.IsMap:
			expr, note = "fc.constant<"+f.TSType+">({})", ""
		}
		if note != "" {
			b.WriteString(indent + "    // " + note + "\n")
		}
		b.WriteString(indent + "    " + tsPropName(f.WireName) + ": " + expr + ",\n")
	}
	b.WriteString(indent + "  },\n")
	// An optional key is ABSENT from the generated record rather than present
	// and undefined: the two are the same to JSON, and absence is the only one
	// assignable to the emitted `key?: T` under exactOptionalPropertyTypes.
	b.WriteString(indent + "  { requiredKeys: [" + strings.Join(required, ", ") + "] },\n" + indent + ")")
	return b.String()
}

// arbRefs lists the registered types a field's arbitrary ties to, following
// arbFieldExpr's and arbElemExpr's routing exactly.
func (r *Registry) arbRefs(f *fieldInfo) []string {
	if f == nil || f.JSONString || f.IsRaw || f.IsIface || f.IsBytes {
		return nil
	}
	if f.IsSlice || f.IsMap {
		return r.arbElemRefs(f.Elem)
	}
	if r.isMapped(f.GoTypeName) || f.IsEnum {
		return nil
	}
	if f.IsStruct && r.typeNames[f.GoTypeName] {
		return []string{f.GoTypeName}
	}
	return nil
}

// arbElemRefs is arbRefs for a collection element, which arbElemExpr routes
// before the raw and interface checks.
func (r *Registry) arbElemRefs(elem *fieldInfo) []string {
	switch {
	case elem == nil:
		return nil
	case elem.IsSlice || elem.IsMap:
		return r.arbElemRefs(elem.Elem)
	case r.isMapped(elem.GoTypeName):
		return nil
	case r.typeNames[elem.GoTypeName]:
		return []string{elem.GoTypeName}
	}
	return nil
}

// arbReachability answers, for every registered type, which registered types
// its arbitrary can reach through ties, unions included.
func (r *Registry) arbReachability(engine *astEngine) map[string]map[string]bool {
	edges := r.arbEdges(engine)
	reaches := make(map[string]map[string]bool, len(edges))
	for _, ti := range engine.types {
		reaches[ti.Name] = reachableFrom(edges[ti.Name], edges)
	}
	return reaches
}

// arbEdges maps each registered type to the types its arbitrary ties to.
func (r *Registry) arbEdges(engine *astEngine) map[string][]string {
	edges := make(map[string][]string, len(engine.types))
	for _, ti := range engine.types {
		if ti.Union != nil {
			for _, v := range ti.Union.Variants {
				if r.typeNames[v] {
					edges[ti.Name] = append(edges[ti.Name], v)
				}
			}
			continue
		}
		for i := range ti.Fields {
			edges[ti.Name] = append(edges[ti.Name], r.arbRefs(&ti.Fields[i])...)
		}
	}
	return edges
}

// reachableFrom is the set of nodes reachable from start over edges.
func reachableFrom(start []string, edges map[string][]string) map[string]bool {
	seen := map[string]bool{}
	stack := slices.Clone(start)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		stack = append(stack, edges[n]...)
	}
	return seen
}

// arbForUnion renders a //wiregen:union type's arbitrary: a draw from any
// variant, which is exactly the emitted `export type X = A | B | C`. The
// discriminator key is NOT injected — the variant interfaces do not declare it,
// and a variant decoder drops an undeclared key, so a value carrying one could
// not round-trip.
func (r *Registry) arbForUnion(ti *typeInfo) string {
	var variants []string
	for _, v := range ti.Union.Variants {
		if r.typeNames[v] {
			variants = append(variants, "tie(\""+r.tsName(v)+"\")")
		}
	}
	if len(variants) == 0 {
		return "fc.constant(undefined as unknown as " + r.tsName(ti.Name) + ")"
	}
	return "fc.oneof(" + strings.Join(variants, ", ") + ")"
}

// arbFieldExpr returns the arbitrary expression for one struct field, and a note
// to emit above it when the expression is opaque. The routing order mirrors
// reqExpr's exactly, because the arbitrary has to satisfy the validator that
// field's decoder emitted.
func (r *Registry) arbFieldExpr(f *fieldInfo, used *arbUses) (expr, note string) {
	if f.JSONString {
		return arbString, ""
	}
	if f.IsRaw || f.IsIface {
		return arbJSON, ""
	}
	if f.IsBytes {
		return arbBase64, ""
	}
	if f.IsSlice {
		return "fc.array(" + r.arbElemExpr(f.Elem, used) + ", " + arbArrayConstraints + ")", ""
	}
	if f.IsMap {
		used.mapKey = true
		return "fc.dictionary(arbMapKey, " + r.arbElemExpr(f.Elem, used) + ", " + arbRecordConstraints + ")", ""
	}
	if r.isMapped(f.GoTypeName) {
		return r.arbMappedExpr(f.GoTypeName, f.TSType)
	}
	if f.IsEnum {
		if _, ok := r.Enums[f.GoTypeName]; ok {
			return arbName(r.tsEnumName(f.GoTypeName)), ""
		}
		return arbString, ""
	}
	if f.IsStruct && r.typeNames[f.GoTypeName] {
		return "tie(\"" + r.tsName(f.GoTypeName) + "\")", ""
	}
	if f.TSType == tsUnknown {
		return arbJSON, ""
	}
	return arbPrim(f.TSType), ""
}

// arbElemExpr returns the arbitrary for a slice element or map value, recursing
// through nested collections the way elemDecoderExpr does.
func (r *Registry) arbElemExpr(elem *fieldInfo, used *arbUses) string {
	if elem == nil {
		return arbJSON
	}
	if elem.IsSlice {
		return "fc.array(" + r.arbElemExpr(elem.Elem, used) + ", " + arbArrayConstraints + ")"
	}
	if elem.IsMap {
		used.mapKey = true
		return "fc.dictionary(arbMapKey, " + r.arbElemExpr(elem.Elem, used) + ", " + arbRecordConstraints + ")"
	}
	if r.isMapped(elem.GoTypeName) {
		expr, _ := r.arbMappedExpr(elem.GoTypeName, elem.TSType)
		return expr
	}
	if r.typeNames[elem.GoTypeName] {
		return "tie(\"" + r.tsName(elem.GoTypeName) + "\")"
	}
	if _, ok := r.Enums[elem.GoTypeName]; ok {
		return arbName(r.tsEnumName(elem.GoTypeName))
	}
	if elem.IsBytes {
		return arbBase64
	}
	switch elem.TSType {
	case tsString, tsNumber, tsBoolean:
		return arbPrim(elem.TSType)
	}
	return arbJSON
}

// isMapped reports whether the consumer supplied a type or decoder mapping for
// goTypeName, which is the one case wiregen has no shape for.
func (r *Registry) isMapped(goTypeName string) bool {
	if _, ok := r.DecoderMappings[goTypeName]; ok {
		return true
	}
	_, ok := r.TypeMappings[goTypeName]
	return ok
}

// arbMappedExpr renders a consumer-mapped type's arbitrary. A mapping to a
// primitive TS type is honoured exactly; anything else is opaque, because the
// mapping's shape lives in the consumer's TS expression and not in wiregen's
// parsed model — so the value is an arbitrary JSON one and the annotation is a
// cast, the same trust the emitted `o[k] as T` decoder already extends.
func (r *Registry) arbMappedExpr(goTypeName, tsType string) (expr, note string) {
	switch tsType {
	case tsString, tsNumber, tsBoolean:
		return arbPrim(tsType), ""
	}
	return "fc.jsonValue().filter((v) => v !== null) as unknown as fc.Arbitrary<" + tsType + ">",
		"opaque: " + goTypeName + " is consumer-mapped, so its shape is not in wiregen's model"
}

// arbPrim is the arbitrary for a primitive TS type, mirroring primHelperAST's
// choice of validator: a number is finite, because reqNum rejects NaN and
// Infinity (and JSON has no spelling for either).
func arbPrim(tsType string) string {
	switch tsType {
	case tsString:
		return arbString
	case tsBoolean:
		return arbBool
	default:
		return arbFiniteNumber
	}
}

// emitArbitraryTable writes the ARBITRARY_BY_TYPE lookup table, keyed by TS
// name and total over the registry: every registered type and enum has a row,
// and a row exists for nothing else. A consumer asserts that set against its
// exported decoders, which is what makes an orphaned arbitrary impossible.
func (r *Registry) emitArbitraryTable(w *strings.Builder, engine *astEngine) {
	rows := map[string]bool{}
	for _, ti := range engine.types {
		rows[r.tsName(ti.Name)] = true
	}
	for name := range r.Enums {
		rows[r.tsEnumName(name)] = true
	}
	w.WriteString("export const ARBITRARY_BY_TYPE: Record<string, fc.Arbitrary<unknown>> = {\n")
	for _, name := range slices.Sorted(maps.Keys(rows)) {
		w.WriteString("  " + name + ": " + arbName(name) + ",\n")
	}
	w.WriteString("};\n")
}

// --- registry generation ---

// generateRegistry writes the registry file. Callers (Generate,
// GenerateRegistry) have already rejected a missing BusImport /
// ValidatorsImport for the selected registry mode.
func (r *Registry) generateRegistry(w *strings.Builder) {
	w.WriteString(r.HeaderComment)

	seen := map[string]bool{}
	for _, e := range r.SSEEvents {
		seen[r.sseDecoderName(e.TypeName)] = true
	}
	decoderImports := slices.Sorted(maps.Keys(seen))

	if r.SelfContainedRegistry {
		w.WriteString("import { " + strings.Join(decoderImports, ", ") + " } from \"" + moduleSpecifier(r.DecodersFilename) + "\";\n")
		w.WriteString("import type { Decoder } from \"" + tsStringLiteral(r.ValidatorsImport) + "\";\n\n")
		w.WriteString("const registry = new Map<string, Decoder<unknown>>();\n\n")
		w.WriteString("export function " + r.RegistryFuncName + "(): void {\n")
		for _, e := range r.SSEEvents {
			w.WriteString("  registry.set(\"" + tsStringLiteral(e.EventType) + "\", " + r.sseDecoderName(e.TypeName) + " as Decoder<unknown>);\n")
		}
		w.WriteString("}\n\n")
		w.WriteString("export function getSSEDecoder(eventType: string): Decoder<unknown> | undefined {\n")
		w.WriteString("  return registry.get(eventType);\n")
		w.WriteString("}\n")
	} else {
		w.WriteString("import { " + r.RegisterFuncName + " } from \"" + tsStringLiteral(r.BusImport) + "\";\n")
		w.WriteString("import { " + strings.Join(decoderImports, ", ") + " } from \"" + moduleSpecifier(r.DecodersFilename) + "\";\n\n")
		w.WriteString("export function " + r.RegistryFuncName + "(): void {\n")
		for _, e := range r.SSEEvents {
			w.WriteString("  " + r.RegisterFuncName + "(\"" + tsStringLiteral(e.EventType) + "\", " + r.sseDecoderName(e.TypeName) + ");\n")
		}
		w.WriteString("}\n")
	}
}

// --- constants generation ---

// generateConstants writes the constants file. Callers (Generate,
// GenerateConstants) have already rejected constants whose TSName sanitizes
// to an empty identifier via validateConstants.
func (r *Registry) generateConstants(w *strings.Builder) {
	w.WriteString(r.HeaderComment)
	for _, c := range r.Constants {
		fmt.Fprintf(w, "export const %s = %d;\n", sanitizeTSIdent(c.TSName), c.Value)
	}
}

// --- validators module generation (library-owned) ---

// generateValidators writes the library-owned validators module: the full set
// of 11 runtime helper functions plus the Decoder<T> type alias that the
// generated decoders import. The content is constant (it does not depend on
// the registered types) — it is THE implementation of the "Validators
// contract", carried under the same DO-NOT-EDIT banner as every other
// generated file. Consumers regenerate it (via WithValidatorsFile or
// GenerateValidators) instead of copying and owning it; the v1-era
// copy-once-then-own starter posture is retired.
func (r *Registry) generateValidators(w *strings.Builder) {
	hc := r.HeaderComment
	if hc == "" {
		hc = defaultHeaderComment
	}
	w.WriteString(hc)
	w.WriteString(validatorsBody)
}

// validatorsBody is the working TypeScript implementation of the validators
// contract: asObject, asArray, reqStr/reqNum/reqBool, optStr/optNum/optBool,
// reqOneOf, decodeArray, decodeRecord (11 functions) plus
// `export type Decoder<T> = (v: unknown) => T`.
const validatorsBody = `/** A decoder is a pure function that returns T or throws on shape mismatch. */
export type Decoder<T> = (v: unknown) => T;

function fail(path: string, msg: string): never {
  throw new TypeError(` + "`${path}: ${msg}`" + `);
}

function typeName(v: unknown): string {
  if (v === null) {
    return "null";
  }
  if (Array.isArray(v)) {
    return "array";
  }
  return typeof v;
}

/** Asserts v is a plain object (not array, not null). Returns the typed map. */
export function asObject(v: unknown, path = "$"): Record<string, unknown> {
  if (typeof v !== "object" || v === null || Array.isArray(v)) {
    fail(path, ` + "`expected object, got ${typeName(v)}`" + `);
  }
  return v as Record<string, unknown>;
}

/** Asserts v is an array; returns it. */
export function asArray(v: unknown, path = "$"): unknown[] {
  if (!Array.isArray(v)) {
    fail(path, ` + "`expected array, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Required string field; throws if absent or not a string. */
export function reqStr(o: Record<string, unknown>, key: string, path = "$"): string {
  const v = o[key];
  if (typeof v !== "string") {
    fail(` + "`${path}.${key}`" + `, ` + "`expected string, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Required finite number field. NaN and Infinity are rejected. */
export function reqNum(o: Record<string, unknown>, key: string, path = "$"): number {
  const v = o[key];
  if (typeof v !== "number" || !Number.isFinite(v)) {
    fail(` + "`${path}.${key}`" + `, ` + "`expected number, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Required boolean field. */
export function reqBool(o: Record<string, unknown>, key: string, path = "$"): boolean {
  const v = o[key];
  if (typeof v !== "boolean") {
    fail(` + "`${path}.${key}`" + `, ` + "`expected boolean, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Optional string: undefined if key absent, otherwise must be a string. */
export function optStr(o: Record<string, unknown>, key: string, path = "$"): string | undefined {
  const v = o[key];
  if (v === undefined) {
    return undefined;
  }
  if (typeof v !== "string") {
    fail(` + "`${path}.${key}`" + `, ` + "`expected string or undefined, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Optional finite number. */
export function optNum(o: Record<string, unknown>, key: string, path = "$"): number | undefined {
  const v = o[key];
  if (v === undefined) {
    return undefined;
  }
  if (typeof v !== "number" || !Number.isFinite(v)) {
    fail(` + "`${path}.${key}`" + `, ` + "`expected number or undefined, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Optional boolean. */
export function optBool(o: Record<string, unknown>, key: string, path = "$"): boolean | undefined {
  const v = o[key];
  if (v === undefined) {
    return undefined;
  }
  if (typeof v !== "boolean") {
    fail(` + "`${path}.${key}`" + `, ` + "`expected boolean or undefined, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Required string with a fixed enum membership check. */
export function reqOneOf<T extends string>(
  o: Record<string, unknown>,
  key: string,
  vals: readonly T[],
  path = "$",
): T {
  const v = o[key];
  if (typeof v !== "string" || !(vals as readonly string[]).includes(v)) {
    fail(` + "`${path}.${key}`" + `, ` + "`expected one of ${vals.join(\"|\")}, got ${JSON.stringify(v)}`" + `);
  }
  return v as T;
}

/** Decodes an array of T using the given per-element decoder. The
 *  per-element path is the parent path + "[i]" so error messages
 *  locate the offending entry. */
export function decodeArray<T>(v: unknown, decode: Decoder<T>, path = "$"): T[] {
  const arr = asArray(v, path);
  return arr.map((el, i) => {
    try {
      return decode(el);
    } catch (e) {
      if (e instanceof TypeError) {
        throw new TypeError(` + "`${path}[${String(i)}]: ${e.message}`" + `, { cause: e });
      }
      throw e;
    }
  });
}

/** Decodes a Record<string, T> by iterating own keys and applying
 *  decode to each value. Error messages include the key. */
export function decodeRecord<T>(v: unknown, decode: Decoder<T>, path = "$"): Record<string, T> {
  const o = asObject(v, path);
  const out: Record<string, T> = {};
  for (const [k, val] of Object.entries(o)) {
    try {
      out[k] = decode(val);
    } catch (e) {
      if (e instanceof TypeError) {
        throw new TypeError(` + "`${path}.${k}: ${e.message}`" + `, { cause: e });
      }
      throw e;
    }
  }
  return out;
}
`
