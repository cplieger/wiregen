package wiregen

import (
	"fmt"
	"sort"
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
	sort.Slice(enumNames, func(i, j int) bool { return r.tsEnumName(enumNames[i]) < r.tsEnumName(enumNames[j]) })
	for _, name := range enumNames {
		def := r.Enums[name]
		w.WriteString("export type " + r.tsEnumName(name) + " = ")
		if len(def.Values) == 0 {
			// A registered enum that resolved to zero values (no explicit
			// Values and no string const block discovered in a loaded
			// package) must never emit "= ;" (invalid TS — see the
			// mustNotContain guard in
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
	sorted := make([]string, 0, len(usedSet))
	for n := range usedSet {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
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

	// Sort keys for determinism
	keys := make([]string, 0, len(dm))
	for k := range dm {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
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

	// Primitive
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

	// Primitive optional
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

	// Check DecoderMappings
	if expr, ok := r.DecoderMappings[elem.GoTypeName]; ok {
		used.opaqueExpr(expr)
		return "(v) => " + expr + "({\"" + wn + "\": v} as Record<string, unknown>, \"" + wn + "\", \"" + path + "\")"
	}
	// Check TypeMappings
	if mapped, ok := r.TypeMappings[elem.GoTypeName]; ok {
		used.opaqueExpr(mapped)
		return "(v) => v as " + mapped
	}
	// Check if elem is a registered struct
	if r.typeNames[elem.GoTypeName] {
		return r.decoderName(elem.GoTypeName)
	}
	// Check if elem is an enum (use GoTypeName which is the Go type name)
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

// --- registry generation ---

// generateRegistry writes the registry file. Callers (Generate,
// GenerateRegistry) have already rejected a missing BusImport /
// ValidatorsImport for the selected registry mode.
func (r *Registry) generateRegistry(w *strings.Builder) {
	w.WriteString(r.HeaderComment)

	decoderImports := make([]string, 0)
	seen := map[string]bool{}
	for _, e := range r.SSEEvents {
		dn := r.sseDecoderName(e.TypeName)
		if !seen[dn] {
			seen[dn] = true
			decoderImports = append(decoderImports, dn)
		}
	}
	sort.Strings(decoderImports)

	if r.SelfContainedRegistry {
		w.WriteString("import { " + strings.Join(decoderImports, ", ") + " } from \"./decoders.gen.js\";\n")
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
		w.WriteString("import { " + strings.Join(decoderImports, ", ") + " } from \"./decoders.gen.js\";\n\n")
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
