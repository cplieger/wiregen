package wiregen

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"
)

// typeInfo holds resolved type data extracted from AST.
type typeInfo struct {
	Union  *unionInfo // set when Registry.Unions declares this type
	Name   string
	Doc    string
	Fields []fieldInfo
}

// fieldInfo is one field's wire plan, decided once from encoding/json's
// rules; every emitter reads it and none re-derives a rule.
type fieldInfo struct {
	// Elem is a slice's element or a map's value, planned like a field.
	Elem    *fieldInfo
	Bounds  *bounds  // from the field type's bound methods; nil: none
	Mapping *Mapping // the Registry.Mappings entry of a kindMapped value
	// Refusals are why this field cannot cross the wire; Generate fails
	// listing every one.
	Refusals []string
	WireName string
	GoName   string // the Go selector a refusal's path names, through embeds
	TSType   string
	Doc      string // JSDoc for the field
	// GoTypeName keys the registered struct, enum or Mappings entry a value
	// names.
	GoTypeName string
	// InRefusals are why encoding/json cannot decode this field, which
	// refuse it only in a type a request carries.
	InRefusals []string
	Depth      int
	// Ptrs counts the pointer levels above the value, each of which can be
	// nil. presence.go derives every null and absence rule from it.
	Ptrs int
	Kind wireKind
	// Omittable: encoding/json leaves the key out of an empty value
	// (omitzero, or omitempty on a kind it can find empty).
	Omittable bool
	// ViaEmbedPtr: promoted through an embedded pointer, whose fields
	// encoding/json skips when it is nil.
	ViaEmbedPtr bool
	Tagged      bool // wire name came from an explicit json tag
}

// wireKind is the one wire form encoding/json gives a value.
type wireKind uint8

const (
	kindRefused wireKind = iota // no checked shape; Refusals say why
	kindString
	kindNumber // a float, or json.Number
	kindInt
	kindUint
	kindBool
	kindQuoted // the ,string option: text holding the scalar's JSON encoding
	kindBytes  // base64 text
	kindTime   // RFC 3339 text
	kindRaw    // any JSON value
	kindEnum
	kindStruct // a registered struct
	kindMapped
	kindSlice
	kindMap
)

// refuse marks fi as unable to cross the wire for reason.
func (fi *fieldInfo) refuse(reason string) {
	fi.Kind = kindRefused
	fi.TSType = tsUnknown
	fi.Refusals = append(fi.Refusals, reason)
}

// astEngine loads Go packages and resolves type information from source.
type astEngine struct {
	byName   map[string]*typeInfo
	pkgs     map[string]*packages.Package
	gen      *generator
	types    []*typeInfo
	refusals []string
}

func newASTEngine(ctx context.Context, g *generator) (*astEngine, error) {
	e := &astEngine{gen: g, byName: make(map[string]*typeInfo)}

	pkgPaths := g.rootPackages()
	if len(pkgPaths) == 0 {
		return e, nil
	}

	pkgs, err := loadPackages(ctx, pkgPaths)
	if err != nil {
		// go/packages formats a driver failure into a fresh error without %w,
		// so a cancelled load loses context.Canceled from the chain on most
		// runs (measured: 13 of 20 on go1.27.0 + x/tools v0.49.0, the other 7
		// coming back from Load's own pre-check with the sentinel intact).
		// Report the cancellation cause when there is one, so a caller with a
		// deadline can tell "you ran out of time" from "your packages are
		// broken" with errors.Is instead of a substring match on the message.
		if cause := context.Cause(ctx); cause != nil {
			return nil, fmt.Errorf("wiregen: load packages: %w", cause)
		}
		return nil, fmt.Errorf("wiregen: load packages: %w", err)
	}
	if err := packagesError(pkgs); err != nil {
		return nil, err
	}
	allPkgs := indexPackages(pkgs)
	e.pkgs = allPkgs
	if err := g.discoverEnums(allPkgs); err != nil {
		return nil, err
	}

	for _, wt := range g.Types {
		ti, err := e.resolveType(wt, allPkgs)
		if err != nil {
			return nil, err
		}
		e.byName[wt.Name] = ti
		e.types = append(e.types, ti)
	}
	if err := e.resolveUnions(); err != nil {
		return nil, err
	}
	e.refuseUndecodable()
	if len(e.refusals) > 0 {
		slices.Sort(e.refusals)
		return nil, fmt.Errorf("wiregen: %d field(s) cannot cross the wire with a checked shape:\n  %s",
			len(e.refusals), strings.Join(e.refusals, "\n  "))
	}

	// Sort by TS name for deterministic output.
	slices.SortStableFunc(e.types, func(a, b *typeInfo) int {
		return cmp.Compare(g.tsName(a.Name), g.tsName(b.Name))
	})
	return e, nil
}

// refuseUndecodable refuses each field encoding/json cannot decode in a type
// a request carries: an endpoint's Request, Params or Query type, or one they
// reach.
func (e *astEngine) refuseUndecodable() {
	used := map[string]map[direction]bool{}
	for i := range e.gen.Endpoints {
		ep := &e.gen.Endpoints[i]
		for _, wt := range []WireType{ep.Request, ep.Params, ep.Query} {
			e.reach(used, wt, dirRequest)
		}
	}
	for _, ti := range e.types {
		if !used[ti.Name][dirRequest] {
			continue
		}
		for i := range ti.Fields {
			f := &ti.Fields[i]
			for _, why := range f.InRefusals {
				e.refusals = append(e.refusals, e.gen.typeNames[ti.Name]+"."+ti.Name+"."+f.GoName+": "+why+", and a request carries it")
			}
		}
	}
}

// reach records in used that wt, and every registered type its fields reach,
// crosses in direction d.
func (e *astEngine) reach(used map[string]map[direction]bool, wt WireType, d direction) {
	if wt == (WireType{}) || used[wt.Name][d] {
		return
	}
	if used[wt.Name] == nil {
		used[wt.Name] = map[direction]bool{}
	}
	used[wt.Name][d] = true
	ti := e.byName[wt.Name]
	for i := range ti.Fields {
		for f := &ti.Fields[i]; f != nil; f = f.Elem {
			if f.Kind == kindStruct {
				e.reach(used, WireType{PkgPath: e.gen.typeNames[f.GoTypeName], Name: f.GoTypeName}, d)
			}
		}
	}
}

// rootPackages returns the packages of every registered type and enum, in
// first-registration order.
func (g *generator) rootPackages() []string {
	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for _, wt := range g.Types {
		add(wt.PkgPath)
	}
	for _, ref := range g.Enums {
		add(ref.Type.PkgPath)
	}
	return paths
}

// loadPackages loads and type-checks paths. The engine only reads what it
// returns, which lets a test binary share one load across tests.
var loadPackages = func(ctx context.Context, paths []string) ([]*packages.Package, error) {
	return packages.Load(loadConfig(ctx), paths...)
}

// loadConfig is the go/packages config used to load and type-check the
// registered packages with CGO disabled. ctx bounds the load, which runs the
// go command as a subprocess: cancelling it aborts the load rather than
// leaving the caller with no way to stop it.
func loadConfig(ctx context.Context) *packages.Config {
	return &packages.Config{
		Context: ctx,
		Mode: packages.NeedSyntax | packages.NeedFiles | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedName | packages.NeedImports |
			packages.NeedDeps,
		Env: append(defaultEnv(), "CGO_ENABLED=0"),
	}
}

// packagesError aggregates every load/type error reported across pkgs, or
// returns nil. Reporting all errors at once lets a consumer fix a
// multi-package or multi-error misconfiguration in a single run.
func packagesError(pkgs []*packages.Package) error {
	var errs []error
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			msg := e.Msg
			if e.Pos != "" {
				msg = e.Pos + ": " + e.Msg
			}
			errs = append(errs, fmt.Errorf("wiregen: package %s: %s", pkg.PkgPath, msg))
		}
	}
	return errors.Join(errs...)
}

// indexPackages returns every package in the import graph rooted at pkgs,
// keyed by import path, so embedded/cross-package field types resolve.
func indexPackages(pkgs []*packages.Package) map[string]*packages.Package {
	all := make(map[string]*packages.Package)
	var visit func(*packages.Package)
	visit = func(p *packages.Package) {
		if _, ok := all[p.PkgPath]; ok {
			return
		}
		all[p.PkgPath] = p
		for _, imp := range p.Imports {
			visit(imp)
		}
	}
	for _, pkg := range pkgs {
		visit(pkg)
	}
	return all
}

// discoverEnums fills each registered enum's members from its own package,
// reporting every enum it cannot resolve.
func (g *generator) discoverEnums(allPkgs map[string]*packages.Package) error {
	var errs []error
	for _, ref := range g.Enums {
		values, err := enumMembers(ref, allPkgs[ref.Type.PkgPath])
		if err != nil {
			errs = append(errs, fmt.Errorf("wiregen: enum %s.%s: %w", ref.Type.PkgPath, ref.Type.Name, err))
			continue
		}
		g.enums[ref.Type.Name] = enumDef{pkgPath: ref.Type.PkgPath, values: values}
	}
	return errors.Join(errs...)
}

// enumMembers returns ref's wire members: the constants of exactly ref's type
// in pkg, in declaration order, each named by its value or by ref.Wire.
func enumMembers(ref EnumRef, pkg *packages.Package) ([]string, error) {
	if pkg == nil || pkg.Types == nil {
		return nil, errors.New("package not loaded")
	}
	tn, ok := pkg.Types.Scope().Lookup(ref.Type.Name).(*types.TypeName)
	if !ok {
		return nil, errors.New("no such type")
	}
	if err := checkEnumKind(ref, tn.Type()); err != nil {
		return nil, err
	}
	names, err := memberNames(ref, typedConsts(pkg.Types.Scope(), tn.Type()))
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New("declares no constant with a wire name")
	}
	return names, nil
}

// checkEnumKind rejects an enum type whose kind ref cannot name: a string
// enum names members by value, an integer enum through Wire.
func checkEnumKind(ref EnumRef, t types.Type) error {
	basic, ok := t.Underlying().(*types.Basic)
	switch {
	case ok && basic.Info()&types.IsString != 0:
		if ref.Wire != nil {
			return errors.New("a string enum names its members by value; Wire is for an integer enum")
		}
	case ok && basic.Info()&types.IsInteger != 0:
		if ref.Wire == nil {
			return errors.New("an integer enum needs Wire to name its members; register it with TextEnum")
		}
	default:
		return errors.New("an enum's underlying type must be a string or an integer")
	}
	switch c := marshalCodec(t); {
	case ref.Wire == nil && c != codecPlain:
		return errors.New("a string enum's members are its constants' values, but it " + c.why() + "; drop the method")
	case ref.Wire != nil && c == codecPlain:
		return errors.New("an integer enum needs a value-receiver MarshalText, or encoding/json writes it as a number")
	case ref.Wire != nil && c != codecText:
		return errors.New("an integer enum is named by its MarshalText, but it " + c.why())
	}
	return nil
}

// memberNames names consts on the wire, skipping a repeated value and a
// constant Wire refuses, and rejects two values encoding alike, which no
// decoder could tell apart.
func memberNames(ref EnumRef, consts []*types.Const) ([]string, error) {
	byName := map[string]string{}
	seenVal := map[string]bool{}
	var names []string
	for _, c := range consts {
		if seenVal[c.Val().ExactString()] {
			continue
		}
		seenVal[c.Val().ExactString()] = true
		name, member, err := wireName(ref, c)
		if err != nil {
			return nil, err
		}
		if !member {
			continue
		}
		if prev, dup := byName[name]; dup {
			return nil, fmt.Errorf("constants %s and %s both encode as %q", prev, c.Name(), name)
		}
		byName[name] = c.Name()
		names = append(names, name)
	}
	return names, nil
}

// typedConsts returns the constants of exactly type t declared at scope, in
// source order.
func typedConsts(scope *types.Scope, t types.Type) []*types.Const {
	var out []*types.Const
	for _, nm := range scope.Names() {
		if c, ok := scope.Lookup(nm).(*types.Const); ok && types.Identical(c.Type(), t) {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b *types.Const) int { return cmp.Compare(a.Pos(), b.Pos()) })
	return out
}

// wireName names constant c of ref's type on the wire; member is false for a
// constant ref.Wire refuses, which encoding/json could not encode either.
func wireName(ref EnumRef, c *types.Const) (name string, member bool, err error) {
	if ref.Wire == nil {
		return constant.StringVal(c.Val()), true, nil
	}
	v, exact := constant.Uint64Val(c.Val())
	if !exact {
		return "", false, fmt.Errorf("constant %s is negative", c.Name())
	}
	name, werr := ref.Wire(v)
	return name, werr == nil, nil
}

func (e *astEngine) resolveType(wt WireType, allPkgs map[string]*packages.Package) (*typeInfo, error) {
	pkg, ok := allPkgs[wt.PkgPath]
	if !ok {
		return nil, fmt.Errorf("wiregen: package %q not loaded, and type %s needs it", wt.PkgPath, wt.Name)
	}

	obj := pkg.Types.Scope().Lookup(wt.Name)
	if obj == nil {
		return nil, fmt.Errorf("wiregen: type %s not found in package %s", wt.Name, wt.PkgPath)
	}

	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("wiregen: %s in %s is not a type", wt.Name, wt.PkgPath)
	}

	ti := &typeInfo{Name: wt.Name}

	ti.Doc = commentToJSDoc(typeDoc(pkg, wt.Name))

	underlying := tn.Type().Underlying()
	st, ok := underlying.(*types.Struct)
	if !ok {
		return nil, fmt.Errorf("wiregen: registered type %s.%s is not a struct; register an enum with StringEnum or TextEnum, or map the type", wt.PkgPath, wt.Name)
	}
	if c := marshalCodec(tn.Type()); c != codecPlain {
		e.refusals = append(e.refusals, wt.PkgPath+"."+wt.Name+": is registered, but "+c.why()+"; map it with Mappings or drop the method")
	}
	ti.Fields = e.resolveStructFields(st, pkg, allPkgs, 0, nil)
	for i := range ti.Fields {
		for _, why := range ti.Fields[i].Refusals {
			e.refusals = append(e.refusals, wt.PkgPath+"."+wt.Name+"."+ti.Fields[i].GoName+": "+why)
		}
	}

	return ti, nil
}

func (e *astEngine) resolveStructFields(st *types.Struct, pkg *packages.Package, allPkgs map[string]*packages.Package, depth int, visited map[*types.Struct]bool) []fieldInfo {
	if visited == nil {
		visited = make(map[*types.Struct]bool)
	}
	if visited[st] {
		return nil
	}
	visited[st] = true
	// Scope visited to the current ancestry path: a struct on its own path is
	// a real cycle (skipped above), but a common base reached via two sibling
	// embeds (diamond) must be walked on both paths so its duplicated fields
	// collide at equal depth and drop in dedupJSONFields (encoding/json behavior).
	defer delete(visited, st)

	var raw []fieldInfo
	for i := range st.NumFields() {
		f := st.Field(i)
		tag := parseJSONTag(st.Tag(i))
		if f.Anonymous() {
			raw = append(raw, e.resolveEmbeddedField(f, tag, depth, pkg, allPkgs, visited)...)
			continue
		}
		if f.Exported() && !tag.skip {
			raw = append(raw, e.resolveNamedField(f, tag, depth, pkg, allPkgs))
		}
	}
	return dedupJSONFields(raw)
}

// jsonTag is a struct field's json tag as encoding/json's typeFields reads
// it.
type jsonTag struct {
	name      string // "" when absent or not a valid key name
	badName   string // a name isValidJSONName refuses
	skip      bool   // json:"-"
	omitEmpty bool
	omitZero  bool
	quoted    bool // the ,string option is present
}

func parseJSONTag(tag string) jsonTag {
	v := reflect.StructTag(tag).Get("json")
	if v == "-" {
		return jsonTag{skip: true}
	}
	name, opts, _ := strings.Cut(v, ",")
	t := jsonTag{name: name}
	if name != "" && !isValidJSONName(name) {
		t.name, t.badName = "", name
	}
	for o := range strings.SplitSeq(opts, ",") {
		switch o {
		case "omitempty":
			t.omitEmpty = true
		case "omitzero":
			t.omitZero = true
		case "string":
			t.quoted = true
		}
	}
	return t
}

// isValidJSONName is encoding/json's v1 isValidTag
// (https://cs.opensource.google/go/go/+/refs/tags/go1.27.2:src/encoding/json/encode.go;l=946).
// v1 ignores a name outside this set and keeps the Go name, while the
// encoder Go 1.27 builds by default (GOEXPERIMENT=jsonv2) reads it up to a
// reserved character, so Generate refuses such a name.
func isValidJSONName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c) && !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

// resolveEmbeddedField applies encoding/json's typeFields rules to an
// embedded field: an untagged struct embed, exported or not, is flattened
// into the parent; an unexported non-struct embed is skipped; any other embed
// is a field named by its tag or its type name.
func (e *astEngine) resolveEmbeddedField(f *types.Var, tag jsonTag, depth int, pkg *packages.Package, allPkgs map[string]*packages.Package, visited map[*types.Struct]bool) []fieldInfo {
	embType := types.Unalias(f.Type())
	ptr, viaPtr := embType.(*types.Pointer)
	if viaPtr {
		embType = types.Unalias(ptr.Elem())
	}
	embSt, isStruct := embType.Underlying().(*types.Struct)
	if (!f.Exported() && !isStruct) || tag.skip {
		return nil
	}
	if tag.name != "" || tag.badName != "" || !isStruct {
		return []fieldInfo{e.resolveNamedField(f, tag, depth, pkg, allPkgs)}
	}
	promoted := e.resolveStructFields(embSt, pkg, allPkgs, depth+1, visited)
	for i := range promoted {
		p := &promoted[i]
		p.GoName = f.Name() + "." + p.GoName
		p.ViaEmbedPtr = p.ViaEmbedPtr || viaPtr
		if viaPtr && !f.Exported() {
			p.InRefusals = append(p.InRefusals, "is promoted through the embedded pointer to unexported "+embType.String()+", which encoding/json cannot allocate to decode into")
		}
	}
	return promoted
}

func (e *astEngine) resolveNamedField(f *types.Var, tag jsonTag, depth int, pkg *packages.Package, allPkgs map[string]*packages.Package) fieldInfo {
	quoted := tag.quoted && quotable(f.Type())
	fi := e.resolveFieldType(f.Type(), depth)
	if quoted {
		fi.quote()
	}
	fi.WireName = cmp.Or(tag.name, f.Name())
	fi.Tagged = tag.name != ""
	fi.GoName = f.Name()
	fi.Doc = findFieldDoc(f, pkg, allPkgs)
	fi.Omittable = tag.omitZero || (tag.omitEmpty && omitemptyApplies(f.Type()))
	if tag.badName != "" {
		fi.Refusals = append(fi.Refusals, fmt.Sprintf("json tag name %q has a character outside letters, digits and !#$%%&()*+-./:;<=>?@[]^_{|}~ and space, which encoding/json versions read differently", tag.badName))
	}
	if tag.quoted && !quoted {
		fi.Refusals = append(fi.Refusals, "the ,string option applies only to a boolean, number or string with no marshaler, under at most one pointer; encoding/json ignores it here")
	}
	if !fi.Tagged && e.gen.o.requireJSONTags {
		fi.Refusals = append(fi.Refusals, "no json tag names it")
	}
	return fi
}

// quote plans fi under the ,string option, which encoding/json applies to
// the scalar it resolved to: the value crosses as the text of its JSON
// encoding, held to that encoding's grammar.
func (fi *fieldInfo) quote() {
	switch {
	case fi.Kind == kindMapped:
		fi.refuse("a Mappings entry states this field's wire form, which the ,string option rewrites as text; drop the option or the mapping")
	case fi.Bounds != nil:
		fi.refuse("is bounded, and a ,string field crosses as text its bounds do not describe")
	case fi.Kind != kindRefused:
		fi.Bounds = &bounds{pattern: quotedPatterns[fi.Kind]}
		fi.Kind, fi.TSType = kindQuoted, tsString
	}
}

// quotedPatterns are the texts encoding/json writes under ,string, by the
// quoted value's kind: the JSON grammar of a string, number or boolean
// (https://www.rfc-editor.org/rfc/rfc8259#section-6).
var quotedPatterns = map[wireKind]string{
	kindString: `^"(?:[^"\\\x00-\x1F]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"$`,
	kindEnum:   `^"(?:[^"\\\x00-\x1F]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"$`,
	kindNumber: `^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`,
	kindInt:    `^-?(?:0|[1-9][0-9]*)$`,
	kindUint:   `^(?:0|[1-9][0-9]*)$`,
	kindBool:   `^(?:true|false)$`,
}

// quotable reports whether encoding/json applies the ,string option to a
// field of type t: a boolean, number or string under at most one unnamed
// pointer, encoded without a marshaler.
func quotable(t types.Type) bool {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&(types.IsBoolean|types.IsInteger|types.IsFloat|types.IsString) != 0 &&
		b.Info()&types.IsUntyped == 0 && marshalCodec(t) == codecPlain
}

// omitemptyApplies reports whether encoding/json's omitempty can ever find a
// value of type t empty: never a struct, and an array only of length 0.
func omitemptyApplies(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Struct:
		return false
	case *types.Array:
		return u.Len() == 0
	}
	return true
}

// dedupJSONFields applies encoding/json's field-selection semantics to the raw
// promoted fields: for each wire name the shallowest depth wins, and a wire
// name tied at its shallowest depth by more than one field is dropped
// (ambiguous promotion). Declaration order is preserved.
func dedupJSONFields(raw []fieldInfo) []fieldInfo {
	type entry struct {
		field fieldInfo
		count int
	}
	best := make(map[string]*entry, len(raw))
	for i := range raw {
		fi := raw[i]
		ent, ok := best[fi.WireName]
		if !ok {
			best[fi.WireName] = &entry{field: fi, count: 1}
			continue
		}
		if fi.Depth < ent.field.Depth {
			ent.field = fi
			ent.count = 1
		} else if fi.Depth == ent.field.Depth {
			ent.field, ent.count = equalDepthWinner(&ent.field, ent.count, &fi)
		}
	}

	seen := make(map[string]bool, len(best))
	var result []fieldInfo
	for i := range raw {
		wireName := raw[i].WireName
		if seen[wireName] {
			continue
		}
		seen[wireName] = true
		if best[wireName].count == 1 {
			result = append(result, best[wireName].field)
		}
	}
	return result
}

// equalDepthWinner applies encoding/json's equal-depth promotion
// tiebreak: a tagged field dominates an untagged one; two fields that
// are both tagged or both untagged at the same depth are an ambiguous
// promotion (count is bumped so the caller drops the wire name).
// Returns the winning field and its updated tie count.
func equalDepthWinner(cur *fieldInfo, curCount int, cand *fieldInfo) (winner fieldInfo, count int) {
	switch {
	case cand.Tagged && !cur.Tagged:
		return *cand, 1 // tagged field dominates at equal depth
	case !cand.Tagged && cur.Tagged:
		return *cur, curCount // keep the tagged winner; not a real collision
	default:
		return *cur, curCount + 1
	}
}

func (e *astEngine) resolveFieldType(t types.Type, depth int) fieldInfo {
	fi := fieldInfo{Depth: depth}
	e.resolveInto(t, &fi)
	return fi
}

func (e *astEngine) resolveInto(t types.Type, fi *fieldInfo) {
	if isNamed(types.Unalias(t), "time", "Duration") {
		fi.refuse("a time.Duration encodes as integer nanoseconds; send int64 milliseconds")
		return
	}
	key := e.gen.mappingKey(t)
	if m, ok := e.gen.Mappings[key]; ok {
		fi.Kind, fi.TSType, fi.Mapping, fi.GoTypeName = kindMapped, m.TS, &m, key
		fi.needDecoder(t)
		return
	}
	switch ut := t.(type) {
	case *types.Alias:
		e.resolveInto(types.Unalias(ut), fi)
	case *types.Pointer:
		fi.Ptrs++
		e.resolveInto(ut.Elem(), fi)
	case *types.Named:
		e.resolveNamedType(ut, fi)
	case *types.Basic:
		fi.Kind, fi.TSType = basicKind(ut)
		if fi.Kind == kindRefused {
			fi.refuse("a " + ut.Name() + " has no JSON encoding")
		}
	case *types.Slice:
		e.resolveSliceType(ut, fi)
	case *types.Map:
		e.resolveMapType(ut, fi)
	case *types.Interface:
		fi.refuse("an interface or any has no checked shape; use json.RawMessage")
	case *types.Struct:
		fi.refuse("an inline anonymous struct; declare and register a named type")
	case *types.Array:
		fi.refuse("a fixed-size array; use a slice")
	default:
		fi.refuse("a " + t.String() + " has no JSON encoding")
	}
}

// resolveNamedType resolves a named type in encoding/json's order: the
// standard-library special cases, a registered enum (whose codec
// discoverEnums checked), a marshaler, a registered struct, otherwise its
// underlying type.
func (e *astEngine) resolveNamedType(ut *types.Named, fi *fieldInfo) {
	name := ut.Obj().Name()
	pkgPath := ""
	if ut.Obj().Pkg() != nil {
		pkgPath = ut.Obj().Pkg().Path()
	}
	fullName := pkgPath + "." + name

	if stdlibWireType(pkgPath, name, fi) {
		return
	}
	if en, ok := e.gen.enums[name]; ok && en.pkgPath == pkgPath {
		fi.Kind, fi.TSType, fi.GoTypeName = kindEnum, e.gen.tsEnumName(name), name
		fi.needDecoder(ut)
		return
	}
	// An interface's dynamic value picks its own encoding, MarshalText or not.
	if _, ok := ut.Underlying().(*types.Interface); ok {
		fi.refuse("interface " + fullName + " has no checked shape; use json.RawMessage")
		return
	}
	codec := marshalCodec(ut)
	if pkg, ok := e.gen.typeNames[name]; ok && pkg == pkgPath {
		if codec != codecPlain {
			fi.refuse(fullName + " is registered, but " + codec.why())
			return
		}
		fi.Kind, fi.TSType, fi.GoTypeName = kindStruct, e.gen.tsName(name), name
		return
	}
	switch codec {
	case codecText:
		fi.Kind, fi.TSType = kindString, tsString
		fi.needDecoder(ut)
	case codecPlain:
		if _, ok := ut.Underlying().(*types.Struct); ok {
			fi.refuse("struct " + fullName + " is neither registered nor mapped")
			return
		}
		e.resolveInto(ut.Underlying(), fi)
	default:
		fi.refuse(fullName + " " + codec.why() + "; map it with Mappings")
		return
	}
	e.applyBounds(ut, fullName, fi)
}

// applyBounds gives fi the bounds named type t declares, refusing any it
// cannot read or that does not fit fi's shape.
func (e *astEngine) applyBounds(t *types.Named, fullName string, fi *fieldInfo) {
	b, refusals := e.namedBounds(t)
	for _, why := range refusals {
		fi.refuse(fullName + " " + why)
	}
	if b == nil {
		return
	}
	if why := b.misfit(fi); why != "" {
		fi.refuse(fullName + " " + why)
	}
	fi.Bounds = b
}

// needDecoder records an InRefusal when encoding/json cannot decode what
// it encodes for t through a marshaler: decoding looks for the matching
// unmarshaler on *t (json.Unmarshaler, or encoding.TextUnmarshaler for text).
func (fi *fieldInfo) needDecoder(t types.Type) {
	ptr := types.NewPointer(t)
	switch marshalCodec(t) {
	case codecJSON:
		if !types.Implements(ptr, jsonUnmarshaler) {
			fi.InRefusals = append(fi.InRefusals, t.String()+" implements json.Marshaler but not json.Unmarshaler, so encoding/json cannot decode it")
		}
	case codecText:
		if !types.Implements(ptr, jsonUnmarshaler) && !types.Implements(ptr, textUnmarshaler) {
			fi.InRefusals = append(fi.InRefusals, t.String()+" implements encoding.TextMarshaler but not encoding.TextUnmarshaler, so encoding/json cannot decode it")
		}
	}
}

// stdlibWireType resolves the standard-library types encoding/json encodes
// specially into fi, reporting whether pkgPath.name is one of them.
func stdlibWireType(pkgPath, name string, fi *fieldInfo) bool {
	switch pkgPath + "." + name {
	case "time.Time":
		fi.Kind, fi.TSType = kindTime, tsString
	// Both identities are recognized because the stdlib re-homes the type:
	// through Go 1.26 it is `type RawMessage []byte` in encoding/json, and from
	// Go 1.27 it is `type RawMessage = jsontext.Value`, an alias go/types
	// resolves past its name, so a check keyed only on "encoding/json" would
	// send the field down the named-[]byte path to a TS `string`.
	case "encoding/json.RawMessage", "encoding/json/jsontext.Value":
		fi.Kind, fi.TSType = kindRaw, tsUnknown
	case "encoding/json.Number":
		fi.Kind, fi.TSType = kindNumber, tsNumber
	default:
		return false
	}
	return true
}

// isNamed reports whether t is the named type pkgPath.name.
func isNamed(t types.Type, pkgPath, name string) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkgPath && n.Obj().Name() == name
}

// codec is how encoding/json encodes a type: through its fields or value, or
// through a marshaler method.
type codec uint8

const (
	codecPlain   codec = iota
	codecText          // a value-receiver MarshalText: a JSON string
	codecJSON          // MarshalJSON on either receiver: any shape
	codecPtrText       // MarshalText on the pointer receiver only
)

func (c codec) why() string {
	switch c {
	case codecText:
		return "implements encoding.TextMarshaler, so encoding/json writes it as a string, not its fields"
	case codecJSON:
		return "implements json.Marshaler, so encoding/json writes its MarshalJSON output, whose shape is unchecked"
	case codecPtrText:
		return "has a pointer-receiver MarshalText, which encoding/json calls only for addressable values, so one field encodes two ways"
	}
	return ""
}

// marshalerIface is the interface of one `name() ([]byte, error)` method,
// which json.Marshaler and encoding.TextMarshaler each are. Building it here
// keeps the check off whether the loaded packages import either.
func marshalerIface(name string) *types.Interface {
	results := types.NewTuple(
		types.NewVar(token.NoPos, nil, "", types.NewSlice(types.Typ[types.Byte])),
		types.NewVar(token.NoPos, nil, "", types.Universe.Lookup("error").Type()),
	)
	sig := types.NewSignatureType(nil, nil, nil, nil, results, false)
	return types.NewInterfaceType([]*types.Func{types.NewFunc(token.NoPos, nil, name, sig)}, nil).Complete()
}

// unmarshalerIface is the interface of one `name([]byte) error` method,
// which json.Unmarshaler and encoding.TextUnmarshaler each are.
func unmarshalerIface(name string) *types.Interface {
	params := types.NewTuple(types.NewVar(token.NoPos, nil, "", types.NewSlice(types.Typ[types.Byte])))
	results := types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Universe.Lookup("error").Type()))
	sig := types.NewSignatureType(nil, nil, nil, params, results, false)
	return types.NewInterfaceType([]*types.Func{types.NewFunc(token.NoPos, nil, name, sig)}, nil).Complete()
}

var (
	jsonMarshaler   = marshalerIface("MarshalJSON")
	textMarshaler   = marshalerIface("MarshalText")
	jsonUnmarshaler = unmarshalerIface("UnmarshalJSON")
	textUnmarshaler = unmarshalerIface("UnmarshalText")
)

// marshalCodec classifies t by encoding/json's marshaler precedence
// (newTypeEncoder): json.Marshaler on the value or pointer receiver first,
// then encoding.TextMarshaler. Promoted methods count, as they do there.
func marshalCodec(t types.Type) codec {
	ptr := types.NewPointer(t)
	switch {
	case types.Implements(t, jsonMarshaler) || types.Implements(ptr, jsonMarshaler):
		return codecJSON
	case types.Implements(t, textMarshaler):
		return codecText
	case types.Implements(ptr, textMarshaler):
		return codecPtrText
	}
	return codecPlain
}

// A slice of a uint8 kind without a marshaler is base64 text, as in
// encoding/json's newSliceEncoder.
func (e *astEngine) resolveSliceType(ut *types.Slice, fi *fieldInfo) {
	elem := ut.Elem()
	if b, ok := elem.Underlying().(*types.Basic); ok && b.Kind() == types.Uint8 && marshalCodec(elem) == codecPlain {
		fi.Kind, fi.TSType = kindBytes, tsString
		return
	}
	elemFI := e.resolveFieldType(elem, 0)
	ts := elemFI.elemTS()
	if elemFI.nullElem() {
		ts = "(" + ts + ")"
	}
	fi.Kind, fi.TSType, fi.Elem = kindSlice, ts+"[]", &elemFI
	fi.inheritRefusals("element", &elemFI)
}

func (e *astEngine) resolveMapType(ut *types.Map, fi *fieldInfo) {
	valFI := e.resolveFieldType(ut.Elem(), 0)
	fi.Kind, fi.TSType, fi.Elem = kindMap, "{ [key: string]: "+valFI.elemTS()+" }", &valFI
	fi.inheritRefusals("map value", &valFI)
	key := ut.Key()
	b, basic := key.Underlying().(*types.Basic)
	switch {
	case basic && b.Info()&types.IsString != 0:
	case !types.Implements(key, textMarshaler) && (!basic || b.Info()&types.IsInteger == 0):
		fi.Refusals = append(fi.Refusals, "map key "+key.String()+" is not a string, an integer or an encoding.TextMarshaler")
	case types.Implements(key, textMarshaler) && !types.Implements(types.NewPointer(key), textUnmarshaler):
		// encoding/json writes a non-string key through MarshalText and reads
		// one back only through UnmarshalText.
		fi.InRefusals = append(fi.InRefusals, "map key "+key.String()+" implements encoding.TextMarshaler but not encoding.TextUnmarshaler, so encoding/json cannot decode it")
	}
}

// inheritRefusals carries a collection element's refusals onto its field.
func (fi *fieldInfo) inheritRefusals(role string, elem *fieldInfo) {
	for _, why := range elem.Refusals {
		fi.Refusals = append(fi.Refusals, role+": "+why)
	}
	for _, why := range elem.InRefusals {
		fi.InRefusals = append(fi.InRefusals, role+": "+why)
	}
}

func findFieldDoc(fieldObj *types.Var, fallback *packages.Package, allPkgs map[string]*packages.Package) string {
	pos := fieldObj.Pos()
	if !pos.IsValid() {
		return ""
	}
	// Search the package where the field is declared (handles fields embedded
	// from another package); fall back to the type's own package.
	pkg := fallback
	if fieldObj.Pkg() != nil {
		if p, ok := allPkgs[fieldObj.Pkg().Path()]; ok {
			pkg = p
		}
	}
	if pkg == nil {
		return ""
	}
	for _, f := range pkg.Syntax {
		if doc := fieldDocAtPos(f, pos); doc != "" {
			return doc
		}
	}
	return ""
}

// fieldDocAtPos returns the JSDoc for the struct field whose name identifier is
// at pos. Position-scoping ties the doc to the exact field declaration, so a
// field doc is never taken from a different same-named field elsewhere in the
// package.
func fieldDocAtPos(file *ast.File, pos token.Pos) string {
	var result string
	ast.Inspect(file, func(n ast.Node) bool {
		if result != "" {
			return false
		}
		field, ok := n.(*ast.Field)
		if !ok {
			return true
		}
		for _, name := range field.Names {
			if name.Pos() == pos {
				if field.Doc != nil {
					result = commentToJSDoc(field.Doc)
				}
				return false
			}
		}
		return true
	})
	return result
}

// typeDoc returns the doc comment of the type declaration name in pkg: its
// spec's, else its declaration group's, or nil.
func typeDoc(pkg *packages.Package, name string) *ast.CommentGroup {
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == name {
					return cmp.Or(ts.Doc, gd.Doc)
				}
			}
		}
	}
	return nil
}

// commentText returns the human text of a single comment line/block, with ok
// false for a Go pragma, which must not flow into JSDoc.
func commentText(c *ast.Comment) (string, bool) {
	text := c.Text
	trimmed := strings.TrimSpace(strings.TrimPrefix(text, "//"))
	if strings.HasPrefix(trimmed, "nolint") || strings.HasPrefix(trimmed, "go:") {
		return "", false
	}
	if strings.HasPrefix(text, "//") {
		return strings.TrimPrefix(text, "// "), true
	}
	if inner, ok := strings.CutPrefix(text, "/*"); ok {
		return strings.TrimSpace(strings.TrimSuffix(inner, "*/")), true
	}
	return "", false
}

func commentToJSDoc(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	var nonEmpty []string
	for _, c := range cg.List {
		line, ok := commentText(c)
		if !ok {
			continue
		}
		// Drop empty (or slash-only) lines.
		if strings.TrimPrefix(line, "/") == "" {
			continue
		}
		// Replace */ with *\/ to prevent a premature JSDoc close.
		nonEmpty = append(nonEmpty, strings.ReplaceAll(line, "*/", "*\\/"))
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	if len(nonEmpty) == 1 {
		return "/** " + nonEmpty[0] + " */\n"
	}
	var b strings.Builder
	b.WriteString("/**\n")
	for _, l := range nonEmpty {
		b.WriteString(" * " + l + "\n")
	}
	b.WriteString(" */\n")
	return b.String()
}

func typeKey(t types.Type) string {
	switch ut := t.(type) {
	case *types.Alias:
		return typeKey(types.Unalias(ut))
	case *types.Named:
		if ut.Obj().Pkg() != nil {
			return ut.Obj().Pkg().Path() + "." + ut.Obj().Name()
		}
		return ut.Obj().Name()
	case *types.Basic:
		return ut.Name()
	default:
		return t.String()
	}
}

// mappingKey returns the Mappings key for t: its resolved importpath.Type,
// or for an alias the alias's own spelling when Mappings holds that. go/types
// resolves an alias past its name, and the stdlib re-homes types behind
// aliases (json.RawMessage became an alias of jsontext.Value in Go 1.27), so
// a mapping keyed by the spelling the consumer's source shows must still match.
func (g *generator) mappingKey(t types.Type) string {
	a, ok := t.(*types.Alias)
	if !ok {
		return typeKey(t)
	}
	if alias := aliasKey(a); alias != "" {
		if _, ok := g.Mappings[alias]; ok {
			return alias
		}
	}
	return typeKey(t)
}

// aliasKey returns the importpath.Type spelling of an alias declaration, or ""
// for a universe alias (any, byte, rune), which has no package to name.
func aliasKey(a *types.Alias) string {
	obj := a.Obj()
	if obj.Pkg() == nil {
		return ""
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

func basicKind(b *types.Basic) (kind wireKind, ts string) {
	info := b.Info()
	switch {
	case info&types.IsBoolean != 0:
		return kindBool, tsBoolean
	case info&types.IsString != 0:
		return kindString, tsString
	case info&types.IsUnsigned != 0:
		return kindUint, tsNumber
	case info&types.IsInteger != 0:
		return kindInt, tsNumber
	case info&types.IsFloat != 0:
		return kindNumber, tsNumber
	}
	return kindRefused, tsUnknown
}

func defaultEnv() []string {
	env := os.Environ()
	// Filter out CGO_ENABLED if already set
	var filtered []string
	for _, e := range env {
		if !strings.HasPrefix(e, "CGO_ENABLED=") {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
