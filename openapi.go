package wiregen

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// OpenAPIInfo is the OpenAPI document's info object and its security
// schemes, which Endpoint.Security names.
type OpenAPIInfo struct {
	SecuritySchemes map[string]SecurityScheme
	Title           string
	Version         string
	Description     string
}

// SecurityScheme is one OpenAPI security scheme
// (https://spec.openapis.org/oas/v3.1.0#security-scheme-object): Type is
// "http", with Scheme ("bearer", say), "apiKey", with In ("header", "query"
// or "cookie") and Name, or "mutualTLS".
type SecurityScheme struct {
	Type        string
	Scheme      string
	In          string
	Name        string
	Description string
}

// The JSON Schema and OpenAPI vocabulary the document is written in.
const (
	kwType        = "type"
	kwRef         = "$ref"
	kwRequired    = "required"
	kwDescription = "description"
	kwName        = "name"
	kwSchema      = "schema"
	kwContent     = "content"
	schemaString  = "string"
	schemaNull    = "null"
	schemaObject  = "object"
	schemaArray   = "array"
	mediaJSON     = "application/json"
	componentsRef = "#/components/schemas/"
)

// maxBytesKeyword is the schema extension carrying a MaxBytes bound in UTF-8
// bytes; maxLength beside it counts code points, so it is only a coarse form.
const maxBytesKeyword = "x-max-bytes"

// headerPattern is the printable ASCII a declared header's value holds.
const headerPattern = `^[\x20-\x7E]*$`

// direction is which way a schema's value crosses: encoding/json writes a
// response and decodes a request, and the two differ in presence and null.
type direction uint8

const (
	dirResponse direction = iota
	dirRequest
)

// validateOpenAPI rejects a malformed security scheme or a name none
// declares and, when OpenAPI files are written, a registry the document
// cannot describe.
func (g *generator) validateOpenAPI() error {
	if err := g.validateSecurity(); err != nil {
		return err
	}
	if len(g.o.openAPIFiles) == 0 {
		return nil
	}
	if g.OpenAPI.Title == "" || g.OpenAPI.Version == "" {
		return errors.New("wiregen: WithOpenAPIFiles needs Registry.OpenAPI's Title and Version")
	}
	for _, key := range slices.Sorted(maps.Keys(g.Mappings)) {
		if g.Mappings[key].Schema == nil {
			return fmt.Errorf("wiregen: mapping %s has no Schema, which the OpenAPI document needs", key)
		}
	}
	return nil
}

// validateSecurity checks every scheme and every scheme name an endpoint
// lists.
func (g *generator) validateSecurity() error {
	for _, name := range slices.Sorted(maps.Keys(g.OpenAPI.SecuritySchemes)) {
		s := g.OpenAPI.SecuritySchemes[name]
		if err := validateSecurityScheme(&s); err != nil {
			return fmt.Errorf("wiregen: security scheme %q: %w", name, err)
		}
	}
	for i := range g.Endpoints {
		e := &g.Endpoints[i]
		for _, s := range e.Security {
			if _, ok := g.OpenAPI.SecuritySchemes[s]; !ok {
				return fmt.Errorf("wiregen: endpoint %s names security scheme %q, which OpenAPI.SecuritySchemes does not declare", e.Name, s)
			}
		}
	}
	return nil
}

func validateSecurityScheme(s *SecurityScheme) error {
	switch s.Type {
	case "http":
		if s.Scheme == "" {
			return errors.New("type http needs a Scheme")
		}
	case "apiKey":
		if s.Name == "" || (s.In != "header" && s.In != "query" && s.In != "cookie") {
			return errors.New("type apiKey needs a Name and In of header, query or cookie")
		}
	case "mutualTLS":
	default:
		return fmt.Errorf("type %q is not http, apiKey or mutualTLS", s.Type)
	}
	return nil
}

// docBuilder renders one OpenAPI document. differs holds the registered
// types whose request schema differs from their response schema, and used
// the directions each type's component is emitted in, by Go name.
type docBuilder struct {
	g       *generator
	engine  *astEngine
	differs map[string]bool
	used    map[string]map[direction]bool
}

func (g *generator) renderOpenAPI(engine *astEngine) (string, error) {
	b := &docBuilder{g: g, engine: engine}
	b.differs = b.differingTypes()
	b.used = b.usedDirections()
	schemas, err := b.componentSchemas()
	if err != nil {
		return "", err
	}
	components := map[string]any{"schemas": schemas}
	if len(g.OpenAPI.SecuritySchemes) > 0 {
		sec := map[string]any{}
		for name, s := range g.OpenAPI.SecuritySchemes {
			sec[name] = omitEmpty(map[string]any{kwType: s.Type, "scheme": s.Scheme, "in": s.In, kwName: s.Name, kwDescription: s.Description})
		}
		components["securitySchemes"] = sec
	}
	paths := map[string]any{}
	for i := range g.Endpoints {
		e := &g.Endpoints[i]
		item, _ := paths[e.Path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[e.Path] = item
		}
		item[strings.ToLower(e.Method)] = b.operation(e)
	}
	doc := map[string]any{
		"openapi":    "3.1.0",
		"info":       omitEmpty(map[string]any{"title": g.OpenAPI.Title, "version": g.OpenAPI.Version, kwDescription: g.OpenAPI.Description}),
		"paths":      paths,
		"components": components,
	}
	var out strings.Builder
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return "", fmt.Errorf("wiregen: encode the OpenAPI document (an extension value JSON cannot encode?): %w", err)
	}
	var written any
	if err := json.Unmarshal([]byte(out.String()), &written); err != nil {
		return "", fmt.Errorf("wiregen: read back the OpenAPI document: %w", err)
	}
	if err := checkRefs(written, written, ""); err != nil {
		return "", err
	}
	return out.String(), nil
}

// checkRefs refuses a "$ref" anywhere in v, at JSON pointer at in doc, that
// is not a string or does not resolve inside doc.
func checkRefs(doc, v any, at string) error {
	switch v := v.(type) {
	case map[string]any:
		if err := checkRef(doc, v, at); err != nil {
			return err
		}
		for _, k := range slices.Sorted(maps.Keys(v)) {
			if err := checkRefs(doc, v[k], at+"/"+strings.NewReplacer("~", "~0", "/", "~1").Replace(k)); err != nil {
				return err
			}
		}
	case []any:
		for i, e := range v {
			if err := checkRefs(doc, e, at+"/"+strconv.Itoa(i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkRef refuses the $ref member of m, at JSON pointer at, unless it is
// absent or a string that resolves inside doc.
func checkRef(doc any, m map[string]any, at string) error {
	raw, ok := m[kwRef]
	if !ok {
		return nil
	}
	ref, ok := raw.(string)
	if !ok {
		return fmt.Errorf("wiregen: the $ref at %s is not a string; a Mappings Schema or an Extensions value gives every $ref wiregen does not write", cmp.Or(at, "/"))
	}
	if !resolvesIn(doc, ref) {
		return fmt.Errorf("wiregen: the OpenAPI document's $ref %q at %s names nothing in the document; a Mappings Schema or an Extensions value gives every $ref wiregen does not write", ref, cmp.Or(at, "/"))
	}
	return nil
}

// resolvesIn reports whether ref is a fragment whose JSON pointer names a
// value in doc.
func resolvesIn(doc any, ref string) bool {
	tokens, ok := pointerTokens(ref)
	if !ok {
		return false
	}
	cur := doc
	for _, key := range tokens {
		switch c := cur.(type) {
		case map[string]any:
			if cur, ok = c[key]; !ok {
				return false
			}
		case []any:
			i, valid := arrayIndex(key)
			if !valid || i >= len(c) {
				return false
			}
			cur = c[i]
		default:
			return false
		}
	}
	return true
}

// pointerTokens decodes ref, a URI fragment holding a JSON pointer, into its
// reference tokens (https://www.rfc-editor.org/rfc/rfc6901#section-6).
func pointerTokens(ref string) ([]string, bool) {
	frag, ok := strings.CutPrefix(ref, "#")
	if !ok {
		return nil, false
	}
	ptr, err := url.PathUnescape(frag)
	if err != nil {
		return nil, false
	}
	if ptr == "" {
		return nil, true
	}
	rest, ok := strings.CutPrefix(ptr, "/")
	if !ok {
		return nil, false
	}
	var tokens []string
	for tok := range strings.SplitSeq(rest, "/") {
		key, ok := unescapeToken(tok)
		if !ok {
			return nil, false
		}
		tokens = append(tokens, key)
	}
	return tokens, true
}

// unescapeToken decodes a reference token, in which "~" appears only as
// "~0" or "~1" (RFC 6901 section 3).
func unescapeToken(tok string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(tok); i++ {
		if tok[i] != '~' {
			b.WriteByte(tok[i])
			continue
		}
		if i+1 == len(tok) || (tok[i+1] != '0' && tok[i+1] != '1') {
			return "", false
		}
		b.WriteByte("~/"[tok[i+1]-'0'])
		i++
	}
	return b.String(), true
}

// arrayIndex reads an array-index token: "0", or a nonzero digit then
// digits (RFC 6901 section 4), so no sign, leading zero or "-".
func arrayIndex(tok string) (int, bool) {
	if tok == "" || (tok[0] == '0' && len(tok) > 1) || strings.Trim(tok, "0123456789") != "" {
		return 0, false
	}
	i, err := strconv.Atoi(tok)
	return i, err == nil
}

// omitEmpty drops m's empty string members.
func omitEmpty(m map[string]any) map[string]any {
	for k, v := range m {
		if v == "" {
			delete(m, k)
		}
	}
	return m
}

// differingTypes returns the types whose two schemas differ: by their own
// fields, or through a type they reference.
func (b *docBuilder) differingTypes() map[string]bool {
	differs := map[string]bool{}
	for _, ti := range b.engine.types {
		if !reflect.DeepEqual(b.typeSchemas(ti, dirResponse), b.typeSchemas(ti, dirRequest)) {
			differs[ti.Name] = true
		}
	}
	b.spreadDiffers(differs)
	return differs
}

// spreadDiffers marks every type that references a differing one, until
// none is left to mark.
func (b *docBuilder) spreadDiffers(differs map[string]bool) {
	for changed := true; changed; {
		changed = false
		for _, ti := range b.engine.types {
			if !differs[ti.Name] && ti.refsAny(differs) {
				differs[ti.Name], changed = true, true
			}
		}
	}
}

// refsAny reports whether any of ti's fields references a type in set.
func (ti *typeInfo) refsAny(set map[string]bool) bool {
	for i := range ti.Fields {
		if refsAny(&ti.Fields[i], set) {
			return true
		}
	}
	return false
}

// refsAny reports whether f's schema references a type in set.
func refsAny(f *fieldInfo, set map[string]bool) bool {
	switch {
	case f == nil:
		return false
	case f.Kind == kindSlice || f.Kind == kindMap:
		return refsAny(f.Elem, set)
	case f.Kind == kindStruct:
		return set[f.GoTypeName]
	}
	return false
}

// usedDirections returns the directions each type's component is emitted
// in: those it is reached in from the endpoints' bodies, events and
// ErrorEnvelope, then the response form of every other type that is not only
// an endpoint's parameters, which become parameters instead. The walk visits
// every field of each form it emits, so every $ref resolves.
func (b *docBuilder) usedDirections() map[string]map[direction]bool {
	used := map[string]map[direction]bool{}
	params := map[string]bool{}
	for i := range b.g.Endpoints {
		e := &b.g.Endpoints[i]
		b.engine.reach(used, e.Request, dirRequest)
		b.engine.reach(used, e.Response, dirResponse)
		params[e.Params.Name], params[e.Query.Name] = true, true
	}
	for _, ev := range b.g.Events {
		b.engine.reach(used, ev.Data, dirResponse)
	}
	b.engine.reach(used, b.g.ErrorEnvelope, dirResponse)
	for _, wt := range b.g.Types {
		if len(used[wt.Name]) == 0 && !params[wt.Name] {
			b.engine.reach(used, wt, dirResponse)
		}
	}
	return used
}

// componentSchemas renders every component schema, refusing two that share a
// name.
func (b *docBuilder) componentSchemas() (map[string]any, error) {
	out := map[string]any{}
	owner := map[string]string{}
	add := func(name, who string, schema any) error {
		if prev, dup := owner[name]; dup {
			return fmt.Errorf("wiregen: OpenAPI component %s is named by both %s and %s; rename one through TSNameOverride or EnumTSName", name, prev, who)
		}
		owner[name] = who
		out[name] = schema
		return nil
	}
	for _, name := range enumNamesSlice(b.g.enums) {
		values := b.g.enums[name].values
		if err := add(b.g.tsEnumName(name), "enum "+name, map[string]any{kwType: schemaString, "enum": slices.Clone(values)}); err != nil {
			return nil, err
		}
	}
	for _, ti := range b.engine.types {
		for _, d := range b.componentDirections(ti.Name) {
			for name, s := range b.typeSchemas(ti, d) {
				if err := add(name, "type "+ti.Name, s); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

// componentDirections returns the forms of type name the document carries:
// none when it is used in neither direction, one when its schemas agree,
// else each direction it is used in.
func (b *docBuilder) componentDirections(name string) []direction {
	used := b.used[name]
	switch {
	case len(used) == 0:
		return nil
	case !b.differs[name]:
		return []direction{dirResponse}
	}
	var dirs []direction
	for _, d := range []direction{dirResponse, dirRequest} {
		if used[d] {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// componentName is the component of registered type goName in direction d:
// its TS name, suffixed Input for a request form that differs.
func (b *docBuilder) componentName(goName string, d direction) string {
	name := b.g.tsName(goName)
	if d == dirRequest && b.differs[goName] {
		name += "Input"
	}
	return name
}

func (b *docBuilder) ref(goName string, d direction) map[string]any {
	return map[string]any{kwRef: componentsRef + b.componentName(goName, d)}
}

func (b *docBuilder) typeSchemas(ti *typeInfo, d direction) map[string]any {
	name := b.componentName(ti.Name, d)
	if ti.Union == nil {
		fields := make([]*fieldInfo, 0, len(ti.Fields))
		for i := range ti.Fields {
			fields = append(fields, &ti.Fields[i])
		}
		return map[string]any{name: b.objectSchema(fields, d, nil)}
	}
	out := map[string]any{}
	refs := make([]any, 0, len(ti.Union.cases))
	mapping := map[string]any{}
	for i := range ti.Union.cases {
		uc := &ti.Union.cases[i]
		caseName := name + "." + uc.value
		s := b.objectSchema(ti.caseFields(uc), d, map[string]any{ti.Union.tag: map[string]any{"const": uc.value}})
		s["additionalProperties"] = false
		out[caseName] = s
		refs = append(refs, map[string]any{kwRef: componentsRef + caseName})
		mapping[uc.value] = componentsRef + caseName
	}
	out[name] = map[string]any{
		"oneOf":         refs,
		"discriminator": map[string]any{"propertyName": ti.Union.tag, "mapping": mapping},
	}
	return out
}

// objectSchema is the object schema of fields in direction d, override
// replacing a property's schema by wire name. A response field is required
// unless omitempty or omitzero, since the encoder always writes it; a
// request field also unless a pointer, as the decoder leaves it nil.
func (b *docBuilder) objectSchema(fields []*fieldInfo, d direction, override map[string]any) map[string]any {
	props := map[string]any{}
	required := []any{}
	for _, f := range fields {
		props[f.WireName] = b.fieldSchema(f, d)
		if s, ok := override[f.WireName]; ok {
			props[f.WireName] = s
		}
		if (d == dirResponse && !f.absentOut()) || (d == dirRequest && !f.optional()) {
			required = append(required, f.WireName)
		}
	}
	s := map[string]any{kwType: schemaObject, "properties": props}
	if len(required) > 0 {
		s[kwRequired] = required
	}
	return s
}

// fieldSchema is f's value schema in direction d. A pointer, slice, map or
// []byte is nullable: in a request always, since null decodes to nil, and in
// a response unless omitempty or omitzero, since nil encodes as null. A Count
// lower bound of 1 or more makes it never nullable.
func (b *docBuilder) fieldSchema(f *fieldInfo, d direction) any {
	s := b.valueSchema(f, d)
	null := f.nullIn()
	if d == dirResponse {
		null = f.nullOut()
	}
	if !null || f.refusesNull() {
		return s
	}
	return nullable(s)
}

// elemSchema is a collection element's schema; a nilable element is
// nullable in both directions, as the decoders accept null for it.
func (b *docBuilder) elemSchema(f *fieldInfo, d direction) any {
	s := b.valueSchema(f, d)
	if f.nullIn() && !f.refusesNull() {
		return nullable(s)
	}
	return s
}

// valueSchema is the schema of f's non-null values.
func (b *docBuilder) valueSchema(f *fieldInfo, d direction) map[string]any {
	var s map[string]any
	switch f.Kind {
	case kindRaw:
		return map[string]any{}
	case kindMapped:
		return maps.Clone(f.Mapping.Schema)
	case kindEnum:
		return map[string]any{kwRef: componentsRef + b.g.tsEnumName(f.GoTypeName)}
	case kindStruct:
		return b.ref(f.GoTypeName, d)
	case kindBytes:
		return map[string]any{kwType: schemaString, "contentEncoding": "base64"}
	case kindTime:
		return map[string]any{kwType: schemaString, "format": "date-time"}
	case kindSlice:
		s = map[string]any{kwType: schemaArray, "items": b.elemSchema(f.Elem, d)}
	case kindMap:
		s = map[string]any{kwType: schemaObject, "additionalProperties": b.elemSchema(f.Elem, d)}
	default:
		s = map[string]any{kwType: scalarTypes[f.Kind]}
		if f.Kind == kindUint {
			s["minimum"] = 0
		}
	}
	f.Bounds.addKeywords(s)
	return s
}

var scalarTypes = map[wireKind]string{
	kindString: schemaString, kindQuoted: schemaString, kindBool: "boolean",
	kindNumber: "number", kindInt: "integer", kindUint: "integer",
}

// nullable widens s to admit null: a plain type keyword becomes a type list,
// anything else is wrapped in anyOf.
func nullable(s map[string]any) map[string]any {
	if t, ok := s[kwType].(string); ok && s["enum"] == nil && s["const"] == nil {
		out := maps.Clone(s)
		out[kwType] = []any{t, schemaNull}
		return out
	}
	return map[string]any{"anyOf": []any{s, map[string]any{kwType: schemaNull}}}
}

// operation renders e's operation object.
func (b *docBuilder) operation(e *Endpoint) map[string]any {
	op := omitEmpty(map[string]any{"operationId": e.Name, "summary": e.Summary, kwDescription: e.Description})
	if params := b.parameters(e); len(params) > 0 {
		op["parameters"] = params
	}
	if e.Request != (WireType{}) {
		op["requestBody"] = map[string]any{
			kwRequired: true,
			kwContent:  map[string]any{mediaJSON: map[string]any{kwSchema: b.ref(e.Request.Name, dirRequest)}},
		}
	}
	responses := map[string]any{strconv.Itoa(e.successStatus()): b.successResponse(e)}
	maps.Copy(responses, b.errorResponses(e))
	op["responses"] = responses
	security := make([]any, 0, len(e.Security))
	for _, s := range e.Security {
		security = append(security, map[string]any{s: []any{}})
	}
	op["security"] = security
	maps.Copy(op, e.Extensions)
	return op
}

// parameters renders e's path, query and header parameters in that order.
func (b *docBuilder) parameters(e *Endpoint) []any {
	var out []any
	if e.Params != (WireType{}) {
		ti := b.engine.byName[e.Params.Name]
		for _, name := range pathParams(e.Path) {
			out = append(out, map[string]any{kwName: name, "in": "path", kwRequired: true, kwSchema: b.valueSchema(ti.field(name), dirRequest)})
		}
	}
	if e.Query != (WireType{}) {
		ti := b.engine.byName[e.Query.Name]
		for i := range ti.Fields {
			f := &ti.Fields[i]
			out = append(out, map[string]any{kwName: f.WireName, "in": "query", kwRequired: !f.optional(), kwSchema: b.valueSchema(f, dirRequest)})
		}
	}
	for _, h := range e.Headers {
		p := map[string]any{
			kwName: h.Name, "in": "header", kwRequired: h.Required,
			kwSchema: map[string]any{kwType: schemaString, "minLength": h.MinBytes, "maxLength": h.MaxBytes, "pattern": headerPattern},
		}
		if h.Description != "" {
			p[kwDescription] = h.Description
		}
		out = append(out, p)
	}
	return out
}

// successResponse renders e's 2xx or redirect response.
func (b *docBuilder) successResponse(e *Endpoint) map[string]any {
	resp := map[string]any{kwDescription: http.StatusText(e.successStatus())}
	switch e.Kind {
	case KindJSON:
		if e.Response == (WireType{}) {
			return resp
		}
		schema := b.ref(e.Response.Name, dirResponse)
		if e.Array {
			schema = map[string]any{kwType: schemaArray, "items": schema}
		}
		resp[kwContent] = map[string]any{mediaJSON: map[string]any{kwSchema: schema}}
	case KindRaw:
		resp[kwContent] = map[string]any{e.ContentType: map[string]any{}}
	case KindEventStream:
		media := map[string]any{kwSchema: map[string]any{kwType: schemaString}}
		if len(b.g.Events) > 0 {
			events := map[string]any{}
			for _, ev := range b.g.Events {
				events[ev.Name] = b.ref(ev.Data.Name, dirResponse)
			}
			media["x-events"] = events
		}
		resp[kwContent] = map[string]any{"text/event-stream": media}
	case KindRedirect:
		resp["headers"] = map[string]any{"Location": map[string]any{kwRequired: true, kwSchema: map[string]any{kwType: schemaString, "format": "uri-reference"}}}
	}
	return resp
}

// errorResponses renders e's errors grouped by status, each code's
// extensions under x-error-codes.
func (b *docBuilder) errorResponses(e *Endpoint) map[string]any {
	byStatus := map[int][]ErrorRef{}
	for _, er := range e.Errors {
		byStatus[er.Status] = append(byStatus[er.Status], er)
	}
	out := map[string]any{}
	for status, refs := range byStatus {
		codes := map[string]any{}
		names := make([]string, 0, len(refs))
		for _, er := range refs {
			ext := map[string]any{}
			maps.Copy(ext, er.Extensions)
			codes[er.Code] = ext
			names = append(names, er.Code)
		}
		resp := map[string]any{
			kwDescription:   http.StatusText(status) + ": " + strings.Join(names, ", "),
			"x-error-codes": codes,
		}
		if b.g.ErrorEnvelope != (WireType{}) {
			resp[kwContent] = map[string]any{mediaJSON: map[string]any{kwSchema: b.ref(b.g.ErrorEnvelope.Name, dirResponse)}}
		}
		out[strconv.Itoa(status)] = resp
	}
	return out
}
