// Package wiregen generates TypeScript interfaces, decoders, and an SSE
// registry from Go struct types using go/packages + go/types + ast.Inspect.
// Consumers register types via the compile-time-safe TypeRef[T]() helper
// and invoke Generate to emit TS files.
package wiregen

import (
	"errors"
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
type Registry struct {
	Enums                 map[string]EnumDef
	EnumTSName            map[string]string
	TSNameOverride        map[string]string
	PathNameOverride      map[string]string
	TypeMappings          map[string]string
	DecoderMappings       map[string]string
	DiscriminatorMap      map[string]map[string]string
	typeNames             map[string]bool
	ValidatorsImport      string
	TypesFilename         string
	ConstantsFilename     string
	RegistryFilename      string
	DecodersFilename      string
	BusImport             string
	TypesImportPath       string
	HeaderComment         string
	RegisterFuncName      string
	RegistryFuncName      string
	Types                 []WireType
	PackagePaths          []string
	Constants             []WireConst
	SSEEvents             []SSERegEntry
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
	r.initMaps()
	r.initDefaults()
	// Build typeNames set for cross-referencing in decoders
	r.typeNames = make(map[string]bool, len(r.Types))
	for _, wt := range r.Types {
		r.typeNames[wt.Name] = true
	}
}

// initMaps allocates the nil override/mapping maps so callers can assign into
// them without a nil check.
func (r *Registry) initMaps() {
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
}

// initDefaults fills the empty header/func-name/filename/import knobs with
// their conventional defaults.
func (r *Registry) initDefaults() {
	if r.HeaderComment == "" {
		r.HeaderComment = "// CODE-GENERATED by wiregen, DO NOT EDIT.\n\n"
	}
	r.RegisterFuncName = sanitizeTSIdent(r.RegisterFuncName)
	if r.RegisterFuncName == "" {
		r.RegisterFuncName = "registerSSEDecoder"
	}
	r.RegistryFuncName = sanitizeTSIdent(r.RegistryFuncName)
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

// genFile is one generated TS file's name and in-memory content, staged before
// the atomic rename pass in writeFilesAtomically.
type genFile struct{ name, content string }

// writeFilesAtomically stages every file as a temp sibling in outDir, then
// renames them into place only after all temp writes succeed, so a mid-sequence
// failure (disk full / ENOSPC) never leaves the committed wire/ directory
// half-updated. Any temp left unrenamed on an error path is removed best-effort
// on return.
func writeFilesAtomically(outDir string, files []genFile) error {
	staged := make([]string, 0, len(files))
	defer func() {
		for _, name := range staged {
			_ = os.Remove(name) // best-effort cleanup of any unrenamed temp
		}
	}()
	for _, gf := range files {
		tmp, createErr := os.CreateTemp(outDir, gf.name+".*.tmp")
		if createErr != nil {
			return fmt.Errorf("stage %s: %w", gf.name, createErr)
		}
		staged = append(staged, tmp.Name())
		_, werr := tmp.WriteString(gf.content)
		cerr := tmp.Close()
		if werr != nil {
			return fmt.Errorf("write %s: %w", gf.name, werr)
		}
		if cerr != nil {
			return fmt.Errorf("write %s: %w", gf.name, cerr)
		}
		if chmodErr := os.Chmod(tmp.Name(), 0o644); chmodErr != nil {
			return fmt.Errorf("chmod %s: %w", gf.name, chmodErr)
		}
	}
	for i, gf := range files {
		if renameErr := os.Rename(staged[i], filepath.Join(outDir, gf.name)); renameErr != nil {
			return fmt.Errorf("write %s: %w", gf.name, renameErr)
		}
	}
	return nil
}

// Generate writes generated TS files to outDir using the AST engine.
func (r *Registry) Generate(outDir string) error {
	r.init()
	if r.ValidatorsImport == "" {
		return errors.New("wiregen: ValidatorsImport must be set")
	}
	if len(r.SSEEvents) > 0 && !r.SelfContainedRegistry && r.BusImport == "" {
		return errors.New("wiregen: BusImport must be set when SelfContainedRegistry is false")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	engine, err := newASTEngine(r)
	if err != nil {
		return err
	}

	// Build every file's content in memory first, so a generation failure
	// never leaves a partially-updated wire/ directory.
	var typesBuf, decodersBuf strings.Builder
	r.generateTypes(&typesBuf, engine)
	r.generateDecoders(&decodersBuf, engine)
	files := []genFile{
		{r.TypesFilename, typesBuf.String()},
		{r.DecodersFilename, decodersBuf.String()},
	}
	if len(r.SSEEvents) > 0 {
		var b strings.Builder
		r.generateRegistry(&b)
		files = append(files, genFile{r.RegistryFilename, b.String()})
	}
	if len(r.Constants) > 0 {
		var b strings.Builder
		r.generateConstants(&b)
		files = append(files, genFile{r.ConstantsFilename, b.String()})
	}

	return writeFilesAtomically(outDir, files)
}

// GenerateTypes returns the types.gen.ts content as a string.
func (r *Registry) GenerateTypes() string {
	r.init()
	engine, err := newASTEngine(r)
	if err != nil {
		panic(err.Error())
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
		panic(err.Error())
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

// GenerateValidators returns the opt-in validators starter module as a string:
// the reference implementation of the "Validators contract" (the 11 helper
// functions the generated decoders import — asObject, asArray, reqStr, reqNum,
// reqBool, optStr, optNum, optBool, reqOneOf, decodeArray, decodeRecord — plus
// the Decoder<T> type alias).
//
// Unlike the other Generate* methods, the content is constant (it does not
// depend on the registered types) and the module carries a distinct "copy
// once, then own it" banner instead of r.HeaderComment — it is a one-time
// scaffold a NEW consumer copies once and then OWNS and edits freely. It is
// never regenerated and is deliberately NOT part of Generate's default writes,
// so an existing consumer's hand-edited copy is never clobbered.
func (r *Registry) GenerateValidators() string {
	r.init()
	var b strings.Builder
	r.generateValidators(&b)
	return b.String()
}

// --- helpers ---

func (r *Registry) tsName(goName string) string {
	if override, ok := r.TSNameOverride[goName]; ok {
		if s := sanitizeTSIdent(override); s != "" {
			return s
		}
	}
	return goName
}

func (r *Registry) tsEnumName(goName string) string {
	if override, ok := r.EnumTSName[goName]; ok {
		s := sanitizeVarName(override)
		if s == "" {
			return goName
		}
		return s
	}
	return goName
}

func (r *Registry) decoderName(typeName string) string {
	return "decode" + r.tsName(typeName)
}

func (r *Registry) pathName(typeName string) string {
	if override, ok := r.PathNameOverride[typeName]; ok {
		return tsStringLiteral(override)
	}
	var b strings.Builder
	runes := []rune(typeName)
	for i, ru := range runes {
		if ru < 'A' || ru > 'Z' {
			b.WriteRune(ru)
			continue
		}
		if needsWordBreak(runes, i) {
			b.WriteByte('_')
		}
		b.WriteRune(ru + 32)
	}
	return b.String()
}

// needsWordBreak reports whether an underscore should precede runes[i] (which
// the caller has determined is an uppercase letter): after a lowercase letter
// (camelCase boundary), or at the tail of an acronym immediately followed by a
// lowercase letter (e.g. the "S" in "HTTPServer").
func needsWordBreak(runes []rune, i int) bool {
	if i == 0 {
		return false
	}
	prev := runes[i-1]
	if prev >= 'a' && prev <= 'z' {
		return true
	}
	return prev >= 'A' && prev <= 'Z' && i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
}

func (r *Registry) enumConstName(goTypeName string) string {
	name := r.tsEnumName(goTypeName)
	var b strings.Builder
	runes := []rune(name)
	for i, ru := range runes {
		if ru < 'A' || ru > 'Z' {
			b.WriteRune(ru - 32)
			continue
		}
		if needsWordBreak(runes, i) {
			b.WriteByte('_')
		}
		b.WriteRune(ru)
	}
	b.WriteString("S")
	return b.String()
}

// keepIdentRune reports whether r is valid in a TS identifier: letters, '_'
// and '$' are always allowed; a digit is allowed only when a prefix has
// already been emitted (hasPrefix), so an identifier never starts with a digit.
func keepIdentRune(r rune, hasPrefix bool) bool {
	if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '$' {
		return true
	}
	return hasPrefix && r >= '0' && r <= '9'
}

// isValidTSIdent reports whether s is usable as a bare TS identifier (an
// interface/object property name or a member access needs no quoting). It
// mirrors keepIdentRune's char-class and rejects the empty string and any
// leading digit.
func isValidTSIdent(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for i, r := range s {
		if !keepIdentRune(r, i > 0) {
			return false
		}
	}
	return true
}

// tsPropName renders wireName as a TS property name: bare when it is a valid
// identifier (preserving existing golden output), otherwise a double-quoted
// string-literal key so a non-identifier JSON key (e.g. "content-type") stays
// valid TypeScript.
func tsPropName(wireName string) string {
	if isValidTSIdent(wireName) {
		return wireName
	}
	return "\"" + tsStringLiteral(wireName) + "\""
}

// tsMemberRef renders a member access into the decoded out object: ".name" for
// a valid identifier (preserving existing golden output), otherwise ["name"]
// bracket access so a non-identifier JSON key stays valid TypeScript.
func tsMemberRef(wireName string) string {
	if isValidTSIdent(wireName) {
		return "." + wireName
	}
	return "[\"" + tsStringLiteral(wireName) + "\"]"
}

// sanitizeTSIdent strips characters that are not valid in a TS identifier,
// preserving case and underscores (unlike sanitizeVarName which camelCases).
func sanitizeTSIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if keepIdentRune(r, b.Len() > 0) {
			b.WriteRune(r)
		}
	}
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

	// Strip characters that are not valid in a TS identifier.
	s = sanitizeTSIdent(s)
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

// localVarName returns a non-empty TS-safe local variable name for wireName.
// sanitizeVarName is contracted to return "" for a wire name with no
// identifier-safe runes (e.g. json:"_" or json:"404"); fall back to a fixed
// safe name so the emitted `const <name> = ...` is never an empty identifier.
func localVarName(wireName string) string {
	if v := sanitizeVarName(wireName); v != "" {
		return v
	}
	return "fieldVal"
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
