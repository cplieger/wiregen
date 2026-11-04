// Package wiregen generates TypeScript types, validating decoders, a typed
// HTTP client and an OpenAPI 3.1 document from Go types and an endpoint table,
// reading the registered packages with go/packages and go/types. A consumer
// fills a [Registry] and calls [Registry.Generate] from a build-time command.
package wiregen

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"maps"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

// WireType names a registered Go type by package path and name.
type WireType struct {
	PkgPath string
	Name    string
}

// TypeRef names T, dereferencing one pointer level. A missing type is a
// compile error at the call site.
func TypeRef[T any]() WireType {
	return TypeOf(reflect.TypeFor[T]())
}

// TypeOf names t, dereferencing one pointer level. A nil, unnamed or
// predeclared type yields a WireType with no package, which
// [Registry.Generate] refuses.
func TypeOf(t reflect.Type) WireType {
	if t == nil {
		return WireType{}
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() == "" {
		return WireType{Name: t.String()}
	}
	return WireType{PkgPath: t.PkgPath(), Name: t.Name()}
}

// WireConst is a named integer constant emitted into the constants file.
type WireConst struct {
	TSName string
	Value  int
}

// Registry is the wire contract: the types, enums, unions, endpoints and
// events to generate from. Generate reads it and never modifies it. Every
// type an endpoint, an event, a union or ErrorEnvelope names is generated as
// if listed in Types.
type Registry struct {
	// ErrorEnvelope is every non-2xx JSON body; zero when none is declared.
	ErrorEnvelope WireType
	// OpenAPI is the document's info and security schemes, read when
	// [WithOpenAPIFiles] names an output.
	OpenAPI OpenAPIInfo
	// TSNameOverride renames a registered type's TS identifier, keyed by its
	// bare Go name.
	TSNameOverride map[string]string
	// EnumTSName renames a registered enum's TS identifier, keyed by its bare
	// Go name.
	EnumTSName map[string]string
	// PathNameOverride replaces the snake_case name a decoder's error paths
	// use for a type, keyed by its TS name.
	PathNameOverride map[string]string
	// Mappings replaces wiregen's reading of a Go type, keyed by its
	// importpath.Name, with the consumer's own TS type, decoder and schema.
	Mappings  map[string]Mapping
	Enums     []EnumRef
	Types     []WireType
	Unions    []Union
	Constants []WireConst
	Endpoints []Endpoint
	// Events are the frames of the event-stream endpoints, by name.
	Events []Event
}

// Mapping is a Go type's wire form, written by the consumer for a type whose
// encoding wiregen cannot read (a json.Marshaler, say).
type Mapping struct {
	// Schema is the value's JSON Schema; required when OpenAPI files are
	// written. A $ref in it must be a string naming something in the document.
	Schema map[string]any
	// TS is the TS type expression the value has.
	TS string
	// Decoder is a TS expression called as (obj, key, path) that returns the
	// field's value or throws; empty casts the value unchecked.
	Decoder string
}

// EnumRef registers an enum by its type. Generate lists as members the
// constants of exactly that type declared in its own package, which it loads
// whether or not a registered type lives there. A string enum's members are
// its constants' values; an integer enum's are the names Wire gives them, and
// a constant Wire fails on (an invalid zero, say) is no member.
type EnumRef struct {
	// Wire names an integer enum's value on the wire; nil for a string enum.
	Wire func(value uint64) (string, error)
	Type WireType
}

// StringEnum registers T, whose constants' values are its wire names.
func StringEnum[T ~string]() EnumRef { return EnumRef{Type: TypeRef[T]()} }

// TextEnum registers T, naming each constant through T's own MarshalText, so
// the wire spelling has one source: the owner's codec.
func TextEnum[T interface {
	~uint8
	encoding.TextMarshaler
}]() EnumRef {
	return EnumRef{Type: TypeRef[T](), Wire: func(value uint64) (string, error) {
		if value > math.MaxUint8 {
			return "", fmt.Errorf("value %d overflows uint8", value)
		}
		b, err := T(value).MarshalText()
		return string(b), err
	}}
}

// Option configures one [Registry.Generate] call. Every option takes its
// value; the last of two same options wins.
type Option func(*options)

type options struct {
	tsDir               string
	validatorsImport    string
	transportImport     string
	typesImportPath     string
	headerComment       string
	typesFilename       string
	decodersFilename    string
	eventsFilename      string
	constantsFilename   string
	clientFilename      string
	validatorsFilename  string
	arbitrariesFilename string
	arbitrariesImport   string
	openAPIFiles        []string
	requireJSONTags     bool
}

// WithTSDir names the directory every TS file is written under; required.
// Every TS file option names a path inside it, which may hold
// subdirectories; Generate refuses an absolute path or one leaving it, and
// writes through an [os.Root] there, so no symlink can redirect a file out.
func WithTSDir(dir string) Option { return func(o *options) { o.tsDir = dir } }

// WithValidatorsFile names the validators module wiregen writes on every run,
// inside the TS directory (default "validators.gen.ts"). The module is
// generated output: never hand-edit it.
func WithValidatorsFile(name string) Option {
	return func(o *options) { o.validatorsFilename = name }
}

// WithValidatorsImport sets the specifier every generated module imports the
// validators module by. The default is the relative specifier from each
// importing file, which is right unless the module is reached through a
// bundler alias.
func WithValidatorsImport(module string) Option {
	return func(o *options) { o.validatorsImport = module }
}

// WithArbitrariesFile makes Generate write the property-test arbitraries
// module at name, inside the TS directory: one fast-check arbitrary per
// registered type and enum, plus the ARBITRARY_BY_TYPE table. Empty (the
// default) writes no such file.
func WithArbitrariesFile(name string) Option {
	return func(o *options) { o.arbitrariesFilename = name }
}

// WithArbitrariesImport sets the specifier the arbitraries module imports
// fast-check's default export from (default "fast-check").
func WithArbitrariesImport(module string) Option {
	return func(o *options) { o.arbitrariesImport = module }
}

// WithTransportImport sets the specifier of the transport module the
// generated client calls into; required when endpoints are registered. The
// module's contract is docs/http-client.md "Client-transport module".
func WithTransportImport(module string) Option {
	return func(o *options) { o.transportImport = module }
}

// WithClientFilename names the generated client file (default
// "client.gen.ts").
func WithClientFilename(name string) Option { return func(o *options) { o.clientFilename = name } }

// WithTypesImportPath sets the specifier every generated module imports the
// types file by (default: the relative specifier from each importing file).
func WithTypesImportPath(module string) Option {
	return func(o *options) { o.typesImportPath = module }
}

// WithHeaderComment sets the comment heading every generated file (default
// "// Code generated by wiregen. DO NOT EDIT.").
func WithHeaderComment(comment string) Option {
	return func(o *options) { o.headerComment = comment }
}

// WithRequireJSONTags makes an exported field without a json tag name a
// generation error naming its path. Without it such a field takes the name
// encoding/json gives it, its Go name.
func WithRequireJSONTags() Option { return func(o *options) { o.requireJSONTags = true } }

// WithOpenAPIFiles makes Generate write the OpenAPI 3.1 document of the
// registry to each path, relative to the working directory and outside the
// TS directory's confinement, in the same pass as the TS files.
// Registry.OpenAPI must name a title and a version.
func WithOpenAPIFiles(paths ...string) Option {
	return func(o *options) { o.openAPIFiles = slices.Clone(paths) }
}

// Filenames names four generated files whose names are same-typed and
// adjacent, so a struct labels each at the call site. An empty field keeps
// that file's default name.
type Filenames struct {
	Types     string // default "types.gen.ts"
	Decoders  string // default "decoders.gen.ts"
	Events    string // default "events.gen.ts", written when Events are registered
	Constants string // default "constants.gen.ts"
}

// WithFilenames overrides the names of the types, decoders, events and
// constants files, inside the TS directory.
func WithFilenames(names Filenames) Option {
	return func(o *options) {
		o.typesFilename = names.Types
		o.decodersFilename = names.Decoders
		o.eventsFilename = names.Events
		o.constantsFilename = names.Constants
	}
}

// defaultHeaderComment heads every generated file when no WithHeaderComment
// is given.
const defaultHeaderComment = "// Code generated by wiregen. DO NOT EDIT.\n\n"

// resolve fills each unset option with its default.
func (o *options) resolve() {
	defaults := []struct {
		field *string
		value string
	}{
		{&o.headerComment, defaultHeaderComment},
		{&o.typesFilename, "types.gen.ts"},
		{&o.decodersFilename, "decoders.gen.ts"},
		{&o.eventsFilename, "events.gen.ts"},
		{&o.constantsFilename, "constants.gen.ts"},
		{&o.clientFilename, "client.gen.ts"},
		{&o.validatorsFilename, "validators.gen.ts"},
		{&o.arbitrariesImport, "fast-check"},
	}
	for _, d := range defaults {
		if *d.field == "" {
			*d.field = d.value
		}
	}
}

// moduleSpecifier returns the relative ES module specifier by which the
// generated file from imports the generated file to, both named inside the
// TS directory, rewriting the extension the way TypeScript's emit does: .mts
// to .mjs, .cts to .cjs, anything else to .js.
func moduleSpecifier(from, to string) string {
	rel, err := filepath.Rel(filepath.Dir(filepath.Clean(from)), filepath.Clean(to))
	if err != nil {
		rel = to // both are local paths, so Rel cannot fail
	}
	base, ext, _ := strings.CutLast(filepath.ToSlash(rel), ".")
	out := ".js"
	switch ext {
	case "mts":
		out = ".mjs"
	case "cts":
		out = ".cjs"
	}
	if !strings.HasPrefix(base, "../") {
		base = "./" + base
	}
	return base + out
}

// generator is one Generate call's state: the registry, cloned so nothing the
// caller holds is written, the resolved options, and the derived tables.
type generator struct {
	// typeNames and enums map each registered bare Go name to its package;
	// enums also holds the members the engine discovered.
	typeNames map[string]string
	enums     map[string]enumDef
	files     []genFile // every output, content unrendered
	Registry
	o options
}

// enumDef is one registered enum's package and wire members, in declaration
// order.
type enumDef struct {
	pkgPath string
	values  []string
}

func newGenerator(r *Registry, opts []Option) (*generator, error) {
	g := &generator{Registry: *r}
	for _, fn := range opts {
		if fn != nil {
			fn(&g.o)
		}
	}
	g.o.resolve()
	g.TSNameOverride = maps.Clone(r.TSNameOverride)
	g.EnumTSName = maps.Clone(r.EnumTSName)
	g.PathNameOverride = maps.Clone(r.PathNameOverride)
	g.Mappings = maps.Clone(r.Mappings)
	g.Unions = slices.Clone(r.Unions)
	g.Enums = slices.Clone(r.Enums)
	g.Types = slices.Clone(r.Types)
	g.Endpoints = slices.Clone(r.Endpoints)
	g.Events = slices.Clone(r.Events)
	if err := g.validate(); err != nil {
		return nil, err
	}
	return g, nil
}

// validate rejects a registry or option set no file can be generated from.
// It rejects a duplicate bare type name because the engine keys types by it.
func (g *generator) validate() error {
	if g.o.tsDir == "" {
		return errors.New("wiregen: WithTSDir must be set")
	}
	if err := g.registerNames(); err != nil {
		return err
	}
	if len(g.Endpoints) > 0 && g.o.transportImport == "" {
		return errors.New("wiregen: WithTransportImport must be set when endpoints are registered")
	}
	if err := g.validateConstants(); err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(g.Mappings)) {
		if g.Mappings[key].TS == "" {
			return fmt.Errorf("wiregen: mapping %s has no TS type", key)
		}
	}
	if err := g.validateEvents(); err != nil {
		return err
	}
	for _, u := range g.Unions {
		if u.Type == (WireType{}) {
			return fmt.Errorf("wiregen: union with tag %q names no Type", u.Tag)
		}
	}
	files, err := g.outputs()
	if err != nil {
		return err
	}
	g.files = files
	taken, err := g.claimIdents()
	if err != nil {
		return err
	}
	if err := g.validateEndpoints(taken); err != nil {
		return err
	}
	return g.validateOpenAPI()
}

// typeNames and enums share one bare-name space.
func (g *generator) registerNames() error {
	seen, err := g.registerTypes()
	if err != nil {
		return err
	}
	g.enums = make(map[string]enumDef, len(g.Enums))
	for _, ref := range g.Enums {
		if err := registerName(seen, "enum", ref.Type); err != nil {
			return err
		}
		g.enums[ref.Type.Name] = enumDef{pkgPath: ref.Type.PkgPath}
	}
	return nil
}

// registerTypes fills typeNames from Types, then appends each type the
// endpoints, events, unions and ErrorEnvelope name that Types lacks. It returns the
// bare names taken, for the enums to share.
func (g *generator) registerTypes() (map[string]string, error) {
	g.typeNames = make(map[string]string, len(g.Types))
	seen := make(map[string]string, len(g.Types)+len(g.Enums))
	for _, wt := range g.Types {
		if err := registerName(seen, "type", wt); err != nil {
			return nil, err
		}
		g.typeNames[wt.Name] = wt.PkgPath
	}
	for _, ref := range g.referencedTypes() {
		if pkg, ok := g.typeNames[ref.wt.Name]; ref.wt == (WireType{}) || (ok && pkg == ref.wt.PkgPath) {
			continue
		}
		if err := registerName(seen, ref.role, ref.wt); err != nil {
			return nil, err
		}
		g.typeNames[ref.wt.Name] = ref.wt.PkgPath
		g.Types = append(g.Types, ref.wt)
	}
	return seen, nil
}

// typeReference is a type the registry names outside Types, with the role
// an error about it names.
type typeReference struct {
	role string
	wt   WireType
}

// referencedTypes lists the types the endpoints, events, unions and
// ErrorEnvelope name, in registry order.
func (g *generator) referencedTypes() []typeReference {
	var refs []typeReference
	for i := range g.Endpoints {
		e := &g.Endpoints[i]
		for _, f := range []struct {
			role string
			wt   WireType
		}{{"Params", e.Params}, {"Query", e.Query}, {"Request", e.Request}, {"Response", e.Response}} {
			refs = append(refs, typeReference{role: "endpoint " + e.Name + " " + f.role + " type", wt: f.wt})
		}
	}
	for _, ev := range g.Events {
		refs = append(refs, typeReference{role: fmt.Sprintf("event %q data type", ev.Name), wt: ev.Data})
	}
	for _, u := range g.Unions {
		refs = append(refs, typeReference{role: "union type", wt: u.Type})
	}
	return append(refs, typeReference{role: "ErrorEnvelope type", wt: g.ErrorEnvelope})
}

// eventsIdents are the names the events module exports.
var eventsIdents = []string{"EventFrame", "EventName", "EVENT_DECODERS", "decodeEventFrame"}

// validateEvents rejects an event with no data, a duplicate name, or a name
// an SSE event field cannot carry (https://html.spec.whatwg.org/multipage/server-sent-events.html#parsing-an-event-stream).
func (g *generator) validateEvents() error {
	if len(g.Events) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, ev := range g.Events {
		if ev.Name == "" || strings.ContainsAny(ev.Name, "\r\n") || !utf8.ValidString(ev.Name) {
			return fmt.Errorf("wiregen: event name %q must be non-empty valid UTF-8 with no line break", ev.Name)
		}
		if seen[ev.Name] {
			return fmt.Errorf("wiregen: event %q is registered twice", ev.Name)
		}
		seen[ev.Name] = true
		if ev.Data == (WireType{}) {
			return fmt.Errorf("wiregen: event %q has no Data type", ev.Name)
		}
	}
	return nil
}

// registerName records wt's bare name in seen, keyed to its package, and
// rejects a type with no package or name, or a bare name registered twice:
// wiregen keys types and enums by bare name.
func registerName(seen map[string]string, role string, wt WireType) error {
	if wt.PkgPath == "" || wt.Name == "" {
		return fmt.Errorf("wiregen: a registered %s %+v names no package-level type; register a named type", role, wt)
	}
	if prev, dup := seen[wt.Name]; dup {
		if prev == wt.PkgPath {
			return fmt.Errorf("wiregen: %s.%s is registered twice", wt.PkgPath, wt.Name)
		}
		return fmt.Errorf("wiregen: name %q is registered from two packages, %s and %s. wiregen keys types and enums by bare name, so rename one or register only one", wt.Name, prev, wt.PkgPath)
	}
	seen[wt.Name] = wt.PkgPath
	return nil
}

// Generate writes every generated file, bounded by ctx, which the package
// load runs under. Before creating anything it refuses two outputs that are
// one file, two generated identifiers of one name, a $ref that is no string
// or names nothing in the OpenAPI document and, listing every field path, a
// field with no checked wire shape (docs/type-mapping.md). Every file is
// staged before any replaces its target; each replacement is atomic, a
// failure among them can leave a prefix replaced, and no temp outlives it.
func (r *Registry) Generate(ctx context.Context, opts ...Option) error {
	g, files, err := r.render(ctx, opts)
	if err != nil {
		return err
	}
	return writeFiles(ctx, g.o.tsDir, files)
}

func (r *Registry) render(ctx context.Context, opts []Option) (*generator, []genFile, error) {
	g, err := newGenerator(r, opts)
	if err != nil {
		return nil, nil, err
	}
	engine, err := newASTEngine(ctx, g)
	if err != nil {
		return nil, nil, err
	}
	args, err := g.validateEndpointTypes(engine)
	if err != nil {
		return nil, nil, err
	}
	files, err := g.buildGenFiles(engine, args)
	return g, files, err
}

func (g *generator) buildGenFiles(engine *astEngine, args map[string]*endpointArgs) ([]genFile, error) {
	var doc string
	if len(g.o.openAPIFiles) > 0 {
		var err error
		if doc, err = g.renderOpenAPI(engine); err != nil {
			return nil, err
		}
	}
	files := slices.Clone(g.files)
	render := map[string]func(*strings.Builder){
		"types":       func(b *strings.Builder) { g.generateTypes(b, engine) },
		"decoders":    func(b *strings.Builder) { g.generateDecoders(b, engine) },
		"validators":  g.generateValidators,
		"events":      g.generateEvents,
		"constants":   g.generateConstants,
		"client":      func(b *strings.Builder) { g.generateClient(b, args) },
		"arbitraries": func(b *strings.Builder) { g.generateArbitraries(b, engine) },
		roleOpenAPI:   func(b *strings.Builder) { b.WriteString(doc) },
	}
	for i := range files {
		var b strings.Builder
		render[files[i].role](&b)
		files[i].content = b.String()
	}
	return files, nil
}

// validateConstants rejects a constant whose TSName has no TS-identifier
// character, which would emit an empty name.
func (g *generator) validateConstants() error {
	for _, c := range g.Constants {
		if sanitizeTSIdent(c.TSName) == "" {
			return fmt.Errorf("wiregen: constant %q with value %d has no TS-identifier-safe characters in its TSName", c.TSName, c.Value)
		}
	}
	return nil
}
