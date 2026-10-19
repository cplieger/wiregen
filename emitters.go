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

	helperDecodeArray  = "decodeArray"
	helperDecodeRecord = "decodeRecord"
	helperReqStr       = "reqStr"
	helperReqNum       = "reqNum"
	helperReqBool      = "reqBool"
)

// --- types generation ---

func (g *generator) generateTypes(w *strings.Builder, engine *astEngine) {
	w.WriteString(g.o.headerComment)
	g.emitEnumTypes(w)
	g.emitUnionTypes(w, engine)
	g.emitStructInterfaces(w, engine)
}

// emitEnumTypes writes the `export type X = "a" | "b";` string-union aliases,
// deduplicated and sorted by TS name. Dedup iterates Go names in sorted order
// so a TS-name collision has a deterministic winner (the first Go name in
// sort order), matching emitEnumConsts' dedup order.
func (g *generator) emitEnumTypes(w *strings.Builder) {
	enumNames := make([]string, 0, len(g.enums))
	seenEnumTS := map[string]bool{}
	for _, name := range enumNamesSlice(g.enums) {
		tn := g.tsEnumName(name)
		if seenEnumTS[tn] {
			continue
		}
		seenEnumTS[tn] = true
		enumNames = append(enumNames, name)
	}
	slices.SortStableFunc(enumNames, func(a, b string) int {
		return cmp.Compare(g.tsEnumName(a), g.tsEnumName(b))
	})
	for _, name := range enumNames {
		def := g.enums[name]
		w.WriteString("export type " + g.tsEnumName(name) + " = ")
		for i, v := range def.values {
			if i > 0 {
				w.WriteString(" | ")
			}
			w.WriteString("\"" + tsStringLiteral(v) + "\"")
		}
		w.WriteString(";\n\n")
	}
}

func (g *generator) emitUnionTypes(w *strings.Builder, engine *astEngine) {
	for _, ti := range engine.types {
		if ti.Union != nil {
			g.emitUnionType(w, ti)
		}
	}
}

func (g *generator) emitStructInterfaces(w *strings.Builder, engine *astEngine) {
	for _, ti := range engine.types {
		if ti.Union != nil {
			continue
		}
		if ti.Doc != "" {
			w.WriteString(ti.Doc)
		}
		w.WriteString("export interface " + g.tsName(ti.Name) + " {\n")
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
	if f.optional() {
		w.WriteString("  " + tsPropName(f.WireName) + "?: " + f.TSType + ";\n")
	} else {
		w.WriteString("  " + tsPropName(f.WireName) + ": " + f.TSType + ";\n")
	}
}

// --- decoders generation ---

// usedIdents accumulates the identifiers decoder emission actually uses, so
// the import/const header lines are derived from what was emitted rather than
// re-discovered by scanning the emitted text (a heuristic that could match an
// identifier inside an emitted string literal). The one place text scanning
// survives is `opaque`: Mappings values are consumer-supplied TS
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

func (g *generator) generateDecoders(w *strings.Builder, engine *astEngine) {
	body, used := g.decoderBodies(engine)

	w.WriteString(g.o.headerComment)
	g.emitHelperImports(w, used)
	g.emitTypeImports(w, used, engine)
	g.emitEnumConsts(w, used)
	w.WriteString(body)
}

// decoderBodies returns the decoders' body and the identifiers it used,
// which decide the import and const lines above it.
func (g *generator) decoderBodies(engine *astEngine) (string, *usedIdents) {
	var bodies strings.Builder
	used := newUsedIdents()
	for _, ti := range engine.types {
		if ti.Union == nil {
			g.emitDecoder(&bodies, ti, used)
		}
	}
	for _, ti := range engine.types {
		if ti.Union != nil {
			g.emitUnionDecoder(&bodies, ti, used)
		}
	}
	return bodies.String(), used
}

// emitHelperImports writes the validators-module import, listing (in the
// contract's canonical order) only the helpers the emitted decoders call.
func (g *generator) emitHelperImports(w *strings.Builder, used *usedIdents) {
	var usedHelpers []string
	for _, h := range validatorsExports[1:] {
		if used.helpers[h] || used.opaqueRefs(h) {
			usedHelpers = append(usedHelpers, h)
		}
	}
	w.WriteString("import { ")
	if len(usedHelpers) > 0 {
		w.WriteString(strings.Join(usedHelpers, ", "))
		w.WriteString(", ")
	}
	w.WriteString("type Decoder } from \"" + g.validatorsSpec(g.o.decodersFilename) + "\";\n")
}

// emitTypeImports writes the `import type { … }` line for the type/enum names
// the emitted decoders reference, sorted; it emits nothing when none are used.
func (g *generator) emitTypeImports(w *strings.Builder, used *usedIdents, engine *astEngine) {
	candidateNames := make([]string, 0)
	for _, ti := range engine.types {
		candidateNames = append(candidateNames, g.tsName(ti.Name))
	}
	enumSeen := map[string]bool{}
	for name := range g.enums {
		tn := g.tsEnumName(name)
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
		w.WriteString(" } from \"" + g.typesSpec(g.o.decodersFilename) + "\";\n")
	}
	w.WriteString("\n")
}

// emitEnumConsts writes the `const XS = [...] as const;` value arrays for the
// enums the emitted decoders reference (deduped), then a trailing blank line.
func (g *generator) emitEnumConsts(w *strings.Builder, used *usedIdents) {
	emitted := map[string]bool{}
	for _, name := range enumNamesSlice(g.enums) {
		constN := g.enumConstName(name)
		if emitted[constN] || (!used.enums[name] && !used.opaqueRefs(constN)) {
			continue
		}
		emitted[constN] = true
		def := g.enums[name]
		w.WriteString("const " + constN + " = [")
		for i, v := range def.values {
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

func (g *generator) emitDecoder(w *strings.Builder, ti *typeInfo, used *usedIdents) {
	tn := g.tsName(ti.Name)
	path := "$." + g.pathName(tn)
	used.helper("asObject")
	used.typeRef(tn)
	w.WriteString("export const " + g.decoderName(ti.Name) + ": Decoder<" + tn + "> = (v) => {\n")
	w.WriteString("  const o = asObject(v, \"" + path + "\");\n")
	fields := make([]*fieldInfo, 0, len(ti.Fields))
	for i := range ti.Fields {
		fields = append(fields, &ti.Fields[i])
	}
	g.emitFieldDecodes(w, tn, "", fields, path, used)
	w.WriteString("};\n\n")
}

// emitFieldDecodes writes the statements that decode fields from o into an
// out of type outType and return it; lead, when set, is out's first member.
func (g *generator) emitFieldDecodes(w *strings.Builder, outType, lead string, fields []*fieldInfo, path string, used *usedIdents) {
	var reqFields, optFields []*fieldInfo
	for _, f := range fields {
		if f.optional() {
			optFields = append(optFields, f)
		} else {
			reqFields = append(reqFields, f)
		}
	}
	if lead != "" || len(reqFields) > 0 || len(optFields) > 0 {
		w.WriteString("  const out: " + outType + " = {\n")
		if lead != "" {
			w.WriteString("    " + lead + ",\n")
		}
		for _, f := range reqFields {
			w.WriteString("    " + tsLiteralKey(f.WireName) + ": " + g.reqExpr(f, path, used) + ",\n")
		}
		w.WriteString("  };\n")
	} else {
		w.WriteString("  const out: " + outType + " = {};\n")
	}
	for _, f := range optFields {
		g.emitOptionalField(w, f, path, used)
	}
	w.WriteString("  return out;\n")
}

func (g *generator) reqExpr(f *fieldInfo, path string, used *usedIdents) string {
	return f.Bounds.validatorCheck(g.reqDecode(f, path, used), "\""+path+"."+tsStringLiteral(f.WireName)+"\"", used)
}

// ownRead is the expression reading o's own property wn; an inherited one
// (constructor, toString) reads as absent.
func ownRead(wn string, used *usedIdents) string {
	used.helper("own")
	return "own(o, \"" + wn + "\")"
}

// reqDecode is reqExpr before the bound checks. A required field has no
// pointer level (presence.go), so null reaches it only as encoding/json's nil
// collection or []byte, which reads as empty.
func (g *generator) reqDecode(f *fieldInfo, path string, used *usedIdents) string {
	wn := tsStringLiteral(f.WireName)
	read := ownRead(wn, used)
	at := ", \"" + path + "\")"
	switch f.Kind {
	case kindRaw:
		return read + " as unknown"
	case kindBytes:
		used.helper(helperReqStr)
		return read + " === null ? \"\" : reqStr(o, \"" + wn + "\"" + at
	case kindSlice, kindMap:
		helper, empty := helperDecodeArray, "[]"
		if f.Kind == kindMap {
			helper, empty = helperDecodeRecord, "{}"
		}
		used.helper(helper)
		decode := helper + "(" + read + ", " + g.elemDecoderExpr(f.Elem, wn, path, used) + ", \"" + path + "." + wn + "\")"
		if f.refusesNull() {
			return decode
		}
		return read + " === null ? " + empty + " : " + decode
	case kindMapped:
		if f.Mapping.Decoder != "" {
			used.opaqueExpr(f.Mapping.Decoder)
			return f.Mapping.Decoder + "(o, \"" + wn + "\"" + at
		}
		used.opaqueExpr(f.TSType)
		return read + " as " + f.TSType
	case kindEnum:
		used.helper("reqOneOf")
		used.enumUse(f.GoTypeName)
		return "reqOneOf(o, \"" + wn + "\", " + g.enumConstName(f.GoTypeName) + at
	case kindStruct:
		return g.decoderName(f.GoTypeName) + "(" + read + ")"
	}
	helper := primHelpers[f.Kind]
	used.helper(helper)
	return helper + "(o, \"" + wn + "\"" + at
}

var primHelpers = map[wireKind]string{
	kindString: helperReqStr, kindTime: helperReqStr, kindQuoted: helperReqStr, kindBool: helperReqBool,
	kindNumber: helperReqNum, kindInt: helperReqNum, kindUint: helperReqNum,
}

// emitOptionalField decodes f into out when present. Null reads as absent,
// except as a json.RawMessage's data and where a Count bound refuses it. The
// block scopes its local, so no field name can collide with it.
func (g *generator) emitOptionalField(w *strings.Builder, f *fieldInfo, path string, used *usedIdents) {
	cond := "$v !== undefined"
	if f.Kind != kindRaw && !f.refusesNull() {
		cond += " && $v !== null"
	}
	w.WriteString("  {\n    const $v = " + ownRead(tsStringLiteral(f.WireName), used) + ";\n")
	w.WriteString("    if (" + cond + ") " + setOwn(f.WireName, g.reqExpr(f, path, used)) + ";\n  }\n")
}

// elemDecoderExpr returns the per-element decoder expression for the slice
// element or map value described by elem. wn and path are the owning field's
// wire name and type-level path: a mapping's element decoder keeps its
// (obj, key, path) contract by being called through a synthesized single-key
// object carrying the real wire name, so its error messages locate the actual
// field (decodeArray/decodeRecord prefix the element index/key themselves).
func (g *generator) elemDecoderExpr(elem *fieldInfo, wn, path string, used *usedIdents) string {
	dec := g.elemDecode(elem, wn, path, used)
	if elem.Bounds != nil {
		dec = "(v) => " + elem.Bounds.validatorCheck("("+dec+")(v)", "\""+path+"."+wn+"\"", used)
	}
	if elem.nullElem() {
		dec = "(v) => v === null ? null : (" + dec + ")(v)"
	}
	return dec
}

// elemDecode is elemDecoderExpr before the element's bound checks. Each
// collection level accepts null as empty, as encoding/json writes a nil one,
// unless a Count lower bound refuses it.
func (g *generator) elemDecode(elem *fieldInfo, wn, path string, used *usedIdents) string {
	switch elem.Kind {
	case kindSlice, kindMap:
		helper, empty := helperDecodeArray, "[]"
		if elem.Kind == kindMap {
			helper, empty = helperDecodeRecord, "{}"
		}
		used.helper(helper)
		decode := helper + "(v, " + g.elemDecoderExpr(elem.Elem, wn, path, used) + ", \"" + path + "." + wn + "\")"
		if elem.refusesNull() {
			return "(v) => " + decode
		}
		return "(v) => v === null ? " + empty + " : " + decode
	case kindMapped:
		if elem.Mapping.Decoder != "" {
			used.opaqueExpr(elem.Mapping.Decoder)
			return "(v) => " + elem.Mapping.Decoder + "({\"" + wn + "\": v} as globalThis.Record<string, unknown>, \"" + wn + "\", \"" + path + "\")"
		}
		used.opaqueExpr(elem.TSType)
		return "(v) => v as " + elem.TSType
	case kindStruct:
		return g.decoderName(elem.GoTypeName)
	case kindEnum:
		constName := g.enumConstName(elem.GoTypeName)
		used.enumUse(elem.GoTypeName)
		used.typeRef(g.tsEnumName(elem.GoTypeName))
		return "(v) => { const s = v as string; if (!" + constName + ".includes(s as never)) throw new globalThis.TypeError(\"invalid enum value: \" + s); return s as " + g.tsEnumName(elem.GoTypeName) + "; }"
	case kindBytes:
		// A nil []byte element marshals to null, which reads as empty.
		return "(v) => { if (v === null) return \"\"; if (typeof v !== \"string\") throw new globalThis.TypeError(\"expected string\"); return v; }"
	case kindRaw:
		return tsIdentityCast
	}
	ts := elem.TSType
	return "(v) => { if (typeof v !== \"" + ts + "\") throw new globalThis.TypeError(\"expected " + ts + "\"); return v as " + ts + "; }"
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

// arbName is the exported arbitrary's name for a TS type or enum name.
func arbName(tsName string) string { return "arb" + tsName }

// generateArbitraries writes the arbitraries file: one fast-check arbitrary per
// registered enum and per registered type, plus the ARBITRARY_BY_TYPE lookup
// table a consumer's property test iterates. Callers have already rejected a
// missing ArbitrariesImport by defaulting it in initDefaults.
func (g *generator) generateArbitraries(w *strings.Builder, engine *astEngine) {
	var body strings.Builder
	g.emitEnumArbitraries(&body)
	g.emitTypeArbitraries(&body, engine)
	g.emitArbitraryTable(&body, engine)

	w.WriteString(g.o.headerComment)
	w.WriteString("import fc from \"" + tsStringLiteral(g.o.arbitrariesImport) + "\";\n")
	g.emitArbTypeImports(w, engine)
	w.WriteString(body.String())
}

// emitArbTypeImports writes the `import type { … }` line naming every type and
// enum the emitted arbitraries annotate, sorted.
func (g *generator) emitArbTypeImports(w *strings.Builder, engine *astEngine) {
	names := map[string]bool{}
	for _, ti := range engine.types {
		names[g.tsName(ti.Name)] = true
	}
	for name := range g.enums {
		names[g.tsEnumName(name)] = true
	}
	sorted := slices.Sorted(maps.Keys(names))
	if len(sorted) > 0 {
		w.WriteString("import type { " + strings.Join(sorted, ", ") + " } from \"" + g.typesSpec(g.o.arbitrariesFilename) + "\";\n")
	}
	w.WriteString("\n")
}

// emitEnumArbitraries writes one fc.constantFrom per registered enum, drawing
// exactly the registered value set — the vocabulary the decoder's reqOneOf
// membership check accepts, from the same members the check was emitted from.
func (g *generator) emitEnumArbitraries(w *strings.Builder) {
	emitted := 0
	seen := map[string]bool{}
	for _, name := range enumNamesSlice(g.enums) {
		tn := g.tsEnumName(name)
		if seen[tn] {
			continue
		}
		seen[tn] = true
		emitted++
		def := g.enums[name]
		w.WriteString("export const " + arbName(tn) + ": fc.Arbitrary<" + tn + "> = ")
		w.WriteString("fc.constantFrom(")
		for i, v := range def.values {
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
func (g *generator) emitTypeArbitraries(w *strings.Builder, engine *astEngine) {
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
		tn := g.tsName(ti.Name)
		w.WriteString("  " + tn + ": " + tn + ";\n")
	}
	w.WriteString("}>((tie) => ({\n")
	reaches := g.arbReachability(engine)
	for _, ti := range engine.types {
		w.WriteString("  " + g.tsName(ti.Name) + ": " + g.arbForType(ti, reaches) + ",\n")
	}
	w.WriteString("}));\n\n")
	for _, ti := range engine.types {
		tn := g.tsName(ti.Name)
		w.WriteString("export const " + arbName(tn) + ": fc.Arbitrary<" + tn + "> = arbModel." + tn + ";\n")
	}
	w.WriteString("\n")
}

// arbForType renders one registered type's arbitrary: a oneof over the cases
// for a union, otherwise a record over the fields. A type that can reach
// itself is a depth-capped oneof of its leaf records and its full ones.
func (g *generator) arbForType(ti *typeInfo, reaches map[string]map[string]bool) string {
	var recursive func(*fieldInfo) bool
	if reaches[ti.Name][ti.Name] {
		recursive = func(f *fieldInfo) bool {
			for _, ref := range g.arbRefs(f) {
				if ref == ti.Name || reaches[ref][ti.Name] {
					return true
				}
			}
			return false
		}
	}
	if ti.Union != nil {
		return g.arbForUnion(ti, recursive)
	}
	if len(ti.Fields) == 0 {
		return "fc.record({})"
	}
	fields := make([]*fieldInfo, 0, len(ti.Fields))
	for i := range ti.Fields {
		fields = append(fields, &ti.Fields[i])
	}
	if recursive == nil {
		return g.arbRecord(fields, "  ", nil, nil)
	}
	return "fc.oneof(\n    " + arbRecursionCap + ",\n    " +
		g.arbRecord(fields, "    ", recursive, nil) + ",\n    " +
		g.arbRecord(fields, "    ", nil, nil) + ",\n  )"
}

// arbRecord renders an fc.record over fields at the given closing indent,
// fixed giving a field's arbitrary by wire name. With a non-nil recursive
// predicate it renders the LEAF record a depth cap forces: a recursive
// optional field is absent and a recursive collection is empty, while a
// recursive required struct field keeps its tie, because that type is capped
// too and Go admits no unbroken chain of required struct fields.
func (g *generator) arbRecord(fields []*fieldInfo, indent string, recursive func(*fieldInfo) bool, fixed map[string]string) string {
	var b strings.Builder
	var required []string
	b.WriteString("fc.record(\n" + indent + "  {\n")
	for _, f := range fields {
		leaf := recursive != nil && recursive(f)
		if leaf && f.optional() {
			continue
		}
		if !f.optional() {
			required = append(required, "\""+tsStringLiteral(f.WireName)+"\"")
		}
		expr, note := g.arbFieldExpr(f)
		if fx, ok := fixed[f.WireName]; ok {
			expr, note = fx, ""
		}
		switch {
		case leaf && f.Kind == kindSlice:
			expr, note = "fc.constant<"+f.TSType+">([])", ""
		case leaf && f.Kind == kindMap:
			expr, note = "fc.constant<"+f.TSType+">({})", ""
		}
		if note != "" {
			b.WriteString(indent + "    // " + note + "\n")
		}
		b.WriteString(indent + "    " + tsLiteralKey(f.WireName) + ": " + expr + ",\n")
	}
	b.WriteString(indent + "  },\n")
	// An optional key is ABSENT from the generated record rather than present
	// and undefined: the two are the same to JSON, and absence is the only one
	// assignable to the emitted `key?: T` under exactOptionalPropertyTypes.
	b.WriteString(indent + "  { requiredKeys: [" + strings.Join(required, ", ") + "] },\n" + indent + ")")
	return b.String()
}

// arbRefs lists the registered types a field's arbitrary ties to.
func (g *generator) arbRefs(f *fieldInfo) []string {
	switch {
	case f == nil:
		return nil
	case f.Kind == kindSlice || f.Kind == kindMap:
		return g.arbRefs(f.Elem)
	case f.Kind == kindStruct:
		return []string{f.GoTypeName}
	}
	return nil
}

// arbReachability answers, for every registered type, which registered types
// its arbitrary can reach through ties, unions included.
func (g *generator) arbReachability(engine *astEngine) map[string]map[string]bool {
	edges := g.arbEdges(engine)
	reaches := make(map[string]map[string]bool, len(edges))
	for _, ti := range engine.types {
		reaches[ti.Name] = reachableFrom(edges[ti.Name], edges)
	}
	return reaches
}

// arbEdges maps each registered type to the types its arbitrary ties to.
func (g *generator) arbEdges(engine *astEngine) map[string][]string {
	edges := make(map[string][]string, len(engine.types))
	for _, ti := range engine.types {
		for i := range ti.Fields {
			edges[ti.Name] = append(edges[ti.Name], g.arbRefs(&ti.Fields[i])...)
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

// arbFieldExpr returns the arbitrary expression for one struct field, and a
// note to emit above it when the expression is opaque. Each draw satisfies
// the validator the field's decoder emits.
func (g *generator) arbFieldExpr(f *fieldInfo) (expr, note string) {
	if f.Kind == kindMapped {
		return arbMappedExpr(f.GoTypeName, f.TSType)
	}
	return g.arbElemValue(f), ""
}

// arbElemExpr returns the arbitrary for a slice element or map value,
// recursing through nested collections the way elemDecoderExpr does.
func (g *generator) arbElemExpr(elem *fieldInfo) string {
	if elem.nullElem() {
		return "fc.option(" + g.arbElemValue(elem) + ", { nil: null })"
	}
	return g.arbElemValue(elem)
}

// arbElemValue is the arbitrary of f's non-null values.
func (g *generator) arbElemValue(f *fieldInfo) string {
	switch f.Kind {
	case kindSlice:
		return "fc.array(" + g.arbElemExpr(f.Elem) + ", " + arbArrayConstraintsFor(f.Bounds) + ")"
	case kindMap:
		return "fc.dictionary(fc.string(), " + g.arbElemExpr(f.Elem) + ", " + arbRecordConstraints + ")"
	case kindMapped:
		expr, _ := arbMappedExpr(f.GoTypeName, f.TSType)
		return expr
	case kindStruct:
		return "tie(\"" + g.tsName(f.GoTypeName) + "\")"
	case kindEnum:
		return arbName(g.tsEnumName(f.GoTypeName))
	case kindBytes:
		return arbBase64
	case kindRaw:
		return arbJSON
	}
	if f.Bounds != nil {
		return arbBoundedPrim(f.TSType, f.Bounds)
	}
	return arbPrim(f.TSType)
}

// arbArrayConstraintsFor is an array's fc constraints, within b's Count when it
// declares one.
func arbArrayConstraintsFor(b *bounds) string {
	if b == nil || !b.hasCount {
		return arbArrayConstraints
	}
	hi := min(b.maxItems, max(b.minItems, 2))
	return fmt.Sprintf(`{ minLength: %d, maxLength: %d, size: "small", depthIdentifier: %q }`, b.minItems, hi, arbDepthIdentifier)
}

// maxSafeInteger is JavaScript's Number.MAX_SAFE_INTEGER, the widest bound
// fc.integer takes.
const maxSafeInteger = 1<<53 - 1

// arbBoundedPrim is the arbitrary of a primitive within b, so a drawn value
// passes the bound checks the decoder emits.
func arbBoundedPrim(tsType string, b *bounds) string {
	if tsType != tsString {
		if !b.hasRange {
			return arbPrim(tsType)
		}
		lo, hi := max(b.min, -maxSafeInteger), min(b.max, maxSafeInteger)
		return fmt.Sprintf("fc.integer({ min: %d, max: %d })", lo, hi)
	}
	expr := arbString
	switch {
	case b.pattern != "":
		expr = "fc.stringMatching(new globalThis.RegExp(" + jsString(b.pattern) + ", \"u\"))"
	case b.maxBytes > 0:
		expr = fmt.Sprintf("fc.string({ maxLength: %d })", b.maxBytes)
	}
	if b.maxBytes > 0 {
		expr = fmt.Sprintf("%s.filter((s) => new globalThis.TextEncoder().encode(s).length <= %d)", expr, b.maxBytes)
	}
	return expr
}

// arbMappedExpr renders a consumer-mapped type's arbitrary. A mapping to a
// primitive TS type is honoured exactly; anything else is opaque, because the
// mapping's shape lives in the consumer's TS expression and not in wiregen's
// parsed model — so the value is an arbitrary JSON one and the annotation is a
// cast, the same trust the emitted `o[k] as T` decoder already extends.
func arbMappedExpr(goTypeName, tsType string) (expr, note string) {
	switch tsType {
	case tsString, tsNumber, tsBoolean:
		return arbPrim(tsType), ""
	}
	return "fc.jsonValue().filter((v) => v !== null) as unknown as fc.Arbitrary<" + tsType + ">",
		"opaque: " + goTypeName + " is consumer-mapped, so its shape is not in wiregen's model"
}

// arbPrim is the arbitrary for a primitive TS type, mirroring primHelpers'
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
func (g *generator) emitArbitraryTable(w *strings.Builder, engine *astEngine) {
	rows := map[string]bool{}
	for _, ti := range engine.types {
		rows[g.tsName(ti.Name)] = true
	}
	for name := range g.enums {
		rows[g.tsEnumName(name)] = true
	}
	w.WriteString("export const ARBITRARY_BY_TYPE: globalThis.Record<string, fc.Arbitrary<unknown>> = {\n")
	for _, name := range slices.Sorted(maps.Keys(rows)) {
		w.WriteString("  " + name + ": " + arbName(name) + ",\n")
	}
	w.WriteString("};\n")
}

// --- events generation ---

// generateEvents writes the events module: the EventFrame union of every
// event's name and data, EVENT_DECODERS with one decoder per name, and
// decodeEventFrame, which decodes one frame and throws on an unknown name.
func (g *generator) generateEvents(w *strings.Builder) {
	w.WriteString(g.o.headerComment)
	decoders := map[string]bool{}
	types := map[string]bool{}
	for _, ev := range g.Events {
		decoders[g.decoderName(ev.Data.Name)] = true
		types[g.tsName(ev.Data.Name)] = true
	}
	w.WriteString("import { " + strings.Join(slices.Sorted(maps.Keys(decoders)), ", ") + " } from \"" + g.decodersSpec(g.o.eventsFilename) + "\";\n")
	w.WriteString("import type { " + strings.Join(slices.Sorted(maps.Keys(types)), ", ") + " } from \"" + g.typesSpec(g.o.eventsFilename) + "\";\n")
	w.WriteString("import type { Decoder } from \"" + g.validatorsSpec(g.o.eventsFilename) + "\";\n\n")

	w.WriteString("export type EventFrame =")
	for _, ev := range g.Events {
		w.WriteString("\n  | { readonly event: \"" + tsStringLiteral(ev.Name) + "\"; readonly data: " + g.tsName(ev.Data.Name) + " }")
	}
	w.WriteString(";\n\n")
	w.WriteString("export type EventName = EventFrame[\"event\"];\n\n")

	w.WriteString("export const EVENT_DECODERS: { readonly [K in EventName]: Decoder<globalThis.Extract<EventFrame, { readonly event: K }>[\"data\"]> } = {\n")
	for _, ev := range g.Events {
		w.WriteString("  " + tsPropName(ev.Name) + ": " + g.decoderName(ev.Data.Name) + ",\n")
	}
	w.WriteString("};\n\n")

	w.WriteString("export function decodeEventFrame(event: string, data: unknown): EventFrame {\n")
	w.WriteString("  switch (event) {\n")
	for _, ev := range g.Events {
		lit := "\"" + tsStringLiteral(ev.Name) + "\""
		w.WriteString("    case " + lit + ":\n      return { event: " + lit + ", data: " + g.decoderName(ev.Data.Name) + "(data) };\n")
	}
	w.WriteString("    default:\n      throw new globalThis.TypeError(`unknown event ${globalThis.JSON.stringify(event)}`);\n")
	w.WriteString("  }\n")
	w.WriteString("}\n")
}

// --- constants generation ---

// generateConstants relies on validateConstants having refused a TSName
// with no identifier character.
func (g *generator) generateConstants(w *strings.Builder) {
	w.WriteString(g.o.headerComment)
	for _, c := range g.Constants {
		fmt.Fprintf(w, "export const %s = %d;\n", sanitizeTSIdent(c.TSName), c.Value)
	}
}

// --- validators module generation (library-owned) ---

// generateValidators writes the validators module: the runtime helpers and
// the Decoder<T> alias the generated decoders import. Its content does not
// depend on the registry.
func (g *generator) generateValidators(w *strings.Builder) {
	w.WriteString(g.o.headerComment)
	w.WriteString(validatorsBody)
}

// validatorsBody is the validators module's TypeScript, the contract
// docs/generated-files.md lists.
const validatorsBody = `/** A decoder is a pure function that returns T or throws on shape mismatch. */
export type Decoder<T> = (v: unknown) => T;

function fail(path: string, msg: string): never {
  throw new globalThis.TypeError(` + "`${path}: ${msg}`" + `);
}

function typeName(v: unknown): string {
  if (v === null) {
    return "null";
  }
  if (globalThis.Array.isArray(v)) {
    return "array";
  }
  return typeof v;
}

/** Reads o's own property key: an inherited one (constructor, toString,
 *  __proto__) reads as absent, as JSON never sends it. */
export function own(o: globalThis.Record<string, unknown>, key: string): unknown {
  return globalThis.Object.hasOwn(o, key) ? o[key] : undefined;
}

/** Asserts v is a plain object (not array, not null). Returns the typed map. */
export function asObject(v: unknown, path = "$"): globalThis.Record<string, unknown> {
  if (typeof v !== "object" || v === null || globalThis.Array.isArray(v)) {
    fail(path, ` + "`expected object, got ${typeName(v)}`" + `);
  }
  return v as globalThis.Record<string, unknown>;
}

/** Asserts v is an array; returns it. */
export function asArray(v: unknown, path = "$"): unknown[] {
  if (!globalThis.Array.isArray(v)) {
    fail(path, ` + "`expected array, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Required string field; throws if absent or not a string. */
export function reqStr(o: globalThis.Record<string, unknown>, key: string, path = "$"): string {
  const v = own(o, key);
  if (typeof v !== "string") {
    fail(` + "`${path}.${key}`" + `, ` + "`expected string, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Required finite number field. NaN and Infinity are rejected. */
export function reqNum(o: globalThis.Record<string, unknown>, key: string, path = "$"): number {
  const v = own(o, key);
  if (typeof v !== "number" || !globalThis.Number.isFinite(v)) {
    fail(` + "`${path}.${key}`" + `, ` + "`expected number, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Required boolean field. */
export function reqBool(o: globalThis.Record<string, unknown>, key: string, path = "$"): boolean {
  const v = own(o, key);
  if (typeof v !== "boolean") {
    fail(` + "`${path}.${key}`" + `, ` + "`expected boolean, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Optional string: undefined if key absent, otherwise must be a string. */
export function optStr(o: globalThis.Record<string, unknown>, key: string, path = "$"): string | undefined {
  const v = own(o, key);
  if (v === undefined) {
    return undefined;
  }
  if (typeof v !== "string") {
    fail(` + "`${path}.${key}`" + `, ` + "`expected string or undefined, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Optional finite number. */
export function optNum(o: globalThis.Record<string, unknown>, key: string, path = "$"): number | undefined {
  const v = own(o, key);
  if (v === undefined) {
    return undefined;
  }
  if (typeof v !== "number" || !globalThis.Number.isFinite(v)) {
    fail(` + "`${path}.${key}`" + `, ` + "`expected number or undefined, got ${typeName(v)}`" + `);
  }
  return v;
}

/** Optional boolean. */
export function optBool(o: globalThis.Record<string, unknown>, key: string, path = "$"): boolean | undefined {
  const v = own(o, key);
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
  o: globalThis.Record<string, unknown>,
  key: string,
  vals: readonly T[],
  path = "$",
): T {
  const v = own(o, key);
  if (typeof v !== "string" || !(vals as readonly string[]).includes(v)) {
    fail(` + "`${path}.${key}`" + `, ` + "`expected one of ${vals.join(\"|\")}, got ${globalThis.JSON.stringify(v)}`" + `);
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
      if (e instanceof globalThis.TypeError) {
        throw new globalThis.TypeError(` + "`${path}[${globalThis.String(i)}]: ${e.message}`" + `, { cause: e });
      }
      throw e;
    }
  });
}

/** Decodes a Record<string, T> by iterating own keys and applying
 *  decode to each value. Error messages include the key. Every key,
 *  "__proto__" included, becomes an own property. */
export function decodeRecord<T>(v: unknown, decode: Decoder<T>, path = "$"): globalThis.Record<string, T> {
  const o = asObject(v, path);
  return globalThis.Object.fromEntries(
    globalThis.Object.entries(o).map(([k, val]): [string, T] => {
      try {
        return [k, decode(val)];
      } catch (e) {
        if (e instanceof globalThis.TypeError) {
          throw new globalThis.TypeError(` + "`${path}.${k}: ${e.message}`" + `, { cause: e });
        }
        throw e;
      }
    }),
  );
}

/** Refuses a present, non-null value at any of keys: a field another union
 *  case owns. */
export function refuseKeys(o: globalThis.Record<string, unknown>, keys: readonly string[], path = "$"): void {
  for (const k of keys) {
    const v = own(o, k);
    if (v !== undefined && v !== null) {
      fail(` + "`${path}.${k}`" + `, "not allowed in this case");
    }
  }
}

/** Checks v is an integer from lo to hi. */
export function checkRange(v: number, lo: number, hi: number, path = "$"): number {
  if (!globalThis.Number.isInteger(v) || v < lo || v > hi) {
    fail(path, ` + "`expected an integer from ${globalThis.String(lo)} to ${globalThis.String(hi)}, got ${globalThis.String(v)}`" + `);
  }
  return v;
}

const utf8 = new globalThis.TextEncoder();

/** Checks s is at most max bytes in UTF-8. */
export function checkBytes<T extends string>(s: T, max: number, path = "$"): T {
  const n = utf8.encode(s).length;
  if (n > max) {
    fail(path, ` + "`expected at most ${globalThis.String(max)} UTF-8 bytes, got ${globalThis.String(n)}`" + `);
  }
  return s;
}

const patterns = new globalThis.Map<string, globalThis.RegExp>();

/** Checks s matches the ECMA-262 pattern, unanchored as JSON Schema's
 *  pattern keyword is. */
export function checkPattern<T extends string>(s: T, pattern: string, path = "$"): T {
  let re = patterns.get(pattern);
  if (re === undefined) {
    re = new globalThis.RegExp(pattern, "u");
    patterns.set(pattern, re);
  }
  if (!re.test(s)) {
    fail(path, ` + "`expected a match of ${pattern}`" + `);
  }
  return s;
}

/** Checks a holds lo to hi items. */
export function checkCount<T>(a: T[], lo: number, hi: number, path = "$"): T[] {
  if (a.length < lo || a.length > hi) {
    fail(path, ` + "`expected ${globalThis.String(lo)} to ${globalThis.String(hi)} items, got ${globalThis.String(a.length)}`" + `);
  }
  return a;
}
`
