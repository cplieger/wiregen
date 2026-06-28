package wiregen

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// typeInfo holds resolved type data extracted from AST.
type typeInfo struct {
	Union  *UnionDef
	Name   string
	Doc    string
	Fields []fieldInfo
	IsEnum bool
}

// fieldInfo holds one struct field's metadata.
type fieldInfo struct {
	WireName   string
	TSType     string
	Doc        string // JSDoc for the field
	GoTypeName string // for cross-ref resolution
	SliceElem  string
	MapVal     string
	Depth      int
	Optional   bool
	JSONString bool
	Tagged     bool // wire name came from an explicit json tag
	IsSlice    bool
	IsMap      bool
	IsStruct   bool
	IsEnum     bool
	IsRaw      bool
	IsIface    bool
}

// astEngine loads Go packages and resolves type information from source.
type astEngine struct {
	byName map[string]*typeInfo
	r      *Registry
	types  []*typeInfo
}

func newASTEngine(r *Registry) (*astEngine, error) {
	e := &astEngine{r: r, byName: make(map[string]*typeInfo)}

	// No types registered — return an empty engine (constants-only / registry-only use).
	if len(r.Types) == 0 && len(r.PackagePaths) == 0 {
		return e, nil
	}
	pkgPaths := r.resolvePackagePaths()
	if len(pkgPaths) == 0 {
		return e, nil
	}

	pkgs, err := packages.Load(loadConfig(), pkgPaths...)
	if err != nil {
		return nil, fmt.Errorf("wiregen: load packages: %w", err)
	}
	if err := packagesError(pkgs); err != nil {
		return nil, err
	}
	allPkgs := indexPackages(pkgs)

	// Auto-discover enum values from source for any enum with empty Values.
	e.discoverEnumValues(pkgs)

	// Resolve each registered type.
	for _, wt := range r.Types {
		ti, err := e.resolveType(wt, allPkgs)
		if err != nil {
			return nil, err
		}
		e.byName[wt.Name] = ti
		e.types = append(e.types, ti)
	}

	// Sort by TS name for deterministic output.
	sort.Slice(e.types, func(i, j int) bool {
		return r.tsName(e.types[i].Name) < r.tsName(e.types[j].Name)
	})
	return e, nil
}

// resolvePackagePaths returns the explicit PackagePaths, or derives them from
// the registered types' packages when none are set.
func (r *Registry) resolvePackagePaths() []string {
	if len(r.PackagePaths) > 0 {
		return r.PackagePaths
	}
	var paths []string
	seen := map[string]bool{}
	for _, wt := range r.Types {
		if wt.PkgPath != "" && !seen[wt.PkgPath] {
			seen[wt.PkgPath] = true
			paths = append(paths, wt.PkgPath)
		}
	}
	return paths
}

// loadConfig is the go/packages config used to load and type-check the
// registered packages with CGO disabled.
func loadConfig() *packages.Config {
	return &packages.Config{
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

// valPos pairs a discovered enum const value with its source position so the
// values can be ordered by declaration order.
type valPos struct {
	val string
	pos token.Pos
}

// discoverEnumValues populates Values for any registered enum whose Values are
// empty, by collecting the string consts of the matching named type from the
// root packages (those loaded from PackagePaths / the registered types — not
// transitive deps, so a same-named enum type in a dependency like
// regexp/syntax can't pollute the set), in source order. Explicit Values
// always win, so consumers can override an enum with no backing const block
// (or a different order).
func (e *astEngine) discoverEnumValues(pkgs []*packages.Package) {
	need := map[string]bool{}
	for name, def := range e.r.Enums {
		if len(def.Values) == 0 {
			need[name] = true
		}
	}
	if len(need) == 0 {
		return
	}
	found := map[string][]valPos{}
	for _, pkg := range pkgs {
		collectStringConsts(pkg, need, found)
	}
	for tn, vps := range found {
		e.r.Enums[tn] = EnumDef{Values: dedupEnumValues(vps)}
	}
}

// collectStringConsts appends, for each needed enum type name, the string
// const values declared at pkg's scope (keyed by the named type's name).
func collectStringConsts(pkg *packages.Package, need map[string]bool, found map[string][]valPos) {
	if pkg.Types == nil {
		return
	}
	scope := pkg.Types.Scope()
	for _, nm := range scope.Names() {
		c, ok := scope.Lookup(nm).(*types.Const)
		if !ok {
			continue
		}
		named, ok := c.Type().(*types.Named)
		if !ok {
			continue
		}
		tn := named.Obj().Name()
		if !need[tn] {
			continue
		}
		if b, ok := named.Underlying().(*types.Basic); !ok || b.Info()&types.IsString == 0 {
			continue
		}
		if c.Val().Kind() != constant.String {
			continue
		}
		found[tn] = append(found[tn], valPos{constant.StringVal(c.Val()), c.Pos()})
	}
}

// dedupEnumValues returns the values ordered by source position and deduped by
// value (an exported and an unexported const can share a value).
func dedupEnumValues(vps []valPos) []string {
	sort.Slice(vps, func(i, j int) bool { return vps[i].pos < vps[j].pos })
	seen := map[string]bool{}
	var vals []string
	for _, vp := range vps {
		if seen[vp.val] {
			continue
		}
		seen[vp.val] = true
		vals = append(vals, vp.val)
	}
	return vals
}

func (e *astEngine) resolveType(wt WireType, allPkgs map[string]*packages.Package) (*typeInfo, error) {
	pkg, ok := allPkgs[wt.PkgPath]
	if !ok {
		return nil, fmt.Errorf("wiregen: package %q not loaded (needed for type %s)", wt.PkgPath, wt.Name)
	}

	// Find the type object
	obj := pkg.Types.Scope().Lookup(wt.Name)
	if obj == nil {
		return nil, fmt.Errorf("wiregen: type %s not found in package %s", wt.Name, wt.PkgPath)
	}

	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("wiregen: %s in %s is not a type", wt.Name, wt.PkgPath)
	}

	ti := &typeInfo{Name: wt.Name}

	// Find AST TypeSpec for doc comments + union directives
	ts := findTypeSpec(pkg, wt.Name)
	if ts != nil && ts.Doc != nil {
		ti.Doc = commentToJSDoc(ts.Doc)
		// Check for //wiregen:union directive
		ti.Union = parseUnionDirective(ts.Doc)
	}

	// Resolve struct fields
	underlying := tn.Type().Underlying()
	if st, ok := underlying.(*types.Struct); ok {
		ti.Fields = e.resolveStructFields(st, pkg, allPkgs, 0, nil)
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
		if !f.Exported() {
			continue // unexported (or embedded unexported) — skip
		}
		if f.Anonymous() {
			raw = append(raw, e.resolveEmbeddedField(f, st.Tag(i), depth, pkg, allPkgs, visited)...)
			continue
		}
		if fi, ok := e.resolveTaggedField(f, st.Tag(i), depth, pkg, allPkgs); ok {
			raw = append(raw, fi)
		}
	}
	return dedupJSONFields(raw)
}

// resolveEmbeddedField handles an anonymous (embedded) struct field per
// encoding/json promotion rules: an embed carrying an explicit json name
// is a NAMED nested field (resolved like a normal tagged field, with
// json:"-" skipping it), while an untagged embed is flattened into the
// parent.
func (e *astEngine) resolveEmbeddedField(f *types.Var, tag string, depth int, pkg *packages.Package, allPkgs map[string]*packages.Package, visited map[*types.Struct]bool) []fieldInfo {
	if name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ","); name != "" {
		if fi, ok := e.resolveTaggedField(f, tag, depth, pkg, allPkgs); ok {
			return []fieldInfo{fi}
		}
		return nil
	}
	return e.flattenEmbedded(f, pkg, allPkgs, depth, visited)
}

// flattenEmbedded resolves the fields promoted from an anonymous (embedded)
// field, unwrapping a pointer embed. A non-struct embed contributes nothing.
func (e *astEngine) flattenEmbedded(f *types.Var, pkg *packages.Package, allPkgs map[string]*packages.Package, depth int, visited map[*types.Struct]bool) []fieldInfo {
	embType := f.Type()
	if ptr, ok := embType.(*types.Pointer); ok {
		embType = ptr.Elem()
	}
	named, ok := embType.(*types.Named)
	if !ok {
		return nil
	}
	embSt, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	return e.resolveStructFields(embSt, pkg, allPkgs, depth+1, visited)
}

// resolveTaggedField resolves one exported, non-embedded field from its type
// and json tag. ok is false when the field is omitted (json:"-").
func (e *astEngine) resolveTaggedField(f *types.Var, tag string, depth int, pkg *packages.Package, allPkgs map[string]*packages.Package) (fieldInfo, bool) {
	jsonTag := reflect.StructTag(tag).Get("json")
	if jsonTag == "-" {
		return fieldInfo{}, false
	}

	parts := strings.Split(jsonTag, ",")
	wireName := parts[0]
	if wireName == "" {
		wireName = f.Name()
	}
	if wireName == "-" && len(parts) == 1 {
		return fieldInfo{}, false
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

	fi := e.resolveFieldType(f.Type(), wireName, omitempty, jsonString, depth)
	// Field doc comment from AST, scoped to this field's declaration.
	fi.Doc = e.findFieldDoc(f, pkg, allPkgs)
	fi.Tagged = parts[0] != "" // wire name from an explicit json tag (dominantField tiebreak)
	return fi, true
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

func (e *astEngine) resolveFieldType(t types.Type, wireName string, omitempty, jsonString bool, depth int) fieldInfo {
	fi := fieldInfo{
		WireName:   wireName,
		Optional:   omitempty,
		JSONString: jsonString,
		Depth:      depth,
	}

	// Unwrap pointer
	if ptr, ok := t.(*types.Pointer); ok {
		fi.Optional = true
		t = ptr.Elem()
	}

	// Check custom type mappings
	typKey := typeKey(t)
	if mapped, ok := e.r.TypeMappings[typKey]; ok {
		fi.TSType = mapped
		fi.GoTypeName = typKey
		return fi
	}

	switch ut := t.(type) {
	case *types.Alias:
		return e.resolveFieldType(types.Unalias(ut), wireName, fi.Optional, jsonString, depth)
	case *types.Named:
		return e.resolveNamedType(ut, &fi, wireName, jsonString, depth)
	case *types.Basic:
		fi.TSType = basicToTS(ut)
		return fi
	case *types.Slice:
		return e.resolveSliceType(ut, &fi)
	case *types.Map:
		return e.resolveMapType(ut, &fi)
	case *types.Interface:
		fi.TSType = tsUnknown
		fi.IsIface = true
		return fi
	case *types.Struct:
		fi.TSType = tsUnknown
		return fi
	case *types.Pointer:
		fi.Optional = true
		return e.resolveFieldType(ut.Elem(), wireName, true, jsonString, depth)
	}

	fi.TSType = tsUnknown
	return fi
}

// resolveNamedType resolves a named type: a custom full-name mapping, the
// time.Time / json.RawMessage special cases, a registered enum or struct, or
// otherwise a recurse into the underlying type.
func (e *astEngine) resolveNamedType(ut *types.Named, fi *fieldInfo, wireName string, jsonString bool, depth int) fieldInfo {
	name := ut.Obj().Name()
	pkgPath := ""
	if ut.Obj().Pkg() != nil {
		pkgPath = ut.Obj().Pkg().Path()
	}
	fullName := pkgPath + "." + name

	// Check custom mapping by full name
	if mapped, ok := e.r.TypeMappings[fullName]; ok {
		fi.TSType = mapped
		fi.GoTypeName = fullName
		return *fi
	}

	// time.Time → string
	if pkgPath == "time" && name == "Time" {
		fi.TSType = tsString
		return *fi
	}
	// json.RawMessage → unknown
	if pkgPath == "encoding/json" && name == "RawMessage" {
		fi.TSType = tsUnknown
		fi.IsRaw = true
		return *fi
	}
	// json.Number -> number (encoding/json marshals it as an unquoted number)
	if pkgPath == "encoding/json" && name == "Number" {
		fi.TSType = tsNumber
		return *fi
	}

	// Check if it's a registered enum
	if _, ok := e.r.Enums[name]; ok {
		fi.TSType = e.r.tsEnumName(name)
		fi.IsEnum = true
		fi.GoTypeName = name
		return *fi
	}

	// Check if it's a registered struct
	if e.r.typeNames[name] {
		fi.TSType = e.r.tsName(name)
		fi.IsStruct = true
		fi.GoTypeName = name
		return *fi
	}

	// Recurse into underlying type
	return e.resolveFieldType(ut.Underlying(), wireName, fi.Optional, jsonString, depth)
}

// resolveSliceType resolves a slice field. []byte maps to string (base64);
// otherwise the element type is resolved and suffixed with "[]".
func (e *astEngine) resolveSliceType(ut *types.Slice, fi *fieldInfo) fieldInfo {
	elem := ut.Elem()
	// []byte → string
	if b, ok := elem.(*types.Basic); ok && b.Kind() == types.Byte {
		fi.TSType = tsString
		return *fi
	}
	fi.IsSlice = true
	elemFI := e.resolveFieldType(elem, "", false, false, 0)
	fi.TSType = elemFI.TSType + "[]"
	fi.SliceElem = elemFI.TSType
	// elemFI.GoTypeName is already keyed correctly: the short Go name for a
	// registered struct/enum (matches r.typeNames / r.Enums) or the full
	// importpath.Type for a mapped type (matches Type/DecoderMappings).
	// Using typeKey(elem) here would always be the full name and miss the
	// short-keyed struct/enum lookups in elemDecoderExpr.
	fi.GoTypeName = elemFI.GoTypeName
	return *fi
}

// resolveMapType resolves a map field into Record<string, V>. The field is
// forced optional for ergonomics; a nil map marshals to null (it is omitted
// only when the field carries omitempty).
func (e *astEngine) resolveMapType(ut *types.Map, fi *fieldInfo) fieldInfo {
	fi.IsMap = true
	fi.Optional = true
	valFI := e.resolveFieldType(ut.Elem(), "", false, false, 0)
	fi.TSType = "Record<string, " + valFI.TSType + ">"
	fi.MapVal = valFI.TSType
	// See the slice case: use the element's resolved key, not typeKey(elem).
	fi.GoTypeName = valFI.GoTypeName
	return *fi
}

func (e *astEngine) findFieldDoc(fieldObj *types.Var, fallback *packages.Package, allPkgs map[string]*packages.Package) string {
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

func findTypeSpec(pkg *packages.Package, name string) *ast.TypeSpec {
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			if ts := typeSpecInDecl(gd, name); ts != nil {
				return ts
			}
		}
	}
	return nil
}

// typeSpecInDecl returns the TypeSpec named `name` declared in the type
// GenDecl gd, attaching the GenDecl doc when the spec itself has none. It
// returns nil if gd is not a type declaration or has no matching spec.
func typeSpecInDecl(gd *ast.GenDecl, name string) *ast.TypeSpec {
	if gd.Tok != token.TYPE {
		return nil
	}
	for _, spec := range gd.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		if ts.Name.Name != name {
			continue
		}
		// Attach GenDecl doc if TypeSpec doc is nil.
		if ts.Doc == nil && gd.Doc != nil {
			ts.Doc = gd.Doc
		}
		return ts
	}
	return nil
}

func parseUnionDirective(cg *ast.CommentGroup) *UnionDef {
	if cg == nil {
		return nil
	}
	for _, c := range cg.List {
		text := strings.TrimPrefix(c.Text, "//")
		text = strings.TrimPrefix(text, " ")
		if !strings.HasPrefix(text, "wiregen:union") {
			continue
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, "wiregen:union"))

		ud := parseUnionFields(text)
		if ud.Discriminator != "" && len(ud.Variants) > 0 {
			return ud
		}
	}
	return nil
}

// parseUnionFields parses the "discriminator=… variants=a,b,c" body of a
// wiregen:union directive, dropping empty variants.
func parseUnionFields(text string) *UnionDef {
	ud := &UnionDef{}
	for part := range strings.FieldsSeq(text) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "discriminator":
			ud.Discriminator = kv[1]
		case "variants":
			for v := range strings.SplitSeq(kv[1], ",") {
				if v != "" {
					ud.Variants = append(ud.Variants, v)
				}
			}
		}
	}
	return ud
}

// commentText returns the human text of a single comment line/block, with ok
// false for a Go pragma or wiregen directive that must not flow into JSDoc.
func commentText(c *ast.Comment) (string, bool) {
	text := c.Text
	// Skip Go pragmas and wiregen directives.
	trimmed := strings.TrimSpace(strings.TrimPrefix(text, "//"))
	if strings.HasPrefix(trimmed, "nolint") || strings.HasPrefix(trimmed, "go:") || strings.HasPrefix(trimmed, "wiregen:") {
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

func basicToTS(b *types.Basic) string {
	switch b.Kind() {
	case types.Bool, types.UntypedBool:
		return tsBoolean
	case types.String, types.UntypedString:
		return tsString
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64,
		types.Float32, types.Float64, types.UntypedInt, types.UntypedFloat:
		return tsNumber
	default:
		return tsUnknown
	}
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
