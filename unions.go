package wiregen

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Union declares Type, a registered struct, a union tagged by its Tag field,
// named by wire name. Each Cases key is a tag value and lists the wire names
// of the other fields that case allows; a listed field keeps its own presence
// rule. The TS type is a discriminated union, the decoder refuses a field
// another case owns, and OpenAPI gets a oneOf with a discriminator whose cases
// admit no other property.
type Union struct {
	Cases map[string][]string
	Type  WireType
	Tag   string
}

// unionInfo is one resolved Union: its tag's wire name and its cases in
// emission order.
type unionInfo struct {
	tag   string
	cases []unionCase
}

// unionCase is one tag value and the non-tag fields it allows, in field
// order.
type unionCase struct {
	value  string
	fields []string
}

// resolveUnions attaches each registered Union to its type, reporting every
// union that does not fit its type.
func (e *astEngine) resolveUnions() error {
	var errs []error
	for _, u := range e.gen.Unions {
		ti := e.byName[u.Type.Name]
		if ti.Union != nil {
			errs = append(errs, fmt.Errorf("wiregen: union %s.%s is declared twice", u.Type.PkgPath, u.Type.Name))
			continue
		}
		ui, err := e.resolveUnion(u, ti)
		if err != nil {
			errs = append(errs, fmt.Errorf("wiregen: union %s.%s: %w", u.Type.PkgPath, u.Type.Name, err))
			continue
		}
		ti.Union = ui
	}
	return errors.Join(errs...)
}

// resolveUnion checks u against ti's fields and orders its cases: by the tag
// enum's declaration order, else by value.
func (e *astEngine) resolveUnion(u Union, ti *typeInfo) (*unionInfo, error) {
	values, err := e.caseValues(u, ti)
	if err != nil {
		return nil, err
	}
	ui := &unionInfo{tag: u.Tag}
	covered := map[string]bool{u.Tag: true}
	for _, v := range values {
		uc, err := resolveCase(u, ti, v)
		if err != nil {
			return nil, err
		}
		for _, name := range uc.fields {
			covered[name] = true
		}
		ui.cases = append(ui.cases, uc)
	}
	for i := range ti.Fields {
		f := &ti.Fields[i]
		if !covered[f.WireName] {
			return nil, fmt.Errorf("field %q is in no case, so it can never be sent", f.WireName)
		}
		if f.WireName != u.Tag && !f.Omittable && !ui.inEveryCase(f.WireName) {
			return nil, fmt.Errorf("field %q is not in every case, but encoding/json writes it in every case; tag it omitempty or omitzero", f.WireName)
		}
	}
	return ui, nil
}

func (ui *unionInfo) inEveryCase(name string) bool {
	for i := range ui.cases {
		if !slices.Contains(ui.cases[i].fields, name) {
			return false
		}
	}
	return true
}

// caseValues checks u's tag field and returns its case values in emission
// order.
func (e *astEngine) caseValues(u Union, ti *typeInfo) ([]string, error) {
	tag := ti.field(u.Tag)
	if tag == nil {
		return nil, fmt.Errorf("has no field %q to tag it", u.Tag)
	}
	if tag.optional() || (tag.Kind != kindString && tag.Kind != kindEnum) {
		return nil, fmt.Errorf("tag %q must be a required string or enum field", u.Tag)
	}
	if len(u.Cases) == 0 {
		return nil, errors.New("declares no case")
	}
	values := slices.Sorted(maps.Keys(u.Cases))
	if tag.Kind != kindEnum {
		return values, nil
	}
	members := e.gen.enums[tag.GoTypeName].values
	if !slices.Equal(slices.Sorted(slices.Values(members)), values) {
		return nil, fmt.Errorf("cases %v must be exactly the members of tag enum %s, %v", values, tag.GoTypeName, members)
	}
	return members, nil
}

// resolveCase checks case v of u against ti and lists its fields in field
// order.
func resolveCase(u Union, ti *typeInfo, v string) (unionCase, error) {
	if !isComponentNamePart(v) {
		return unionCase{}, fmt.Errorf("case %q must be letters, digits, '.', '-' or '_', since it names an OpenAPI component", v)
	}
	allowed := map[string]bool{}
	for _, name := range u.Cases[v] {
		switch {
		case name == u.Tag:
			return unionCase{}, fmt.Errorf("case %q lists the tag %q", v, name)
		case ti.field(name) == nil:
			return unionCase{}, fmt.Errorf("case %q lists %q, which is no field of the type", v, name)
		case allowed[name]:
			return unionCase{}, fmt.Errorf("case %q lists %q twice", v, name)
		}
		allowed[name] = true
	}
	uc := unionCase{value: v}
	for i := range ti.Fields {
		if allowed[ti.Fields[i].WireName] {
			uc.fields = append(uc.fields, ti.Fields[i].WireName)
		}
	}
	return uc, nil
}

// field returns ti's field of wire name name, or nil.
func (ti *typeInfo) field(name string) *fieldInfo {
	for i := range ti.Fields {
		if ti.Fields[i].WireName == name {
			return &ti.Fields[i]
		}
	}
	return nil
}

// caseFields returns ti's fields uc allows, the tag first.
func (ti *typeInfo) caseFields(uc *unionCase) []*fieldInfo {
	out := make([]*fieldInfo, 0, 1+len(uc.fields))
	out = append(out, ti.field(ti.Union.tag))
	for _, name := range uc.fields {
		out = append(out, ti.field(name))
	}
	return out
}

// otherFields returns the wire names of ti's non-tag fields uc does not
// allow.
func (ti *typeInfo) otherFields(uc *unionCase) []string {
	var out []string
	for i := range ti.Fields {
		name := ti.Fields[i].WireName
		if name != ti.Union.tag && !slices.Contains(uc.fields, name) {
			out = append(out, name)
		}
	}
	return out
}

// isComponentNamePart reports whether s may appear in an OpenAPI component
// name (https://spec.openapis.org/oas/v3.1.0#components-object).
func isComponentNamePart(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if c := s[i]; !isAlnum(c) && c != '.' && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func (g *generator) emitUnionType(w *strings.Builder, ti *typeInfo) {
	w.WriteString(ti.Doc)
	w.WriteString("export type " + g.tsName(ti.Name) + " =")
	for i := range ti.Union.cases {
		uc := &ti.Union.cases[i]
		members := []string{tsPropName(ti.Union.tag) + ": \"" + tsStringLiteral(uc.value) + "\""}
		for _, f := range ti.caseFields(uc)[1:] {
			q := ""
			if f.optional() {
				q = "?"
			}
			members = append(members, tsPropName(f.WireName)+q+": "+f.TSType)
		}
		w.WriteString("\n  | { " + strings.Join(members, "; ") + " }")
	}
	w.WriteString(";\n\n")
}

func (g *generator) emitUnionDecoder(w *strings.Builder, ti *typeInfo, used *usedIdents) {
	tn := g.tsName(ti.Name)
	path := "$." + g.pathName(tn)
	used.helper("asObject")
	used.helper("reqOneOf")
	used.typeRef(tn)
	tagLit := "\"" + tsStringLiteral(ti.Union.tag) + "\""
	values := make([]string, 0, len(ti.Union.cases))
	for _, uc := range ti.Union.cases {
		values = append(values, "\""+tsStringLiteral(uc.value)+"\"")
	}
	w.WriteString("export const " + g.decoderName(ti.Name) + ": Decoder<" + tn + "> = (v) => {\n")
	w.WriteString("  const o = asObject(v, \"" + path + "\");\n")
	w.WriteString("  const tag = reqOneOf(o, " + tagLit + ", [" + strings.Join(values, ", ") + "] as const, \"" + path + "\");\n")
	w.WriteString("  switch (tag) {\n")
	for i := range ti.Union.cases {
		uc := &ti.Union.cases[i]
		lit := "\"" + tsStringLiteral(uc.value) + "\""
		var body strings.Builder
		if others := ti.otherFields(uc); len(others) > 0 {
			used.helper("refuseKeys")
			quoted := make([]string, 0, len(others))
			for _, o := range others {
				quoted = append(quoted, "\""+tsStringLiteral(o)+"\"")
			}
			body.WriteString("  refuseKeys(o, [" + strings.Join(quoted, ", ") + "], \"" + path + "\");\n")
		}
		outType := "globalThis.Extract<" + tn + ", { " + tsPropName(ti.Union.tag) + ": " + lit + " }>"
		g.emitFieldDecodes(&body, outType, tsPropName(ti.Union.tag)+": tag", ti.caseFields(uc)[1:], path, used)
		w.WriteString("    case " + lit + ": {\n")
		for line := range strings.SplitSeq(strings.TrimSuffix(body.String(), "\n"), "\n") {
			w.WriteString("    " + line + "\n")
		}
		w.WriteString("    }\n")
	}
	w.WriteString("    default:\n")
	w.WriteString("      throw new globalThis.TypeError(`" + path + ": unknown " + tsStringLiteral(ti.Union.tag) + " ${globalThis.String(tag as unknown)}`);\n")
	w.WriteString("  }\n")
	w.WriteString("};\n\n")
}

// arbForUnion renders a union's arbitrary: a draw from any case, the tag
// fixed to the case's value. A union that can reach itself is depth-capped
// like a recursive struct.
func (g *generator) arbForUnion(ti *typeInfo, recursive func(*fieldInfo) bool) string {
	render := func(indent string, rec func(*fieldInfo) bool) []string {
		out := make([]string, 0, len(ti.Union.cases))
		for i := range ti.Union.cases {
			uc := &ti.Union.cases[i]
			fixed := map[string]string{ti.Union.tag: "fc.constant(\"" + tsStringLiteral(uc.value) + "\" as const)"}
			out = append(out, g.arbRecord(ti.caseFields(uc), indent, rec, fixed))
		}
		return out
	}
	if recursive == nil {
		return "fc.oneof(\n    " + strings.Join(render("    ", nil), ",\n    ") + ",\n  )"
	}
	leaves := render("    ", recursive)
	full := render("    ", nil)
	return "fc.oneof(\n    " + arbRecursionCap + ",\n    " + strings.Join(append(leaves, full...), ",\n    ") + ",\n  )"
}
