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
	tsString       = "string"
)

// EnumDef defines a named string enum with its valid values.
type EnumDef struct{ Values []string }

// SSERegEntry maps an SSE event type to a registered struct name.
type SSERegEntry struct {
	EventType string
	TypeName  string
}

// WireConst defines a named integer constant to emit into TypeScript.
type WireConst struct {
	TSName string
	Value  int
}

// Option configures optional behavior knobs on a [Registry].
type Option func(*options)

// options holds optional configuration applied via functional options.
type options struct {
	validatorsImport      string
	busImport             string
	typesImportPath       string
	headerComment         string
	registerFuncName      string
	registryFuncName      string
	typesFilename         string
	decodersFilename      string
	registryFilename      string
	constantsFilename     string
	selfContainedRegistry bool
}

// WithValidatorsImport sets the import path for the validators module
// used in generated decoders (e.g. "./validators.js").
func WithValidatorsImport(v string) Option { return func(o *options) { o.validatorsImport = v } }

// WithBusImport sets the import path for the SSE bus module
// used in the generated registry (e.g. "./bus.js").
func WithBusImport(v string) Option { return func(o *options) { o.busImport = v } }

// WithTypesImportPath sets the import path used in decoders to reference the
// generated types file (default "./types.gen.js").
func WithTypesImportPath(v string) Option { return func(o *options) { o.typesImportPath = v } }

// WithHeaderComment sets the header comment prepended to every generated file.
func WithHeaderComment(v string) Option { return func(o *options) { o.headerComment = v } }

// WithRegisterFuncName sets the function name imported from the bus module to
// register individual SSE decoders (default "registerSSEDecoder").
func WithRegisterFuncName(v string) Option { return func(o *options) { o.registerFuncName = v } }

// WithRegistryFuncName sets the exported function name in the generated registry
// file that registers all SSE decoders (default "registerAllSSEDecoders").
func WithRegistryFuncName(v string) Option { return func(o *options) { o.registryFuncName = v } }

// WithSelfContainedRegistry enables self-contained registry mode where a local
// Map is used instead of importing a bus module.
func WithSelfContainedRegistry(v bool) Option {
	return func(o *options) { o.selfContainedRegistry = v }
}

// WithFilenames overrides the output filenames for the four generated files.
// Pass empty strings to keep defaults.
func WithFilenames(types, decoders, registry, constants string) Option {
	return func(o *options) {
		if types != "" {
			o.typesFilename = types
		}
		if decoders != "" {
			o.decodersFilename = decoders
		}
		if registry != "" {
			o.registryFilename = registry
		}
		if constants != "" {
			o.constantsFilename = constants
		}
	}
}

// Registry holds all type registrations for code generation.
//
// Payload data — the types, enums, constants, and mappings that describe what to
// generate — is registered via exported fields. Use [NewRegistry] to create a
// Registry with optional behavior knobs configured via functional [Option] values.
// A zero-value Registry is also valid and applies sensible defaults on first use.
type Registry struct {
	// Enums maps Go type names to their string enum definitions.
	Enums map[string]EnumDef
	// EnumTSName overrides the TypeScript name for a Go enum type.
	EnumTSName map[string]string
	// TSNameOverride overrides the TypeScript interface name for a Go struct type.
	TSNameOverride map[string]string
	// PathNameOverride overrides the decoder path segment for a Go struct type.
	PathNameOverride map[string]string
	// TypeMappings maps Go types to custom TypeScript type strings.
	TypeMappings map[reflect.Type]string
	// DecoderMappings maps Go types to custom decoder expression strings.
	DecoderMappings map[reflect.Type]string
	typeByName      map[string]reflect.Type
	// ValidatorsImport is the import path for the validators module.
	ValidatorsImport string
	// BusImport is the import path for the SSE bus module.
	BusImport string
	// TypesImportPath is the import path for the generated types file in decoders.
	TypesImportPath string
	// HeaderComment is prepended to every generated file.
	HeaderComment string
	// RegisterFuncName is the function name imported from the bus module.
	RegisterFuncName string
	// RegistryFuncName is the exported function name in the registry file.
	RegistryFuncName string
	// TypesFilename is the output filename for types (default "types.gen.ts").
	TypesFilename string
	// DecodersFilename is the output filename for decoders (default "decoders.gen.ts").
	DecodersFilename string
	// RegistryFilename is the output filename for the registry (default "registry.gen.ts").
	RegistryFilename string
	// ConstantsFilename is the output filename for constants (default "constants.gen.ts").
	ConstantsFilename string
	// WireTypes lists the Go struct types to generate TypeScript interfaces and decoders for.
	WireTypes []reflect.Type
	// SSEEvents maps SSE event type strings to registered struct names for registry generation.
	SSEEvents []SSERegEntry
	// Constants lists integer constants to emit as TypeScript exports.
	Constants []WireConst
	// SelfContainedRegistry enables self-contained registry mode.
	SelfContainedRegistry bool
}

// NewRegistry creates a [Registry] with the given functional options applied.
// Defaults are applied for any option not explicitly set.
func NewRegistry(opts ...Option) *Registry {
	var o options
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	r := &Registry{
		ValidatorsImport:      o.validatorsImport,
		BusImport:             o.busImport,
		TypesImportPath:       o.typesImportPath,
		HeaderComment:         o.headerComment,
		RegisterFuncName:      o.registerFuncName,
		RegistryFuncName:      o.registryFuncName,
		TypesFilename:         o.typesFilename,
		DecodersFilename:      o.decodersFilename,
		RegistryFilename:      o.registryFilename,
		ConstantsFilename:     o.constantsFilename,
		SelfContainedRegistry: o.selfContainedRegistry,
	}
	return r
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
	if r.HeaderComment == "" {
		r.HeaderComment = "// CODE-GENERATED by wiregen, DO NOT EDIT.\n\n"
	}
	if r.RegisterFuncName == "" {
		r.RegisterFuncName = "registerSSEDecoder"
	}
	if r.RegistryFuncName == "" {
		r.RegistryFuncName = "registerAllSSEDecoders"
	}
	if r.TypesFilename == "" {
		r.TypesFilename = "types.gen.ts"
	}
	if r.DecodersFilename == "" {
		r.DecodersFilename = "decoders.gen.ts"
	}
	if r.RegistryFilename == "" {
		r.RegistryFilename = "registry.gen.ts"
	}
	if r.ConstantsFilename == "" {
		r.ConstantsFilename = "constants.gen.ts"
	}
	if r.TypesImportPath == "" {
		r.TypesImportPath = "./types.gen.js"
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
	if err := os.WriteFile(filepath.Join(outDir, r.TypesFilename), []byte(typesBuf.String()), 0o644); err != nil { //nolint:gosec // generated TypeScript source is intentionally world-readable
		return fmt.Errorf("write %s: %w", r.TypesFilename, err)
	}
	var decodersBuf strings.Builder
	r.generateDecoders(&decodersBuf)
	if err := os.WriteFile(filepath.Join(outDir, r.DecodersFilename), []byte(decodersBuf.String()), 0o644); err != nil { //nolint:gosec // generated TypeScript source is intentionally world-readable
		return fmt.Errorf("write %s: %w", r.DecodersFilename, err)
	}
	var registryBuf strings.Builder
	r.generateRegistry(&registryBuf)
	if err := os.WriteFile(filepath.Join(outDir, r.RegistryFilename), []byte(registryBuf.String()), 0o644); err != nil { //nolint:gosec // generated TypeScript source is intentionally world-readable
		return fmt.Errorf("write %s: %w", r.RegistryFilename, err)
	}
	if len(r.Constants) > 0 {
		var constBuf strings.Builder
		r.generateConstants(&constBuf)
		if err := os.WriteFile(filepath.Join(outDir, r.ConstantsFilename), []byte(constBuf.String()), 0o644); err != nil { //nolint:gosec // generated TypeScript source is intentionally world-readable
			return fmt.Errorf("write %s: %w", r.ConstantsFilename, err)
		}
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

// GenerateConstants returns the constants.gen.ts content as a string.
func (r *Registry) GenerateConstants() string {
	r.init()
	var b strings.Builder
	r.generateConstants(&b)
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
	goType     reflect.Type
	wireName   string
	optional   bool
	jsonString bool // json:",string" option — wraps value as string on the wire
	depth      int  // embedding depth: 0 = direct field
}

func (r *Registry) parseFields(t reflect.Type) []fieldInfo {
	raw := r.parseFieldsDepth(t, 0, nil)
	// Apply encoding/json field selection semantics:
	// 1. Group by wireName
	// 2. For each name, pick the shallowest depth
	// 3. If multiple fields share the shallowest depth, omit (ambiguity)
	type entry struct {
		field fieldInfo
		count int // number of fields at the winning depth
	}
	best := make(map[string]*entry, len(raw))
	for _, f := range raw {
		if e, ok := best[f.wireName]; ok {
			if f.depth < e.field.depth {
				e.field = f
				e.count = 1
			} else if f.depth == e.field.depth {
				e.count++
			}
		} else {
			best[f.wireName] = &entry{field: f, count: 1}
		}
	}
	// Collect in stable order (order of first appearance in raw)
	seen := make(map[string]bool, len(best))
	var result []fieldInfo
	for _, f := range raw {
		if seen[f.wireName] {
			continue
		}
		seen[f.wireName] = true
		e := best[f.wireName]
		if e.count > 1 {
			continue // ambiguous: encoding/json omits both
		}
		result = append(result, e.field)
	}
	return result
}

func (r *Registry) parseFieldsDepth(t reflect.Type, depth int, visited map[reflect.Type]bool) []fieldInfo {
	if visited == nil {
		visited = make(map[reflect.Type]bool)
	}
	if visited[t] {
		return nil
	}
	visited[t] = true
	var fields []fieldInfo
	for sf := range t.Fields() {
		if !sf.IsExported() {
			continue
		}
		if sf.Anonymous {
			embedded := sf.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() != reflect.Struct {
				continue
			}
			fields = append(fields, r.parseFieldsDepth(embedded, depth+1, visited)...)
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
		// A bare "-" with no options means skip; "-" with options (e.g. "-,") means field named "-".
		if wireName == "-" && len(parts) == 1 {
			continue
		}
		omitempty := false
		jsonString := false
		for _, p := range parts[1:] {
			switch p {
			case "omitempty", "omitzero":
				omitempty = true
			case "string":
				jsonString = true
			}
		}
		// Pointers and maps are always optional; omitempty/omitzero makes any field optional.
		optional := omitempty || sf.Type.Kind() == reflect.Pointer || sf.Type.Kind() == reflect.Map
		fields = append(fields, fieldInfo{wireName: wireName, goType: sf.Type, optional: optional, jsonString: jsonString, depth: depth})
	}
	return fields
}

func (r *Registry) tsType(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		return r.tsType(t.Elem())
	}
	if r.TypeMappings != nil {
		if mapped, ok := r.TypeMappings[t]; ok {
			return mapped
		}
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
		return tsString
	}
	switch t.Kind() {
	case reflect.String:
		return tsString
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return tsString // encoding/json marshals []byte as base64 string
		}
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
	// DecoderMappings: use the provided decoder expression wrapped as an element decoder.
	if r.DecoderMappings != nil {
		if expr, ok := r.DecoderMappings[t]; ok {
			return "(v) => " + expr + "({v} as Record<string, unknown>, \"v\", \"elem\")"
		}
	}
	// TypeMappings without decoder: cast.
	if r.TypeMappings != nil {
		if mapped, ok := r.TypeMappings[t]; ok {
			return "(v) => v as " + mapped
		}
	}
	if r.isStruct(t) {
		return r.decoderName(t.Name())
	}
	if r.isEnum(t) {
		constName := r.enumConstName(t.Name())
		return "(v) => { const s = v as string; if (!" + constName + ".includes(s as never)) throw new TypeError(\"invalid enum value: \" + s); return s as " + r.tsEnumName(t.Name()) + "; }"
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
	return tsIdentityCast
}

// --- generation ---

func (r *Registry) generateTypes(w *strings.Builder) {
	w.WriteString(r.HeaderComment)

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
	seenTS := map[string]bool{}
	for _, t := range r.WireTypes {
		tn := r.tsName(t.Name())
		if seenTS[tn] {
			continue
		}
		seenTS[tn] = true
		names = append(names, t.Name())
	}
	sort.Slice(names, func(i, j int) bool { return r.tsName(names[i]) < r.tsName(names[j]) })
	for _, name := range names {
		t := r.typeByName[name]
		fields := r.parseFields(t)
		w.WriteString("export interface " + r.tsName(name) + " {\n")
		for _, f := range fields {
			ts := r.tsType(f.goType)
			if f.jsonString {
				ts = tsString
			}
			if f.optional {
				w.WriteString("  " + f.wireName + "?: " + ts + ";\n")
			} else {
				w.WriteString("  " + f.wireName + ": " + ts + ";\n")
			}
		}
		w.WriteString("}\n\n")
	}
}

func (r *Registry) generateDecoders(w *strings.Builder) { //nolint:gocyclo // large but flat type switch
	if r.ValidatorsImport == "" {
		panic("wiregen: ValidatorsImport must be set")
	}
	var bodies strings.Builder
	goNames := make([]string, 0, len(r.WireTypes))
	seenDecTS := map[string]bool{}
	for _, t := range r.WireTypes {
		tn := r.tsName(t.Name())
		if seenDecTS[tn] {
			continue
		}
		seenDecTS[tn] = true
		goNames = append(goNames, t.Name())
	}
	sort.Slice(goNames, func(i, j int) bool { return r.tsName(goNames[i]) < r.tsName(goNames[j]) })
	for _, name := range goNames {
		t := r.typeByName[name]
		r.emitDecoder(&bodies, name, t)
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
		w.WriteString(" } from \"" + r.TypesImportPath + "\";\n")
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
		w.WriteString("import type { Decoder } from \"" + r.ValidatorsImport + "\";\n\n")
		w.WriteString("const registry = new Map<string, Decoder<unknown>>();\n\n")
		w.WriteString("export function " + r.RegistryFuncName + "(): void {\n")
		for _, e := range r.SSEEvents {
			w.WriteString("  registry.set(\"" + e.EventType + "\", " + r.decoderName(e.TypeName) + " as Decoder<unknown>);\n")
		}
		w.WriteString("}\n\n")
		w.WriteString("export function getSSEDecoder(eventType: string): Decoder<unknown> | undefined {\n")
		w.WriteString("  return registry.get(eventType);\n")
		w.WriteString("}\n")
	} else {
		w.WriteString("import { " + r.RegisterFuncName + " } from \"" + r.BusImport + "\";\n")
		w.WriteString("import { " + strings.Join(decoderImports, ", ") + " } from \"./decoders.gen.js\";\n\n")
		w.WriteString("export function " + r.RegistryFuncName + "(): void {\n")
		for _, e := range r.SSEEvents {
			w.WriteString("  " + r.RegisterFuncName + "(\"" + e.EventType + "\", " + r.decoderName(e.TypeName) + ");\n")
		}
		w.WriteString("}\n")
	}
}

func (r *Registry) generateConstants(w *strings.Builder) {
	w.WriteString(r.HeaderComment)
	for _, c := range r.Constants {
		fmt.Fprintf(w, "export const %s = %d;\n", c.TSName, c.Value)
	}
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
	// json:",string" means the wire value is always a JSON string.
	if f.jsonString {
		return "reqStr(o, \"" + f.wireName + "\", \"" + path + "\")"
	}
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
	// DecoderMappings takes priority for custom-mapped types.
	if r.DecoderMappings != nil {
		if expr, ok := r.DecoderMappings[t]; ok {
			return expr + "(o, \"" + f.wireName + "\", \"" + path + "\")"
		}
	}
	if r.TypeMappings != nil {
		if _, ok := r.TypeMappings[t]; ok {
			return "o[\"" + f.wireName + "\"] as " + r.tsType(t)
		}
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
	// []byte → string (base64)
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		return "reqStr(o, \"" + f.wireName + "\", \"" + path + "\")"
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
	// json:",string" means the wire value is always a JSON string.
	if f.jsonString {
		varName := sanitizeVarName(f.wireName)
		w.WriteString("  const " + varName + " = optStr(o, \"" + f.wireName + "\", \"" + path + "\");\n")
		w.WriteString("  if (" + varName + " !== undefined) out." + f.wireName + " = " + varName + ";\n")
		return
	}
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
	// DecoderMappings takes priority for custom-mapped types.
	if r.DecoderMappings != nil {
		if expr, ok := r.DecoderMappings[t]; ok {
			varName := sanitizeVarName(f.wireName)
			w.WriteString("  const " + varName + " = " + expr + "(o, \"" + f.wireName + "\", \"" + path + "\");\n")
			w.WriteString("  if (" + varName + " !== undefined) out." + f.wireName + " = " + varName + ";\n")
			return
		}
	}
	if r.TypeMappings != nil {
		if _, ok := r.TypeMappings[t]; ok {
			w.WriteString("  if (o[\"" + f.wireName + "\"] !== undefined) out." + f.wireName + " = o[\"" + f.wireName + "\"] as " + r.tsType(t) + ";\n")
			return
		}
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
	// []byte → string (base64)
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		varName := sanitizeVarName(f.wireName)
		w.WriteString("  const " + varName + " = optStr(o, \"" + f.wireName + "\", \"" + path + "\");\n")
		w.WriteString("  if (" + varName + " !== undefined) out." + f.wireName + " = " + varName + ";\n")
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
	if ident == "" {
		return false
	}
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
