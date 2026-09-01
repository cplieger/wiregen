// Package wiregen generates TypeScript interfaces, decoders, and an SSE
// registry from Go struct types using go/packages + go/types + ast.Inspect.
// Consumers register types via the compile-time-safe TypeRef[T]() helper
// and invoke Generate to emit TS files.
package wiregen

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	t := reflect.TypeFor[T]()
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
	transportImport       string
	typesImportPath       string
	headerComment         string
	registerFuncName      string
	registryFuncName      string
	typesFilename         string
	decodersFilename      string
	registryFilename      string
	constantsFilename     string
	clientFilename        string
	validatorsFilename    string
	selfContainedRegistry bool
}

// WithValidatorsImport sets the import path for the validators module.
func WithValidatorsImport(v string) Option { return func(o *options) { o.validatorsImport = v } }

// WithValidatorsFile makes Generate write the library-owned validators module
// (the runtime the generated decoders import, with a DO-NOT-EDIT banner) at
// the given path relative to outDir on every run. The path may point outside
// outDir (e.g. "../validators.ts") so the module can live beside the
// consumer's hand-written source at the import path the decoders use. Empty
// (the default) writes nothing — but the module is wiregen-owned either way:
// scaffold it via GenerateValidators() and never hand-edit it.
func WithValidatorsFile(v string) Option { return func(o *options) { o.validatorsFilename = v } }

// WithTransportImport sets the import path for the transport module the
// generated client calls into. Required when endpoints are registered. The
// module must export clientRequest, clientRequestOK, clientRequestRaw, and
// the ApiResult type — see the client-transport contract in the README.
func WithTransportImport(v string) Option { return func(o *options) { o.transportImport = v } }

// WithClientFilename overrides the generated client filename
// (default "client.gen.ts"). It is a standalone option rather than a
// [Filenames] field because Filenames exists to label a RUN of same-typed
// positional arguments, which a single string does not have — and a fifth
// field would push the struct to 80 bytes, which gocritic's hugeParam
// correctly flags for a by-value parameter.
func WithClientFilename(v string) Option {
	return func(o *options) {
		if v != "" {
			o.clientFilename = v
		}
	}
}

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

// Filenames names the four generated output files whose names are otherwise
// interchangeable positionals. It is a struct rather than four positional
// strings because the four are same-typed and adjacent: a transposed pair
// compiled and wrote each file's content under another file's name, which a
// build only notices when an importer fails to find a symbol. Field names
// label each one at the call site.
//
// An empty field keeps that file's default name, so a caller overriding one
// name sets one field and leaves the rest zero — the zero Filenames overrides
// nothing and is exactly equivalent to omitting WithFilenames.
//
// Two generated files are named elsewhere, each for its own reason: the client
// by [WithClientFilename] (a lone string argument cannot be transposed), and
// the validators module by [WithValidatorsFile] (whose empty value means "do
// not write it at all", not "use the default name").
type Filenames struct {
	// Types names the file holding the generated type declarations.
	Types string
	// Decoders names the file holding the generated decoders.
	Decoders string
	// Registry names the file holding the generated registry.
	Registry string
	// Constants names the file holding the generated constants.
	Constants string
}

// WithFilenames overrides the output filenames for generated files. An empty
// field in names keeps that file's default (see [Filenames]).
func WithFilenames(names Filenames) Option {
	return func(o *options) {
		if names.Types != "" {
			o.typesFilename = names.Types
		}
		if names.Decoders != "" {
			o.decodersFilename = names.Decoders
		}
		if names.Registry != "" {
			o.registryFilename = names.Registry
		}
		if names.Constants != "" {
			o.constantsFilename = names.Constants
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
	ClientFilename        string
	ValidatorsFilename    string
	BusImport             string
	TransportImport       string
	TypesImportPath       string
	HeaderComment         string
	RegisterFuncName      string
	RegistryFuncName      string
	Types                 []WireType
	PackagePaths          []string
	Constants             []WireConst
	SSEEvents             []SSERegEntry
	Endpoints             []Endpoint
	SelfContainedRegistry bool
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
		TransportImport:       o.transportImport,
		TypesImportPath:       o.typesImportPath,
		HeaderComment:         o.headerComment,
		RegisterFuncName:      o.registerFuncName,
		RegistryFuncName:      o.registryFuncName,
		TypesFilename:         o.typesFilename,
		DecodersFilename:      o.decodersFilename,
		RegistryFilename:      o.registryFilename,
		ConstantsFilename:     o.constantsFilename,
		ClientFilename:        o.clientFilename,
		ValidatorsFilename:    o.validatorsFilename,
		SelfContainedRegistry: o.selfContainedRegistry,
	}
}

// init (re)builds the derived registration state. It runs on EVERY generate
// call — there is no once-latch, so a Registry reused across calls (Types
// appended between two Generate invocations) never sees stale typeNames.
// It rejects a duplicate bare type name: the engine keys types by bare Name,
// so two same-named types (from the same or different packages) would
// silently merge into one emitted type.
func (r *Registry) init() error {
	r.initMaps()
	r.initDefaults()
	r.typeNames = make(map[string]bool, len(r.Types))
	seenPkg := make(map[string]string, len(r.Types))
	for _, wt := range r.Types {
		if prev, dup := seenPkg[wt.Name]; dup {
			if prev == wt.PkgPath {
				return fmt.Errorf("wiregen: type %s.%s is registered twice", wt.PkgPath, wt.Name)
			}
			return fmt.Errorf("wiregen: type name %q is registered from two packages (%s and %s); wiregen keys types by bare name — rename one type or register only one", wt.Name, prev, wt.PkgPath)
		}
		seenPkg[wt.Name] = wt.PkgPath
		r.typeNames[wt.Name] = true
	}
	return r.validateEnums()
}

// validateEnums rejects enum registrations whose emitted identifiers collide:
// two Go enums resolving to the same TS type name (the second would silently
// vanish from types.gen.ts), or to the same const value-array name (reqOneOf
// membership checks would validate against the wrong value set).
func (r *Registry) validateEnums() error {
	seenTS := map[string]string{}
	seenConst := map[string]string{}
	for _, name := range enumNamesSlice(r.Enums) {
		tn := r.tsEnumName(name)
		if prev, ok := seenTS[tn]; ok {
			return fmt.Errorf("wiregen: enums %q and %q both emit TS type name %q; set EnumTSName to disambiguate", prev, name, tn)
		}
		seenTS[tn] = name
		cn := r.enumConstName(name)
		if prev, ok := seenConst[cn]; ok {
			return fmt.Errorf("wiregen: enums %q and %q both emit const array name %q; set EnumTSName to disambiguate", prev, name, cn)
		}
		seenConst[cn] = name
	}
	return nil
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

// defaultHeaderComment heads every generated file when the consumer sets no
// WithHeaderComment override.
const defaultHeaderComment = "// CODE-GENERATED by wiregen, DO NOT EDIT.\n\n"

// moduleSpecifier returns the relative ES module specifier the generated files
// use to import from another generated file. The Filenames knobs name
// TypeScript SOURCE files while an ES import must name the module the compiler
// emits, so the final extension is rewritten the way TypeScript rewrites it:
// .mts becomes .mjs, .cts becomes .cjs, and every other extension (.ts, .tsx,
// or none at all) becomes .js.
func moduleSpecifier(filename string) string {
	base, ext, _ := strings.CutLast(filename, ".")
	out := ".js"
	switch ext {
	case "mts":
		out = ".mjs"
	case "cts":
		out = ".cjs"
	}
	return "./" + tsStringLiteral(base) + out
}

// initDefaults fills the empty header/func-name/filename/import knobs with
// their conventional defaults.
func (r *Registry) initDefaults() {
	if r.HeaderComment == "" {
		r.HeaderComment = defaultHeaderComment
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
	if r.ClientFilename == "" {
		r.ClientFilename = "client.gen.ts"
	}
	if r.TypesImportPath == "" {
		// Derived from TypesFilename (defaulted just above), so renaming the
		// types file does not silently leave the decoders importing a module
		// that was never written. An explicit WithTypesImportPath still wins,
		// which is what a consumer keeping the types file outside outDir needs.
		r.TypesImportPath = moduleSpecifier(r.TypesFilename)
	}
}

// genFile is one generated TS file's name and in-memory content, staged before
// the atomic rename pass in writeFilesAtomically.
type genFile struct{ name, content string }

// writeFilesAtomically stages every file as a temp sibling of its target,
// then renames them into place only after all temp writes succeed. A failure
// while staging (the common case — e.g. disk full / ENOSPC) therefore leaves
// the committed wire/ directory untouched, and each individual rename is
// atomic — but the rename pass itself is sequential, so a failure mid-pass
// (e.g. outDir removed concurrently) can leave a prefix of the files updated:
// the guarantee is per-file atomicity, not a multi-file transaction. Any temp
// left unrenamed on an error path is removed best-effort on return. A file
// name may carry a relative directory prefix (e.g. the ValidatorsFilename
// "../validators.ts"); its temp is staged in the target's own directory so
// the rename stays same-filesystem-atomic.
func writeFilesAtomically(outDir string, files []genFile) error {
	staged := make([]string, 0, len(files))
	defer func() {
		for _, name := range staged {
			_ = os.Remove(name) // best-effort cleanup of any unrenamed temp
		}
	}()
	for _, gf := range files {
		target := filepath.Join(outDir, gf.name)
		tmp, createErr := os.CreateTemp(filepath.Dir(target), filepath.Base(gf.name)+".*.tmp")
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

// Generate writes generated TS files to outDir using the AST engine. It loads
// and type-checks the registered packages, which runs the go command as a
// subprocess, so ctx bounds the whole pass: cancelling it aborts the load.
func (r *Registry) Generate(ctx context.Context, outDir string) error {
	if err := r.init(); err != nil {
		return err
	}
	if r.ValidatorsImport == "" {
		return errors.New("wiregen: ValidatorsImport must be set")
	}
	if len(r.SSEEvents) > 0 && !r.SelfContainedRegistry && r.BusImport == "" {
		return errors.New("wiregen: BusImport must be set when SelfContainedRegistry is false")
	}
	if len(r.Endpoints) > 0 && r.TransportImport == "" {
		return errors.New("wiregen: TransportImport must be set when endpoints are registered")
	}
	if err := r.validateConstants(); err != nil {
		return err
	}
	if err := r.validateEndpoints(); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	engine, err := newASTEngine(ctx, r)
	if err != nil {
		return err
	}
	if err := r.validateUnionSSE(engine); err != nil {
		return err
	}

	return writeFilesAtomically(outDir, r.buildGenFiles(engine))
}

// buildGenFiles renders every configured output file's content in memory, so
// a generation failure never leaves a partially-updated wire/ directory.
func (r *Registry) buildGenFiles(engine *astEngine) []genFile {
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
	if len(r.Endpoints) > 0 {
		var b strings.Builder
		r.generateClient(&b)
		files = append(files, genFile{r.ClientFilename, b.String()})
	}
	if r.ValidatorsFilename != "" {
		var b strings.Builder
		r.generateValidators(&b)
		files = append(files, genFile{r.ValidatorsFilename, b.String()})
	}
	return files
}

// GenerateTypes returns the types.gen.ts content as a string. Every per-file
// string generator returns an error on the same config problems Generate
// rejects — no exported generator panics.
//
// ctx bounds the package load, as in [Registry.Generate]. The three generators
// that take one ([Registry.Generate], this, and
// [Registry.GenerateDecoders]) are exactly the three that read the registered
// packages from source; the rest render from the registry alone and do no I/O.
func (r *Registry) GenerateTypes(ctx context.Context) (string, error) {
	if err := r.init(); err != nil {
		return "", err
	}
	engine, err := newASTEngine(ctx, r)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	r.generateTypes(&b, engine)
	return b.String(), nil
}

// GenerateDecoders returns the decoders.gen.ts content as a string. ctx bounds
// the package load, as in [Registry.Generate].
func (r *Registry) GenerateDecoders(ctx context.Context) (string, error) {
	if err := r.init(); err != nil {
		return "", err
	}
	if r.ValidatorsImport == "" {
		return "", errors.New("wiregen: ValidatorsImport must be set")
	}
	engine, err := newASTEngine(ctx, r)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	r.generateDecoders(&b, engine)
	return b.String(), nil
}

// GenerateRegistry returns the registry.gen.ts content as a string.
func (r *Registry) GenerateRegistry() (string, error) {
	if err := r.init(); err != nil {
		return "", err
	}
	if !r.SelfContainedRegistry && r.BusImport == "" {
		return "", errors.New("wiregen: BusImport must be set when SelfContainedRegistry is false")
	}
	if r.SelfContainedRegistry && r.ValidatorsImport == "" {
		return "", errors.New("wiregen: ValidatorsImport must be set when SelfContainedRegistry is true")
	}
	var b strings.Builder
	r.generateRegistry(&b)
	return b.String(), nil
}

// GenerateConstants returns the constants.gen.ts content as a string,
// rejecting a registered constant whose TSName sanitizes to an empty TS
// identifier (the same validation Generate applies).
func (r *Registry) GenerateConstants() (string, error) {
	if err := r.init(); err != nil {
		return "", err
	}
	if err := r.validateConstants(); err != nil {
		return "", err
	}
	var b strings.Builder
	r.generateConstants(&b)
	return b.String(), nil
}

// validateConstants rejects any registered WireConst whose TSName sanitizes
// to an empty TS identifier — previously such a constant was silently dropped
// from constants.gen.ts, making a misconfigured driver invisible.
func (r *Registry) validateConstants() error {
	for _, c := range r.Constants {
		if sanitizeTSIdent(c.TSName) == "" {
			return fmt.Errorf("wiregen: constant %q (value %d) has no TS-identifier-safe characters in its TSName", c.TSName, c.Value)
		}
	}
	return nil
}

// GenerateValidators returns the library-owned validators module as a string:
// the implementation of the "Validators contract" (the 11 helper functions
// the generated decoders import — asObject, asArray, reqStr, reqNum, reqBool,
// optStr, optNum, optBool, reqOneOf, decodeArray, decodeRecord — plus the
// Decoder<T> type alias), under the same DO-NOT-EDIT banner as every other
// generated file (r.HeaderComment, or the default when unset).
//
// The content is constant (it does not depend on the registered types).
// Consumers either set WithValidatorsFile so Generate writes it on every run,
// or call this directly from their driver — never hand-edit the output. The
// v1-era copy-once-then-own starter posture is retired.
func (r *Registry) GenerateValidators() string {
	var b strings.Builder
	r.generateValidators(&b)
	return b.String()
}

// --- helpers ---

func (r *Registry) tsName(goName string) string {
	if override, ok := r.TSNameOverride[goName]; ok {
		if s := sanitizeTypeIdent(override); s != "" {
			return s
		}
	}
	return goName
}

func (r *Registry) tsEnumName(goName string) string {
	if override, ok := r.EnumTSName[goName]; ok {
		if s := sanitizeTypeIdent(override); s != "" {
			return s
		}
	}
	return goName
}

// sanitizeTypeIdent sanitizes a consumer-supplied TS type-name override —
// the ONE sanitizer for both struct (TSNameOverride) and enum (EnumTSName)
// overrides, so the same raw override string always yields the same
// identifier: case-preserving character stripping plus the reserved-word
// suffix guard.
func sanitizeTypeIdent(s string) string {
	t := sanitizeTSIdent(s)
	if t == "" {
		return ""
	}
	if tsReservedWords[t] {
		return t + "Val"
	}
	return t
}

func (r *Registry) decoderName(typeName string) string {
	return "decode" + r.tsName(typeName)
}

// unionPayloadDecoderName is the 1-argument payload-adapter decoder name for
// a //wiregen:union type: decode<TSName>Payload.
func (r *Registry) unionPayloadDecoderName(typeName string) string {
	return "decode" + r.tsName(typeName) + "Payload"
}

// sseDecoderName returns the decoder the registry binds for an SSE event's
// type: the 1-arg union payload adapter when the type carries a
// DiscriminatorMap (a //wiregen:union type), otherwise the plain struct
// decoder.
func (r *Registry) sseDecoderName(typeName string) string {
	if r.DiscriminatorMap[typeName] != nil {
		return r.unionPayloadDecoderName(typeName)
	}
	return r.decoderName(typeName)
}

// validateUnionSSE rejects SSE registrations that cannot produce a working
// registry: a //wiregen:union type registered in SSEEvents needs a
// DiscriminatorMap entry (its runtime decoder and 1-arg payload adapter are
// only emitted with one), and a union's payload-adapter name must not collide
// with another registered type's decoder name.
func (r *Registry) validateUnionSSE(engine *astEngine) error {
	for _, e := range r.SSEEvents {
		ti := engine.byName[e.TypeName]
		if ti != nil && ti.Union != nil && r.DiscriminatorMap[e.TypeName] == nil {
			return fmt.Errorf("wiregen: SSE event %q registers union type %s without a DiscriminatorMap entry (required for its runtime decoder)", e.EventType, e.TypeName)
		}
	}
	for _, ti := range engine.types {
		if ti.Union == nil || r.DiscriminatorMap[ti.Name] == nil {
			continue
		}
		adapter := r.unionPayloadDecoderName(ti.Name)
		for _, wt := range r.Types {
			if wt.Name != ti.Name && r.decoderName(wt.Name) == adapter {
				return fmt.Errorf("wiregen: union %s payload adapter %s collides with the decoder of registered type %s; rename one type", ti.Name, adapter, wt.Name)
			}
		}
	}
	return nil
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
		switch {
		case ru >= 'a' && ru <= 'z':
			b.WriteRune(ru - 32)
		case ru >= 'A' && ru <= 'Z':
			if needsWordBreak(runes, i) {
				b.WriteByte('_')
			}
			b.WriteRune(ru)
		default:
			// Digits, '_', '$' pass through unchanged (the old unconditional
			// ru-32 corrupted them into control characters / other symbols).
			b.WriteRune(ru)
		}
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

	if tsReservedWords[s] {
		return s + "Val"
	}
	return s
}

// tsReservedWords are the identifiers sanitizeVarName must never emit as a
// generated `const` binding name: the ES keyword set plus the strict-mode
// reserved words (generated files are ES modules, which are always strict),
// `await` (reserved at module level), the strict-mode restricted binding
// names `eval`/`arguments`, the locals the decoder emit context already binds
// (`o`, `out`, `v`), and the globals the emitted bodies reference (`undefined`
// in optional-field guards, `TypeError` in inline element decoders) whose
// shadowing would silently break the generated code.
var tsReservedWords = map[string]bool{
	// ES keywords
	"break": true, "case": true, "catch": true, "class": true, "const": true,
	"continue": true, "debugger": true, "default": true, "delete": true,
	"do": true, "else": true, "enum": true, "export": true, "extends": true,
	"false": true, "finally": true, "for": true, "function": true, "if": true,
	"import": true, "in": true, "instanceof": true, "new": true, "null": true,
	"return": true, "super": true, "switch": true, "this": true, "throw": true,
	"true": true, "try": true, "typeof": true, "var": true, "void": true,
	"while": true, "with": true,
	// strict-mode reserved words, restricted binding names, module-level await
	"implements": true, "interface": true, "let": true, "package": true,
	"private": true, "protected": true, "public": true, "static": true,
	"yield": true, "await": true, "eval": true, "arguments": true,
	// decoder emit-context locals
	"o": true, "out": true, "v": true,
	// referenced globals whose shadowing breaks emitted code
	"undefined": true, "TypeError": true,
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
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isIdentReferenced reports whether ident occurs in body with identifier
// boundaries on both sides. Import/const selection is owned by the
// usedIdents collector (identifiers recorded at their emission sites), so
// this scan runs ONLY over the consumer-supplied Type/DecoderMappings
// expressions (usedIdents.opaque) — never over the generated body, whose
// string literals (wire names, paths) could false-positively match.
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
	return slices.Sorted(maps.Keys(m))
}
