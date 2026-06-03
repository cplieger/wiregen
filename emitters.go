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

	// Enums (sorted by TS name)
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

	// Union types
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

	// Struct interfaces (sorted by TS name, skip unions)
	for _, ti := range engine.types {
		if ti.Union != nil {
			continue
		}
		if ti.Doc != "" {
			w.WriteString(ti.Doc)
		}
		w.WriteString("export interface " + r.tsName(ti.Name) + " {\n")
		for _, f := range ti.Fields {
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
		w.WriteString("}\n\n")
	}
}

// --- decoders generation ---

//nolint:gocyclo // large but flat
func (r *Registry) generateDecoders(w *strings.Builder, engine *astEngine) {
	if r.ValidatorsImport == "" {
		panic("wiregen: ValidatorsImport must be set")
	}

	var bodies strings.Builder

	// Emit decoders for structs (skip unions)
	for _, ti := range engine.types {
		if ti.Union != nil {
			continue
		}
		r.emitDecoder(&bodies, ti)
	}

	// Emit union decoders
	for _, ti := range engine.types {
		if ti.Union == nil {
			continue
		}
		r.emitUnionDecoder(&bodies, ti)
	}

	body := bodies.String()

	w.WriteString(r.HeaderComment)
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

	// Types import
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

	// Enum constants
	emitted := map[string]bool{}
	for _, name := range enumNamesSlice(r.Enums) {
		constN := r.enumConstName(name)
		if emitted[constN] {
			continue
		}
		if !isIdentReferenced(body, constN) {
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

	w.WriteString(body)
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
