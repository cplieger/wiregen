package wiregen

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// typeInfo holds resolved type data extracted from AST.
type typeInfo struct { //nolint:govet // fieldalignment: readability over alignment
	Fields []fieldInfo
	Union  *UnionDef
	Name   string
	Doc    string // JSDoc comment
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
	IsSlice    bool
	IsMap      bool
	IsStruct   bool
	IsEnum     bool
	IsRaw      bool
	IsIface    bool
}

// astEngine loads Go packages and resolves type information from source.
type astEngine struct { //nolint:govet // fieldalignment: readability over alignment
	types  []*typeInfo // sorted by TS name
	byName map[string]*typeInfo
	r      *Registry
}

func newASTEngine(r *Registry) (*astEngine, error) {
	e := &astEngine{r: r, byName: make(map[string]*typeInfo)}

	if len(r.Types) == 0 && len(r.PackagePaths) == 0 {
		// No types registered — return empty engine (for constants-only, registry-only use)
		return e, nil
	}

	// Load packages
	pkgPaths := r.PackagePaths
	if len(pkgPaths) == 0 {
		// Derive package paths from registered types
		seen := map[string]bool{}
		for _, wt := range r.Types {
			if wt.PkgPath != "" && !seen[wt.PkgPath] {
				seen[wt.PkgPath] = true
				pkgPaths = append(pkgPaths, wt.PkgPath)
			}
		}
	}

	if len(pkgPaths) == 0 {
		return e, nil
	}

	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedFiles | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedName | packages.NeedImports |
			packages.NeedDeps,
		Env: append(defaultEnv(), "CGO_ENABLED=0"),
	}

	pkgs, err := packages.Load(cfg, pkgPaths...)
	if err != nil {
		return nil, fmt.Errorf("wiregen: load packages: %w", err)
	}

	// Check for package errors
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			return nil, fmt.Errorf("wiregen: package %s: %s", pkg.PkgPath, e.Msg)
		}
	}

	// Build index of all packages (including deps)
	allPkgs := make(map[string]*packages.Package)
	var visit func(*packages.Package)
	visit = func(p *packages.Package) {
		if _, ok := allPkgs[p.PkgPath]; ok {
			return
		}
		allPkgs[p.PkgPath] = p
		for _, imp := range p.Imports {
			visit(imp)
		}
	}
	for _, pkg := range pkgs {
		visit(pkg)
	}

	// Resolve each registered type
	for _, wt := range r.Types {
		ti, err := e.resolveType(wt, allPkgs)
		if err != nil {
			return nil, err
		}
		e.byName[wt.Name] = ti
		e.types = append(e.types, ti)
	}

	// Sort by TS name for deterministic output
	sort.Slice(e.types, func(i, j int) bool {
		return r.tsName(e.types[i].Name) < r.tsName(e.types[j].Name)
	})

	return e, nil
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

//nolint:gocyclo // flat type switch
func (e *astEngine) resolveStructFields(st *types.Struct, pkg *packages.Package, allPkgs map[string]*packages.Package, depth int, visited map[*types.Struct]bool) []fieldInfo {
	if visited == nil {
		visited = make(map[*types.Struct]bool)
	}
	if visited[st] {
		return nil
	}
	visited[st] = true

	type rawField struct {
		info  fieldInfo
		index int
	}
	var raw []rawField

	for i := range st.NumFields() {
		f := st.Field(i)
		if !f.Exported() {
			// Embedded unexported type — skip
			continue
		}

		if f.Anonymous() {
			// Flatten embedded struct
			embType := f.Type()
			if ptr, ok := embType.(*types.Pointer); ok {
				embType = ptr.Elem()
			}
			named, ok := embType.(*types.Named)
			if !ok {
				continue
			}
			embSt, ok := named.Underlying().(*types.Struct)
			if !ok {
				continue
			}
			subFields := e.resolveStructFields(embSt, pkg, allPkgs, depth+1, visited)
			for idx, sf := range subFields {
				raw = append(raw, rawField{info: sf, index: len(raw) + idx})
			}
			continue
		}

		tag := st.Tag(i)
		jsonTag := reflect.StructTag(tag).Get("json")
		if jsonTag == "-" {
			continue
		}

		parts := strings.Split(jsonTag, ",")
		wireName := parts[0]
		if wireName == "" {
			wireName = f.Name()
		}
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

		fi := e.resolveFieldType(f.Type(), wireName, omitempty, jsonString, depth, allPkgs)

		// Get field doc comment from AST
		fi.Doc = e.findFieldDoc(pkg, f.Name(), st, allPkgs)

		raw = append(raw, rawField{info: fi, index: len(raw)})
	}

	// Apply encoding/json field-selection semantics
	type entry struct {
		field fieldInfo
		count int
		idx   int
	}
	best := make(map[string]*entry, len(raw))
	for i := range raw {
		rf := &raw[i]
		if ent, ok := best[rf.info.WireName]; ok {
			if rf.info.Depth < ent.field.Depth {
				ent.field = rf.info
				ent.count = 1
				ent.idx = rf.index
			} else if rf.info.Depth == ent.field.Depth {
				ent.count++
			}
		} else {
			best[rf.info.WireName] = &entry{field: rf.info, count: 1, idx: rf.index}
		}
	}

	seen := make(map[string]bool, len(best))
	var result []fieldInfo
	for i := range raw {
		rf := &raw[i]
		if seen[rf.info.WireName] {
			continue
		}
		seen[rf.info.WireName] = true
		ent := best[rf.info.WireName]
		if ent.count > 1 {
			continue
		}
		result = append(result, ent.field)
	}
	return result
}

//nolint:gocyclo // flat type switch
func (e *astEngine) resolveFieldType(t types.Type, wireName string, omitempty, jsonString bool, depth int, _ map[string]*packages.Package) fieldInfo {
	fi := fieldInfo{
		WireName:   wireName,
		Optional:   omitempty,
		JSONString: jsonString,
		Depth:      depth,
	}

	origType := t
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
		return e.resolveFieldType(types.Unalias(ut), wireName, fi.Optional, jsonString, depth, nil)

	case *types.Named:
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
			return fi
		}

		// time.Time → string
		if pkgPath == "time" && name == "Time" {
			fi.TSType = tsString
			return fi
		}
		// json.RawMessage → unknown
		if pkgPath == "encoding/json" && name == "RawMessage" {
			fi.TSType = tsUnknown
			fi.IsRaw = true
			return fi
		}

		// Check if it's a registered enum
		if _, ok := e.r.Enums[name]; ok {
			fi.TSType = e.r.tsEnumName(name)
			fi.IsEnum = true
			fi.GoTypeName = name
			return fi
		}

		// Check if it's a registered struct
		if e.r.typeNames[name] {
			fi.TSType = e.r.tsName(name)
			fi.IsStruct = true
			fi.GoTypeName = name
			return fi
		}

		// Recurse into underlying type
		return e.resolveFieldType(ut.Underlying(), wireName, fi.Optional, jsonString, depth, nil)

	case *types.Basic:
		fi.TSType = basicToTS(ut)
		return fi

	case *types.Slice:
		elem := ut.Elem()
		// []byte → string
		if b, ok := elem.(*types.Basic); ok && b.Kind() == types.Byte {
			fi.TSType = tsString
			return fi
		}
		fi.IsSlice = true
		elemFI := e.resolveFieldType(elem, "", false, false, 0, nil)
		fi.TSType = elemFI.TSType + "[]"
		fi.SliceElem = elemFI.TSType
		fi.GoTypeName = typeKey(elem)
		return fi

	case *types.Map:
		fi.IsMap = true
		fi.Optional = true
		valFI := e.resolveFieldType(ut.Elem(), "", false, false, 0, nil)
		fi.TSType = "Record<string, " + valFI.TSType + ">"
		fi.MapVal = valFI.TSType
		fi.GoTypeName = typeKey(ut.Elem())
		return fi

	case *types.Interface:
		fi.TSType = tsUnknown
		fi.IsIface = true
		return fi

	case *types.Struct:
		fi.TSType = tsUnknown
		return fi

	case *types.Pointer:
		fi.Optional = true
		return e.resolveFieldType(ut.Elem(), wireName, true, jsonString, depth, nil)
	}

	// Map type also makes field optional (same as reflect engine)
	if _, isMap := origType.(*types.Map); isMap {
		fi.Optional = true
	}

	fi.TSType = tsUnknown
	return fi
}

func (e *astEngine) findFieldDoc(pkg *packages.Package, fieldName string, _ *types.Struct, allPkgs map[string]*packages.Package) string {
	// Search through AST files for the field doc
	for _, f := range pkg.Syntax {
		doc := findFieldDocInFile(f, fieldName)
		if doc != "" {
			return doc
		}
	}
	// Also check imported packages
	_ = allPkgs
	return ""
}

func findFieldDocInFile(file *ast.File, fieldName string) string {
	var result string
	ast.Inspect(file, func(n ast.Node) bool {
		if result != "" {
			return false
		}
		st, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range st.Fields.List {
			for _, name := range field.Names {
				if name.Name == fieldName && field.Doc != nil {
					result = commentToJSDoc(field.Doc)
					return false
				}
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
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if ts.Name.Name == name {
					// Attach GenDecl doc if TypeSpec doc is nil
					if ts.Doc == nil && gd.Doc != nil {
						ts.Doc = gd.Doc
					}
					return ts
				}
			}
		}
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
		text = strings.TrimPrefix(text, "wiregen:union")
		text = strings.TrimSpace(text)

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
				ud.Variants = strings.Split(kv[1], ",")
			}
		}
		// Filter out empty variants
		var filtered []string
		for _, v := range ud.Variants {
			if v != "" {
				filtered = append(filtered, v)
			}
		}
		ud.Variants = filtered
		if ud.Discriminator != "" && len(ud.Variants) > 0 {
			return ud
		}
	}
	return nil
}

func commentToJSDoc(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	var lines []string
	for _, c := range cg.List {
		text := c.Text
		// Skip Go pragmas and wiregen directives
		trimmed := strings.TrimSpace(strings.TrimPrefix(text, "//"))
		if strings.HasPrefix(trimmed, "nolint") || strings.HasPrefix(trimmed, "go:") || strings.HasPrefix(trimmed, "wiregen:") {
			continue
		}
		if strings.HasPrefix(text, "//") {
			lines = append(lines, strings.TrimPrefix(text, "// "))
		} else if inner, ok := strings.CutPrefix(text, "/*"); ok {
			inner = strings.TrimSuffix(inner, "*/")
			lines = append(lines, strings.TrimSpace(inner))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	// Filter empty lines
	var nonEmpty []string
	for _, l := range lines {
		if strings.TrimPrefix(l, "/") != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	// Sanitize: replace */ with *\/ to prevent premature JSDoc close
	for i, l := range nonEmpty {
		nonEmpty[i] = strings.ReplaceAll(l, "*/", "*\\/")
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
		return "number"
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
