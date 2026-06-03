// Package wiregen generates TypeScript interfaces, decoders, and an SSE
// registry from Go struct types using go/packages + go/types + ast.Inspect.
// Consumers register types via the compile-time-safe TypeRef[T]() helper
// and invoke Generate to emit TS files.
package wiregen

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// WireType is a compile-time-safe type reference captured by TypeRef[T]().
type WireType struct {
	PkgPath string
	Name    string
}

// TypeRef registers a concrete Go type for TS generation. A typo or
// nonexistent type is a compile error — the generic constraint ensures T exists.
func TypeRef[T any]() WireType {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		t = reflect.TypeFor[T]()
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return WireType{PkgPath: t.PkgPath(), Name: t.Name()}
}

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

// UnionDef defines a discriminated union parsed from //wiregen:union directive.
type UnionDef struct {
	Discriminator string
	Variants      []string
}

// Option configures optional behavior knobs on a [Registry].
type Option func(*options)

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

// WithValidatorsImport sets the import path for the validators module.
func WithValidatorsImport(v string) Option { return func(o *options) { o.validatorsImport = v } }

// WithBusImport sets the import path for the SSE bus module.
func WithBusImport(v string) Option { return func(o *options) { o.busImport = v } }

// WithTypesImportPath sets the import path used in decoders to reference types.
func WithTypesImportPath(v string) Option { return func(o *options) { o.typesImportPath = v } }

// WithHeaderComment sets the header comment prepended to every generated file.
func WithHeaderComment(v string) Option { return func(o *options) { o.headerComment = v } }

// WithRegisterFuncName sets the function name imported from the bus module.
func WithRegisterFuncName(v string) Option { return func(o *options) { o.registerFuncName = v } }

// WithRegistryFuncName sets the exported function name in the registry file.
func WithRegistryFuncName(v string) Option { return func(o *options) { o.registryFuncName = v } }

// WithSelfContainedRegistry enables self-contained registry mode.
func WithSelfContainedRegistry(v bool) Option {
	return func(o *options) { o.selfContainedRegistry = v }
}

// WithFilenames overrides the output filenames for generated files.
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
type Registry struct { //nolint:govet // fieldalignment: readability over alignment
	Enums                 map[string]EnumDef
	EnumTSName            map[string]string
	TSNameOverride        map[string]string
	PathNameOverride      map[string]string
	TypeMappings          map[string]string
	DecoderMappings       map[string]string
	DiscriminatorMap      map[string]map[string]string
	typeNames             map[string]bool // populated from Types for decoder cross-ref
	PackagePaths          []string
	Types                 []WireType
	SSEEvents             []SSERegEntry
	Constants             []WireConst
	ValidatorsImport      string
	BusImport             string
	TypesImportPath       string
	HeaderComment         string
	RegisterFuncName      string
	RegistryFuncName      string
	TypesFilename         string
	DecodersFilename      string
	RegistryFilename      string
	ConstantsFilename     string
	SelfContainedRegistry bool
	initialized           bool
}

// NewRegistry creates a [Registry] with the given functional options applied.
func NewRegistry(opts ...Option) *Registry {
	var o options
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}
	return &Registry{
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
}

func (r *Registry) init() {
	if r.initialized {
		return
	}
	r.initialized = true
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
	if r.TypeMappings == nil {
		r.TypeMappings = map[string]string{}
	}
	if r.DecoderMappings == nil {
		r.DecoderMappings = map[string]string{}
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
	// Build typeNames set for cross-referencing in decoders
	r.typeNames = make(map[string]bool, len(r.Types))
	for _, wt := range r.Types {
		r.typeNames[wt.Name] = true
	}
}

// Generate writes generated TS files to outDir using the AST engine.
func (r *Registry) Generate(outDir string) error {
	r.init()
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	engine, err := newASTEngine(r)
	if err != nil {
		return err
	}

	var typesBuf strings.Builder
	r.generateTypes(&typesBuf, engine)
	if err := os.WriteFile(filepath.Join(outDir, r.TypesFilename), []byte(typesBuf.String()), 0o644); err != nil { //nolint:gosec // generated TS is intentionally world-readable
		return fmt.Errorf("write %s: %w", r.TypesFilename, err)
	}

	var decodersBuf strings.Builder
	r.generateDecoders(&decodersBuf, engine)
	if err := os.WriteFile(filepath.Join(outDir, r.DecodersFilename), []byte(decodersBuf.String()), 0o644); err != nil { //nolint:gosec // generated TS is intentionally world-readable
		return fmt.Errorf("write %s: %w", r.DecodersFilename, err)
	}

	var registryBuf strings.Builder
	r.generateRegistry(&registryBuf)
	if err := os.WriteFile(filepath.Join(outDir, r.RegistryFilename), []byte(registryBuf.String()), 0o644); err != nil { //nolint:gosec // generated TS is intentionally world-readable
		return fmt.Errorf("write %s: %w", r.RegistryFilename, err)
	}

	if len(r.Constants) > 0 {
		var constBuf strings.Builder
		r.generateConstants(&constBuf)
		if err := os.WriteFile(filepath.Join(outDir, r.ConstantsFilename), []byte(constBuf.String()), 0o644); err != nil { //nolint:gosec // generated TS is intentionally world-readable
			return fmt.Errorf("write %s: %w", r.ConstantsFilename, err)
		}
	}
	return nil
}

// GenerateTypes returns the types.gen.ts content as a string.
func (r *Registry) GenerateTypes() string {
	r.init()
	engine, err := newASTEngine(r)
	if err != nil {
		panic("wiregen: " + err.Error())
	}
	var b strings.Builder
	r.generateTypes(&b, engine)
	return b.String()
}

// GenerateDecoders returns the decoders.gen.ts content as a string.
func (r *Registry) GenerateDecoders() string {
	r.init()
	if r.ValidatorsImport == "" {
		panic("wiregen: ValidatorsImport must be set")
	}
	engine, err := newASTEngine(r)
	if err != nil {
		panic("wiregen: " + err.Error())
	}
	var b strings.Builder
	r.generateDecoders(&b, engine)
	return b.String()
}

// GenerateRegistry returns the registry.gen.ts content as a string.
func (r *Registry) GenerateRegistry() string {
	r.init()
	if !r.SelfContainedRegistry && r.BusImport == "" {
		panic("wiregen: BusImport must be set when SelfContainedRegistry is false")
	}
	if r.SelfContainedRegistry && r.ValidatorsImport == "" {
		panic("wiregen: ValidatorsImport must be set when SelfContainedRegistry is true")
	}
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

// --- helpers ---

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

	// Strip characters that are not valid in a TS identifier
	var clean strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '$' {
			clean.WriteRune(r)
		} else if clean.Len() > 0 && r >= '0' && r <= '9' {
			clean.WriteRune(r)
		}
	}
	s = clean.String()
	if s == "" {
		return ""
	}

	switch s {
	case "o", "out", "v", "private", "public", "protected", "class",
		"return", "delete", "default", "export", "import", "new", "this":
		return s + "Val"
	}
	return s
}

// tsStringLiteral escapes a string for safe embedding in a TS double-quoted string literal.
func tsStringLiteral(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '`':
			b.WriteRune('`')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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
