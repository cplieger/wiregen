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
)

// --- types generation ---

func (r *Registry) generateTypes(w *strings.Builder, engine *astEngine) {
	w.WriteString(r.HeaderComment)
	r.emitEnumTypes(w)
	r.emitUnionTypes(w, engine)
	r.emitStructInterfaces(w, engine)
}

// emitEnumTypes writes the `export type X = "a" | "b";` string-union aliases,
// deduplicated and sorted by TS name.
func (r *Registry) emitEnumTypes(w *strings.Builder) {
	enumNames := make([]string, 0, len(r.Enums))
	seenEnumTS := map[string]bool{}
	for name := range r.Enums {
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
		w.WriteString("  " + f.WireName + "?: " + ts + ";\n")
	} else {
		w.WriteString("  " + f.WireName + ": " + ts + ";\n")
	}
}

// --- decoders generation ---

func (r *Registry) generateDecoders(w *strings.Builder, engine *astEngine) {
	if r.ValidatorsImport == "" {
		panic("wiregen: ValidatorsImport must be set")
	}
	body := r.decoderBodies(engine)

	w.WriteString(r.HeaderComment)
	r.emitHelperImports(w, body)
	r.emitTypeImports(w, body, engine)
	r.emitEnumConsts(w, body)
	w.WriteString(body)
}

// decoderBodies emits the struct decoders followed by the union decoders and
// returns the concatenated body (used to decide which imports/consts the
// header needs).
func (r *Registry) decoderBodies(engine *astEngine) string {
	var bodies strings.Builder
	for _, ti := range engine.types {
		if ti.Union == nil {
			r.emitDecoder(&bodies, ti)
		}
	}
	for _, ti := range engine.types {
		if ti.Union != nil {
			r.emitUnionDecoder(&bodies, ti)
		}
	}
	return bodies.String()
}

// emitHelperImports writes the validators-module import, listing only the
// contract helpers actually referenced by the decoder body.
func (r *Registry) emitHelperImports(w *strings.Builder, body string) {
	allHelpers := []string{
		"asObject", "asArray", "reqStr", "reqNum", "reqBool",
		"optStr", "optNum", "optBool", "reqOneOf",
		"decodeArray", "decodeRecord",
	}
	var usedHelpers []string
	for _, h := range allHelpers {
		if isIdentReferenced(body, h) {
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
// referenced by the decoder body, sorted; it emits nothing when none are used.
func (r *Registry) emitTypeImports(w *strings.Builder, body string, engine *astEngine) {
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
		if isIdentReferenced(body, n) {
			usedSet[n] = true
		}
	}
	used := make([]string, 0, len(usedSet))
	for n := range usedSet {
		used = append(used, n)
	}
	sort.Strings(used)
	if len(used) > 0 {
		w.WriteString("import type { ")
		w.WriteString(strings.Join(used, ", "))
		w.WriteString(" } from \"" + r.TypesImportPath + "\";\n")
	}
	w.WriteString("\n")
}

// emitEnumConsts writes the `const XS = [...] as const;` value arrays for the
// enums referenced by the decoder body (deduped), then a trailing blank line.
func (r *Registry) emitEnumConsts(w *strings.Builder, body string) {
	emitted := map[string]bool{}
	for _, name := range enumNamesSlice(r.Enums) {
		constN := r.enumConstName(name)
		if emitted[constN] || !isIdentReferenced(body, constN) {
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

func (r *Registry) emitDecoder(w *strings.Builder, ti *typeInfo) {
	tn := r.tsName(ti.Name)
	path := "$." + r.pathName(tn)
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
			w.WriteString("    " + f.WireName + ": " + r.reqExpr(&f, path) + ",\n")
		}
		w.WriteString("  };\n")
	} else {
		w.WriteString("  const out: " + tn + " = {};\n")
	}

	for _, f := range optFields {
		r.emitOptionalField(w, &f, path)
	}

	w.WriteString("  return out;\n")
	w.WriteString("};\n\n")
}

func (r *Registry) emitUnionDecoder(w *strings.Builder, ti *typeInfo) {
	tn := r.tsName(ti.Name)
	dm := r.DiscriminatorMap[ti.Name]
	if dm == nil {
		return // No discriminator map → only type alias emitted
	}

	disc := ti.Union.Discriminator
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
		w.WriteString("    case \"" + k + "\": return " + r.decoderName(variant) + "(v);\n")
	}
	w.WriteString("    default: throw new TypeError(`unknown " + tn + " variant: ${" + disc + "}`);\n")
	w.WriteString("  }\n")
	w.WriteString("};\n\n")
}

func (r *Registry) reqExpr(f *fieldInfo, path string) string {
	if f.JSONString {
		return "reqStr(o, \"" + f.WireName + "\", \"" + path + "\")"
	}
	if f.IsRaw || f.IsIface {
		return "o[\"" + f.WireName + "\"] as unknown"
	}

	// Custom decoder mapping
	if expr, ok := r.DecoderMappings[f.GoTypeName]; ok {
		return expr + "(o, \"" + f.WireName + "\", \"" + path + "\")"
	}
	// Custom type mapping without decoder
	if _, ok := r.TypeMappings[f.GoTypeName]; ok {
		return "o[\"" + f.WireName + "\"] as " + f.TSType
	}

	if f.IsEnum {
		return "reqOneOf(o, \"" + f.WireName + "\", " + r.enumConstName(f.GoTypeName) + ", \"" + path + "\")"
	}
	if f.IsStruct {
		return r.decoderName(f.GoTypeName) + "(o[\"" + f.WireName + "\"])"
	}
	if f.IsSlice {
		return "decodeArray(o[\"" + f.WireName + "\"], " + r.elemDecoderExpr(f) + ", \"" + path + "." + f.WireName + "\")"
	}
	if f.IsMap {
		return "decodeRecord(o[\"" + f.WireName + "\"], " + r.mapValDecoderExpr(f) + ", \"" + path + "." + f.WireName + "\")"
	}

	// Unresolved type (e.g. an unregistered nested struct) — pass through as
	// unknown rather than mis-decoding it as a number.
	if f.TSType == tsUnknown {
		return "o[\"" + f.WireName + "\"] as unknown"
	}

	// Primitive
	return primHelperAST(f.TSType, false) + "(o, \"" + f.WireName + "\", \"" + path + "\")"
}

func (r *Registry) emitOptionalField(w *strings.Builder, f *fieldInfo, path string) {
	if f.JSONString {
		varName := sanitizeVarName(f.WireName)
		w.WriteString("  const " + varName + " = optStr(o, \"" + f.WireName + "\", \"" + path + "\");\n")
		w.WriteString("  if (" + varName + " !== undefined) out." + f.WireName + " = " + varName + ";\n")
		return
	}
	if f.IsRaw || f.IsIface {
		w.WriteString("  if (o[\"" + f.WireName + "\"] !== undefined) out." + f.WireName + " = o[\"" + f.WireName + "\"] as unknown;\n")
		return
	}
	if expr, ok := r.DecoderMappings[f.GoTypeName]; ok {
		varName := sanitizeVarName(f.WireName)
		w.WriteString("  const " + varName + " = " + expr + "(o, \"" + f.WireName + "\", \"" + path + "\");\n")
		w.WriteString("  if (" + varName + " !== undefined) out." + f.WireName + " = " + varName + ";\n")
		return
	}
	if _, ok := r.TypeMappings[f.GoTypeName]; ok {
		w.WriteString("  if (o[\"" + f.WireName + "\"] !== undefined) out." + f.WireName + " = o[\"" + f.WireName + "\"] as " + f.TSType + ";\n")
		return
	}
	if f.IsEnum {
		w.WriteString("  if (o[\"" + f.WireName + "\"] !== undefined) out." + f.WireName + " = reqOneOf(o, \"" + f.WireName + "\", " + r.enumConstName(f.GoTypeName) + ", \"" + path + "\");\n")
		return
	}
	if f.IsStruct {
		w.WriteString("  if (o[\"" + f.WireName + "\"] !== undefined) out." + f.WireName + " = " + r.decoderName(f.GoTypeName) + "(o[\"" + f.WireName + "\"]);\n")
		return
	}
	if f.IsSlice {
		// []byte already handled as string in TSType
		if f.TSType == tsString {
			varName := sanitizeVarName(f.WireName)
			w.WriteString("  const " + varName + " = optStr(o, \"" + f.WireName + "\", \"" + path + "\");\n")
			w.WriteString("  if (" + varName + " !== undefined) out." + f.WireName + " = " + varName + ";\n")
			return
		}
		w.WriteString("  if (o[\"" + f.WireName + "\"] !== undefined) out." + f.WireName + " = decodeArray(o[\"" + f.WireName + "\"], " + r.elemDecoderExpr(f) + ", \"" + path + "." + f.WireName + "\");\n")
		return
	}
	if f.IsMap {
		w.WriteString("  if (o[\"" + f.WireName + "\"] !== undefined) out." + f.WireName + " = decodeRecord(o[\"" + f.WireName + "\"], " + r.mapValDecoderExpr(f) + ", \"" + path + "." + f.WireName + "\");\n")
		return
	}

	// Unresolved type — pass through as unknown rather than optNum.
	if f.TSType == tsUnknown {
		w.WriteString("  if (o[\"" + f.WireName + "\"] !== undefined) out." + f.WireName + " = o[\"" + f.WireName + "\"] as unknown;\n")
		return
	}

	// Primitive optional
	helper := primHelperAST(f.TSType, true)
	varName := sanitizeVarName(f.WireName)
	w.WriteString("  const " + varName + " = " + helper + "(o, \"" + f.WireName + "\", \"" + path + "\");\n")
	w.WriteString("  if (" + varName + " !== undefined) out." + f.WireName + " = " + varName + ";\n")
}

func (r *Registry) elemDecoderExpr(f *fieldInfo) string {
	elemType := f.SliceElem
	goTypeName := f.GoTypeName

	// Check DecoderMappings
	if expr, ok := r.DecoderMappings[goTypeName]; ok {
		return "(v) => " + expr + "({v} as Record<string, unknown>, \"v\", \"elem\")"
	}
	// Check TypeMappings
	if mapped, ok := r.TypeMappings[goTypeName]; ok {
		return "(v) => v as " + mapped
	}
	// Check if elem is a registered struct
	if r.typeNames[goTypeName] {
		return r.decoderName(goTypeName)
	}
	// Check if elem is an enum (use GoTypeName which is the Go type name)
	if _, ok := r.Enums[goTypeName]; ok {
		constName := r.enumConstName(goTypeName)
		return "(v) => { const s = v as string; if (!" + constName + ".includes(s as never)) throw new TypeError(\"invalid enum value: \" + s); return s as " + r.tsEnumName(goTypeName) + "; }"
	}

	switch elemType {
	case tsString:
		return "(v) => { if (typeof v !== \"string\") throw new TypeError(\"expected string\"); return v as string; }"
	case "number":
		return "(v) => { if (typeof v !== \"number\") throw new TypeError(\"expected number\"); return v as number; }"
	case tsBoolean:
		return "(v) => { if (typeof v !== \"boolean\") throw new TypeError(\"expected boolean\"); return v as boolean; }"
	}

	return tsIdentityCast
}

func (r *Registry) mapValDecoderExpr(f *fieldInfo) string {
	return r.elemDecoderExpr(&fieldInfo{
		SliceElem:  f.MapVal,
		GoTypeName: f.GoTypeName,
	})
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

func (r *Registry) generateRegistry(w *strings.Builder) {
	if !r.SelfContainedRegistry && r.BusImport == "" {
		panic("wiregen: BusImport must be set when SelfContainedRegistry is false")
	}
	if r.SelfContainedRegistry && r.ValidatorsImport == "" {
		panic("wiregen: ValidatorsImport must be set when SelfContainedRegistry is true")
	}
	w.WriteString(r.HeaderComment)

	decoderImports := make([]string, 0)
	seen := map[string]bool{}
	for _, e := range r.SSEEvents {
		dn := r.decoderName(e.TypeName)
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
			w.WriteString("  registry.set(\"" + tsStringLiteral(e.EventType) + "\", " + r.decoderName(e.TypeName) + " as Decoder<unknown>);\n")
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
			w.WriteString("  " + r.RegisterFuncName + "(\"" + tsStringLiteral(e.EventType) + "\", " + r.decoderName(e.TypeName) + ");\n")
		}
		w.WriteString("}\n")
	}
}

// --- constants generation ---

func (r *Registry) generateConstants(w *strings.Builder) {
	w.WriteString(r.HeaderComment)
	for _, c := range r.Constants {
		name := sanitizeTSIdent(c.TSName)
		if name == "" {
			continue
		}
		fmt.Fprintf(w, "export const %s = %d;\n", name, c.Value)
	}
}

// --- validators starter generation (opt-in) ---

// validatorsStarterBanner heads the opt-in validators starter module. It is
// deliberately NOT r.HeaderComment: this file is a one-time scaffold a new
// consumer copies once and then OWNS — it is never regenerated and must never
// carry a "DO NOT EDIT" / "CODE-GENERATED" banner, or a consumer's hand-edited
// copy would look machine-managed.
const validatorsStarterBanner = `// wiregen validators starter — copy this file ONCE into your consumer, then
// OWN it: edit it freely. It is a scaffold, NOT a generated artifact, and
// wiregen will never regenerate or overwrite it.
//
// Runtime decode helpers — the primitives the generated decoders in
// ./wire/decoders.gen.ts (and any hand-rolled decoder) import by name. The
// wire-format decoders themselves ARE generated from your Go structs at build
// time (see your cmd/wire-codegen driver); edit the Go side and re-run the
// generator to update them. This validators module is the only hand-authored
// part of the wire validation system — keep the exported names and signatures
// below stable so the generated decoders keep compiling against it.

`

// generateValidators writes the opt-in validators starter module: the full
// set of 11 runtime helper functions plus the Decoder<T> type alias that the
// generated decoders import. The content is constant (it does not depend on
// the registered types) — it is the reference implementation of the
// "Validators contract". A consumer calls GenerateValidators() once to scaffold
// their own copy, then owns and edits it. It is never part of Generate's
// default writes.
func (r *Registry) generateValidators(w *strings.Builder) {
	w.WriteString(validatorsStarterBanner)
	w.WriteString(validatorsStarterBody)
}

// validatorsStarterBody is the working TypeScript implementation of the
// validators contract: asObject, asArray, reqStr/reqNum/reqBool,
// optStr/optNum/optBool, reqOneOf, decodeArray, decodeRecord (11 functions)
// plus `export type Decoder<T> = (v: unknown) => T`.
const validatorsStarterBody = `/** A decoder is a pure function that returns T or throws on shape mismatch. */
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
