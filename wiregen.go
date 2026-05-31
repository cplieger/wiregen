// Package wiregen generates TypeScript interfaces, decoders, and an SSE
// registry from Go struct types using reflect. It is a generic engine:
// consumers register their own types and invoke Generate to emit TS files.
package wiregen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	tsUnknown      = "unknown"
	tsIdentityCast = "(v) => v as unknown"
)

// EnumDef defines a named string enum with its valid values.
type EnumDef struct{ Values []string }

// SSERegEntry maps an SSE event type to a registered struct name.
type SSERegEntry struct {
	EventType string
	TypeName  string
}

// Registry holds all type registrations for code generation.
type Registry struct {
	// WireTypes is the set of Go struct types to generate TS for.
	WireTypes []reflect.Type
	// Enums maps Go type names to their enum values.
	Enums map[string]EnumDef
	// EnumTSName maps Go enum type names to preferred TS names (for aliasing).
	EnumTSName map[string]string
	// TSNameOverride maps Go type names to preferred TS interface names.
	TSNameOverride map[string]string
	// PathNameOverride overrides the automatic snake_case path for specific types.
	PathNameOverride map[string]string
	// SSEEvents is the set of SSE events to register decoders for.
	SSEEvents []SSERegEntry
	// ValidatorsImport is the import path for the validators module (default: "../validators.js").
	ValidatorsImport string
	// BusImport is the import path for the bus module with registerSSEDecoder (default: "../bus.js").
	BusImport string

	// internal index built on first use
	typeByName map[string]reflect.Type
}

func (r *Registry) init() {
	if r.typeByName != nil {
		return
	}
	r.typeByName = make(map[string]reflect.Type, len(r.WireTypes))
	for _, t := range r.WireTypes {
		r.typeByName[t.Name()] = t
	}
	if r.Enums == nil {
		r.Enums = map[string]EnumDef{}
	}
	if r.EnumTSName == nil {
		r.EnumTSName = map[string]string{}
	}
	if r.TSNameOverride == nil {
		r.TSNameOverride = map[string]string{}
	}
	if r.PathNameOverride == nil {
		r.PathNameOverride = map[string]string{}
	}
	if r.ValidatorsImport == "" {
		r.ValidatorsImport = "../validators.js"
	}
	if r.BusImport == "" {
		r.BusImport = "../bus.js"
	}
}

// Generate writes types.gen.ts, decoders.gen.ts, and registry.gen.ts to outDir.
func (r *Registry) Generate(outDir string) error {
	r.init()
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}
	var typesBuf strings.Builder
	r.generateTypes(&typesBuf)
	if err := os.WriteFile(filepath.Join(outDir, "types.gen.ts"), []byte(typesBuf.String()), 0o644); err != nil {
		return fmt.Errorf("write types.gen.ts: %w", err)
	}
	var decodersBuf strings.Builder
	r.generateDecoders(&decodersBuf)
	if err := os.WriteFile(filepath.Join(outDir, "decoders.gen.ts"), []byte(decodersBuf.String()), 0o644); err != nil {
		return fmt.Errorf("write decoders.gen.ts: %w", err)
	}
	var registryBuf strings.Builder
	r.generateRegistry(&registryBuf)
	if err := os.WriteFile(filepath.Join(outDir, "registry.gen.ts"), []byte(registryBuf.String()), 0o644); err != nil {
		return fmt.Errorf("write registry.gen.ts: %w", err)
	}
	return nil
}

// GenerateTypes returns the types.gen.ts content as a string.
func (r *Registry) GenerateTypes() string {
	r.init()
	var b strings.Builder
	r.generateTypes(&b)
	return b.String()
}

// GenerateDecoders returns the decoders.gen.ts content as a string.
func (r *Registry) GenerateDecoders() string {
	r.init()
	var b strings.Builder
	r.generateDecoders(&b)
	return b.String()
}

// GenerateRegistry returns the registry.gen.ts content as a string.
func (r *Registry) GenerateRegistry() string {
	r.init()
	var b strings.Builder
	r.generateRegistry(&b)
	return b.String()
}

// --- internal helpers ---

func (r *Registry) tsName(goName string) string {
	if override, ok := r.TSNameOverride[goName]; ok {
		return override
	}
	return goName
}

func (r *Registry) tsEnumName(goName string) string {
	if override, ok := r.EnumTSName[goName]; ok {
		return override
	}
	return goName
}

func (r *Registry) decoderName(typeName string) string {
	return "decode" + r.tsName(typeName)
}

// fieldInfo holds parsed metadata for one struct field.
type fieldInfo struct {
	goType   reflect.Type
	wireName string
	optional bool
}

func (r *Registry) parseFields(t reflect.Type) []fieldInfo {
	var fields []fieldInfo
	for i := range t.NumField() {
		sf := t.Field(i)
		if sf.Anonymous {
			embedded := sf.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			fields = append(fields, r.parseFields(embedded)...)
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		wireName := parts[0]
		if wireName == "" {
			wireName = sf.Name
		}
		if wireName == "-" {
			continue
		}
		omitempty := false
		for _, p := range parts[1:] {
			if p == "omitempty" {
				omitempty = true
			}
		}
		// Pointers and maps are always optional; omitempty makes any field optional.
		optional := omitempty || sf.Type.Kind() == reflect.Pointer || sf.Type.Kind() == reflect.Map
		fields = append(fields, fieldInfo{wireName: wireName, goType: sf.Type, optional: optional})
	}
	return fields
}

func (r *Registry) tsType(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		return r.tsType(t.Elem())
	}
	if t.Name() != "" {
		if _, ok := r.Enums[t.Name()]; ok {
			return r.tsEnumName(t.Name())
		}
		if _, ok := r.typeByName[t.Name()]; ok {
			return r.tsName(t.Name())
		}
	}
	if t == reflect.TypeFor[json.RawMessage]() {
		return tsUnknown
	}
	if t == reflect.TypeFor[time.Time]() {
		return "string"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		return r.tsType(t.Elem()) + "[]"
	case reflect.Map:
		return "Record<string, " + r.tsType(t.Elem()) + ">"
	case reflect.Interface:
		return tsUnknown
	case reflect.Struct:
		return tsUnknown
	}
	return tsUnknown
}

func (r *Registry) pathName(typeName string) string {
	if override, ok := r.PathNameOverride[typeName]; ok {
		return override
	}
	var b strings.Builder
	runes := []rune(typeName)
	for i, ru := range runes {
		if ru >= 'A' && ru <= 'Z' {
			if i > 0 {
				prev := runes[i-1]
				if prev >= 'a' && prev <= 'z' {
					b.WriteByte('_')
				} else if prev >= 'A' && prev <= 'Z' && i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z' {
					b.WriteByte('_')
				}
			}
			b.WriteRune(ru + 32)
		} else {
			b.WriteRune(ru)
		}
	}
	return b.String()
}

func (r *Registry) enumConstName(goTypeName string) string {
	name := r.tsEnumName(goTypeName)
	var b strings.Builder
	runes := []rune(name)
	for i, ru := range runes {
		if ru >= 'A' && ru <= 'Z' {
			if i > 0 {
				prev := runes[i-1]
				if prev >= 'a' && prev <= 'z' {
					b.WriteByte('_')
				} else if prev >= 'A' && prev <= 'Z' && i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z' {
					b.WriteByte('_')
				}
			}
			b.WriteRune(ru)
		} else {
			b.WriteRune(ru - 32)
		}
	}
	b.WriteString("S")
	return b.String()
}

func (r *Registry) isPrimitive(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		return r.isPrimitive(t.Elem())
	}
	if t == reflect.TypeFor[time.Time]() {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func (r *Registry) isEnum(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		return r.isEnum(t.Elem())
	}
	_, ok := r.Enums[t.Name()]
	return ok && t.Kind() == reflect.String
}

func (r *Registry) isStruct(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		return r.isStruct(t.Elem())
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	_, ok := r.typeByName[t.Name()]
	return ok
}

func isRawMessage(t reflect.Type) bool {
	return t == reflect.TypeFor[json.RawMessage]()
}

func isInterface(t reflect.Type) bool {
	return t.Kind() == reflect.Interface
}

func primHelper(t reflect.Type, optional bool) string {
	if t.Kind() == reflect.Pointer {
		return primHelper(t.Elem(), optional)
	}
	if t == reflect.TypeFor[time.Time]() {
		if optional {
			return "optStr"
		}
		return "reqStr"
	}
	prefix := "req"
	if optional {
		prefix = "opt"
	}
	switch t.Kind() {
	case reflect.String:
		return prefix + "Str"
	case reflect.Bool:
		return prefix + "Bool"
	default:
		return prefix + "Num"
	}
}

func (r *Registry) elemDecoderExpr(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if r.isStruct(t) {
		return r.decoderName(t.Name())
	}
	if t.Kind() == reflect.String {
		return "(v) => { if (typeof v !== \"string\") throw new TypeError(\"expected string\"); return v as string; }"
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "(v) => { if (typeof v !== \"number\") throw new TypeError(\"expected number\"); return v as number; }"
	case reflect.Bool:
		return "(v) => { if (typeof v !== \"boolean\") throw new TypeError(\"expected boolean\"); return v as boolean; }"
	}
	if t.Kind() == reflect.Interface {
		return tsIdentityCast
	}
	if isRawMessage(t) {
		return tsIdentityCast
	}
	if t.Kind() == reflect.Map {
		return "(v) => asObject(v)"
	}
	if t.Name() != "" && r.isEnum(t) {
		return r.decoderName(t.Name())
	}
	return tsIdentityCast
}

// --- generation ---

func (r *Registry) generateTypes(w *strings.Builder) {
	w.WriteString("// CODE-GENERATED by wiregen, DO NOT EDIT.\n\n")

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
			w.WriteString("\"" + v + "\"")
		}
		w.WriteString(";\n\n")
	}

	names := make([]string, 0, len(r.WireTypes))
	for _, t := range r.WireTypes {
		names = append(names, t.Name())
	}
	sort.Slice(names, func(i, j int) bool { return r.tsName(names[i]) < r.tsName(names[j]) })
	for _, name := range names {
		t := r.typeByName[name]
		fields := r.parseFields(t)
		w.WriteString("export interface " + r.tsName(name) + " {\n")
		for _, f := range fields {
			ts := r.tsType(f.goType)
			if f.optional {
				w.WriteString("  " + f.wireName + "?: " + ts + ";\n")
			} else {
				w.WriteString("  " + f.wireName + ": " + ts + ";\n")
			}
		}
		w.WriteString("}\n\n")
	}
}

func (r *Registry) generateDecoders(w *strings.Builder) {
	var bodies strings.Builder
	goNames := make([]string, 0, len(r.WireTypes))
	for _, t := range r.WireTypes {
		goNames = append(goNames, t.Name())
	}
	sort.Slice(goNames, func(i, j int) bool { return r.tsName(goNames[i]) < r.tsName(goNames[j]) })
	for _, name := range goNames {
		t := r.typeByName[name]
		r.emitDecoder(&bodies, name, t)
	}
	body := bodies.String()

	w.WriteString("// CODE-GENERATED by wiregen, DO NOT EDIT.\n\n")
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
	w.WriteString("type Decoder } from \"" + r.ValidatorsImport + "\";\n")

	candidateNames := make([]string, 0, len(r.WireTypes))
	for _, t := range r.WireTypes {
		candidateNames = append(candidateNames, r.tsName(t.Name()))
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
		w.WriteString(" } from \"./types.gen.js\";\n")
	}
	w.WriteString("\n")

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
			w.WriteString("\"" + v + "\"")
		}
		w.WriteString("] as const;\n")
	}
	if len(emitted) > 0 {
		w.WriteString("\n")
	}

	w.WriteString(body)
}

func (r *Registry) generateRegistry(w *strings.Builder) {
	w.WriteString("// CODE-GENERATED by wiregen, DO NOT EDIT.\n\n")
	w.WriteString("import { registerSSEDecoder } from \"" + r.BusImport + "\";\n")

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
	w.WriteString("import { " + strings.Join(decoderImports, ", ") + " } from \"./decoders.gen.js\";\n\n")

	w.WriteString("export function registerAllSSEDecoders(): void {\n")
	for _, e := range r.SSEEvents {
		w.WriteString("  registerSSEDecoder(\"" + e.EventType + "\", " + r.decoderName(e.TypeName) + ");\n")
	}
	w.WriteString("}\n")
}

func (r *Registry) emitDecoder(w *strings.Builder, name string, t reflect.Type) {
	fields := r.parseFields(t)
	tn := r.tsName(name)
	path := "$." + r.pathName(tn)
	w.WriteString("export const " + r.decoderName(name) + ": Decoder<" + tn + "> = (v) => {\n")
	w.WriteString("  const o = asObject(v, \"" + path + "\");\n")

	var reqFields, optFields []fieldInfo
	for _, f := range fields {
		if f.optional {
			optFields = append(optFields, f)
		} else {
			reqFields = append(reqFields, f)
		}
	}

	if len(reqFields) > 0 || len(optFields) > 0 {
		w.WriteString("  const out: " + tn + " = {\n")
		for _, f := range reqFields {
			w.WriteString("    " + f.wireName + ": " + r.reqExpr(f, path) + ",\n")
		}
		w.WriteString("  };\n")
	} else {
		w.WriteString("  const out: " + tn + " = {};\n")
	}

	for _, f := range optFields {
		r.emitOptionalField(w, f, path)
	}

	w.WriteString("  return out;\n")
	w.WriteString("};\n\n")
}

func (r *Registry) reqExpr(f fieldInfo, path string) string {
	t := f.goType
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if isRawMessage(t) {
		return "o[\"" + f.wireName + "\"] as unknown"
	}
	if isInterface(t) {
		return "o[\"" + f.wireName + "\"] as unknown"
	}
	if r.isEnum(t) {
		return "reqOneOf(o, \"" + f.wireName + "\", " + r.enumConstName(t.Name()) + ", \"" + path + "\")"
	}
	if r.isPrimitive(t) {
		return primHelper(t, false) + "(o, \"" + f.wireName + "\", \"" + path + "\")"
	}
	if r.isStruct(t) {
		return r.decoderName(t.Name()) + "(o[\"" + f.wireName + "\"])"
	}
	if t.Kind() == reflect.Slice {
		elem := t.Elem()
		if elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		return "decodeArray(o[\"" + f.wireName + "\"], " + r.elemDecoderExpr(elem) + ", \"" + path + "." + f.wireName + "\")"
	}
	if t.Kind() == reflect.Map {
		valType := t.Elem()
		return "decodeRecord(o[\"" + f.wireName + "\"], " + r.elemDecoderExpr(valType) + ", \"" + path + "." + f.wireName + "\")"
	}
	return "o[\"" + f.wireName + "\"] as unknown"
}

func (r *Registry) emitOptionalField(w *strings.Builder, f fieldInfo, path string) {
	t := f.goType
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if isRawMessage(t) {
		w.WriteString("  if (o[\"" + f.wireName + "\"] !== undefined) out." + f.wireName + " = o[\"" + f.wireName + "\"] as unknown;\n")
		return
	}
	if isInterface(t) {
		w.WriteString("  if (o[\"" + f.wireName + "\"] !== undefined) out." + f.wireName + " = o[\"" + f.wireName + "\"] as unknown;\n")
		return
	}
	if r.isEnum(t) {
		w.WriteString("  if (o[\"" + f.wireName + "\"] !== undefined) out." + f.wireName + " = reqOneOf(o, \"" + f.wireName + "\", " + r.enumConstName(t.Name()) + ", \"" + path + "\");\n")
		return
	}
	if r.isPrimitive(t) {
		helper := primHelper(t, true)
		varName := sanitizeVarName(f.wireName)
		w.WriteString("  const " + varName + " = " + helper + "(o, \"" + f.wireName + "\", \"" + path + "\");\n")
		w.WriteString("  if (" + varName + " !== undefined) out." + f.wireName + " = " + varName + ";\n")
		return
	}
	if r.isStruct(t) {
		w.WriteString("  if (o[\"" + f.wireName + "\"] !== undefined) out." + f.wireName + " = " + r.decoderName(t.Name()) + "(o[\"" + f.wireName + "\"]);\n")
		return
	}
	if t.Kind() == reflect.Slice {
		elem := t.Elem()
		if elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		w.WriteString("  if (o[\"" + f.wireName + "\"] !== undefined) out." + f.wireName + " = decodeArray(o[\"" + f.wireName + "\"], " + r.elemDecoderExpr(elem) + ", \"" + path + "." + f.wireName + "\");\n")
		return
	}
	if t.Kind() == reflect.Map {
		valType := t.Elem()
		w.WriteString("  if (o[\"" + f.wireName + "\"] !== undefined) out." + f.wireName + " = decodeRecord(o[\"" + f.wireName + "\"], " + r.elemDecoderExpr(valType) + ", \"" + path + "." + f.wireName + "\");\n")
		return
	}
	w.WriteString("  if (o[\"" + f.wireName + "\"] !== undefined) out." + f.wireName + " = o[\"" + f.wireName + "\"] as unknown;\n")
}

func sanitizeVarName(wireName string) string {
	parts := strings.Split(wireName, "_")
	var b strings.Builder
	for i, p := range parts {
		if i == 0 {
			b.WriteString(p)
		} else if p != "" {
			b.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	s := b.String()
	switch s {
	case "o", "out", "v", "private", "public", "protected", "class",
		"return", "delete", "default", "export", "import", "new", "this":
		return s + "Val"
	}
	return s
}

func isIdentReferenced(body, ident string) bool {
	for i := 0; i < len(body); {
		j := strings.Index(body[i:], ident)
		if j < 0 {
			return false
		}
		j += i
		if j > 0 {
			c := body[j-1]
			if isIdentChar(c) {
				i = j + len(ident)
				continue
			}
		}
		end := j + len(ident)
		if end < len(body) {
			c := body[end]
			if isIdentChar(c) {
				i = end
				continue
			}
		}
		return true
	}
	return false
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$'
}

func enumNamesSlice(m map[string]EnumDef) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
