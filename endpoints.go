package wiregen

import (
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

// Kind classifies an endpoint's 2xx body, which decides what the client
// emits for it.
type Kind uint8

// Endpoint kinds. The zero Kind is no kind: every endpoint states one.
const (
	// KindJSON answers Response as JSON, or no body when Response is zero; the
	// client gets a descriptor and a function.
	KindJSON Kind = iota + 1
	// KindRaw answers a body of ContentType that the transport reads as a
	// Blob; the client gets a descriptor and a function.
	KindRaw
	// KindEventStream answers text/event-stream carrying Registry.Events; the
	// client gets a descriptor only, whose request names the stream's URL.
	KindEventStream
	// KindRedirect answers a redirect a browser navigation follows; the client
	// gets a descriptor only.
	KindRedirect
)

func (k Kind) String() string {
	switch k {
	case KindJSON:
		return "json"
	case KindRaw:
		return "raw"
	case KindEventStream:
		return "event-stream"
	case KindRedirect:
		return "redirect"
	default:
		return fmt.Sprintf("Kind(%d)", uint8(k))
	}
}

// ErrorRef is one error an endpoint may answer. Code joins the endpoint's TS
// error union and its descriptor's codes. A code means one thing across the
// registry, so every endpoint listing it gives the same Status and
// Extensions, whose keys are OpenAPI x- members of that code's entry.
type ErrorRef struct {
	Extensions map[string]any
	Code       string
	Status     int
}

// Header is one request header an endpoint declares: a typed argument of its
// TS function, named Arg, sent in the transport's headers. Its value is
// printable ASCII of MinBytes to MaxBytes bytes.
type Header struct {
	Name, Arg, Description string
	MinBytes, MaxBytes     int
	Required               bool
}

// Endpoint is one HTTP operation of the wire contract. The table is
// authoritative: generate the client and document from the same values the
// server binds its routes from, so no second route list can drift from it.
//
// Params names the path placeholders' struct, its fields' wire names exactly
// the Path's {name} segments; Query is the query string's struct. Both hold
// only string, number, boolean or enum fields. Every non-zero WireType here
// joins the registry's types.
type Endpoint struct {
	Extensions  map[string]any // OpenAPI x- members of the operation
	Params      WireType
	Query       WireType
	Request     WireType
	Response    WireType // zero: no body
	Name        string   // a TS identifier: the function's name
	Method      string
	Path        string
	ContentType string // the 2xx body's media type; KindRaw only, and required there
	Summary     string
	Description string
	Headers     []Header
	Errors      []ErrorRef
	Security    []string // OpenAPI security scheme names; nil: public
	// Status is the success status: a 2xx, or a redirect's 3xx. Zero is 200,
	// 204 for a KindJSON endpoint with no Response, and 302 for KindRedirect.
	Status int
	Array  bool // the body is a JSON array of Response
	Kind   Kind
}

// successStatus is e's success status, Status or its kind's default.
func (e *Endpoint) successStatus() int {
	switch {
	case e.Status != 0:
		return e.Status
	case e.Kind == KindRedirect:
		return http.StatusFound
	case e.Kind == KindJSON && e.Response == (WireType{}):
		return http.StatusNoContent
	}
	return http.StatusOK
}

// Event is one named frame of an event-stream endpoint, whose data is a JSON
// Data.
type Event struct {
	Data WireType
	Name string
}

// endpointMethods is the closed set of allowed Endpoint.Method values.
var endpointMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
}

// pathTemplate is path with each placeholder's name dropped, so two routes
// that match the same requests compare equal.
func pathTemplate(path string) string {
	var b strings.Builder
	for rest := path; rest != ""; {
		i := strings.IndexByte(rest, '{')
		end := strings.IndexByte(rest, '}')
		if i < 0 || end < i {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i] + "{}")
		rest = rest[end+1:]
	}
	return b.String()
}

// pathParams extracts the {name} placeholder names from a validated path in
// order.
func pathParams(path string) []string {
	var params []string
	for rest := path; ; {
		_, after, ok := strings.Cut(rest, "{")
		if !ok {
			return params
		}
		name, tail, _ := strings.Cut(after, "}")
		params = append(params, name)
		rest = tail
	}
}

// pascal upper-cases name's first letter: the prefix of an endpoint's
// exported TS types.
func pascal(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

// descriptorName is the exported descriptor constant of endpoint name.
func descriptorName(name string) string { return name + "Endpoint" }

// codesName is the module-private constant holding endpoint name's codes,
// from which its error union is derived.
func codesName(name string) string { return name + "Codes" }

// clientReserved are the identifiers the client file binds itself, which no
// endpoint-derived name may take.
var clientReserved = []string{"clientRequest", "ApiResult", "EndpointSpec", "ClientOpts", "qs", "decodeBlob"}

// errorCatalogEntry is the first listing of a code, against which every
// other listing must agree.
type errorCatalogEntry struct {
	ext      map[string]any
	endpoint string
	status   int
}

// validateEndpoints rejects a structurally invalid endpoint table before any
// package is loaded, claiming each endpoint's identifiers in taken.
// Field-level checks that need the loaded types are validateEndpointTypes'.
func (g *generator) validateEndpoints(taken map[string]string) error {
	catalog := map[string]errorCatalogEntry{}
	routes, paths := map[string]string{}, map[string]string{}
	for i := range g.Endpoints {
		e := &g.Endpoints[i]
		if err := validateEndpoint(e); err != nil {
			return err
		}
		template := pathTemplate(e.Path)
		if prev, ok := paths[template]; ok && prev != e.Path {
			return fmt.Errorf("wiregen: paths %s and %s differ only in placeholder names, which OpenAPI forbids (https://spec.openapis.org/oas/v3.1.0#paths-object)", prev, e.Path)
		}
		paths[template] = e.Path
		route := e.Method + " " + template
		if prev, dup := routes[route]; dup {
			return fmt.Errorf("wiregen: endpoints %s and %s are both %s %s", prev, e.Name, e.Method, e.Path)
		}
		routes[route] = e.Name
		if err := validateEndpointErrors(e, catalog); err != nil {
			return err
		}
		if err := e.claimIdents(taken); err != nil {
			return err
		}
	}
	return nil
}

// claimIdents records the identifiers the client emits for e in taken,
// refusing one already taken.
func (e *Endpoint) claimIdents(taken map[string]string) error {
	ids := []string{descriptorName(e.Name), codesName(e.Name), pascal(e.Name) + "Error", pascal(e.Name) + "Args"}
	if e.hasFunction() {
		ids = append(ids, e.Name)
	}
	for _, id := range ids {
		if prev, dup := taken[id]; dup {
			return fmt.Errorf("wiregen: endpoint %s emits %s, which %s already uses", e.Name, id, prev)
		}
		taken[id] = "endpoint " + e.Name
	}
	return nil
}

// hasFunction reports whether the client emits a function for e.
func (e *Endpoint) hasFunction() bool { return e.Kind == KindJSON || e.Kind == KindRaw }

// validateEndpoint checks one endpoint's own fields.
func validateEndpoint(e *Endpoint) error {
	if !isValidTSIdent(e.Name) || tsReservedWords[e.Name] {
		return fmt.Errorf("wiregen: endpoint name %q is not a valid TS identifier or is a reserved one", e.Name)
	}
	if !endpointMethods[e.Method] {
		return fmt.Errorf("wiregen: endpoint %s has unknown method %q", e.Name, e.Method)
	}
	if err := validatePath(e.Name, e.Path); err != nil {
		return err
	}
	if err := validateKind(e); err != nil {
		return err
	}
	if e.Request != (WireType{}) && e.Method == http.MethodGet {
		return fmt.Errorf("wiregen: endpoint %s is a GET and cannot take a Request body", e.Name)
	}
	if (len(pathParams(e.Path)) > 0) != (e.Params != (WireType{})) {
		return fmt.Errorf("wiregen: endpoint %s must set Params exactly when its path has placeholders", e.Name)
	}
	if err := validateExtensions("endpoint "+e.Name, e.Extensions); err != nil {
		return err
	}
	return validateHeaders(e)
}

// validateKind checks the fields each kind allows.
func validateKind(e *Endpoint) error {
	switch e.Kind {
	case KindJSON:
		if e.Array && e.Response == (WireType{}) {
			return fmt.Errorf("wiregen: endpoint %s sets Array without a Response", e.Name)
		}
	case KindRaw, KindEventStream, KindRedirect:
		if e.Response != (WireType{}) || e.Array {
			return fmt.Errorf("wiregen: endpoint %s is kind %s and cannot set Response or Array", e.Name, e.Kind)
		}
	case 0:
		return fmt.Errorf("wiregen: endpoint %s has no Kind", e.Name)
	default:
		return fmt.Errorf("wiregen: endpoint %s has unknown kind %s", e.Name, e.Kind)
	}
	if (e.Kind == KindRaw) != (e.ContentType != "") {
		return fmt.Errorf("wiregen: endpoint %s must set ContentType exactly when it is kind raw", e.Name)
	}
	if e.Kind == KindEventStream || e.Kind == KindRedirect {
		if e.Method != http.MethodGet {
			return fmt.Errorf("wiregen: endpoint %s is kind %s and must be a GET", e.Name, e.Kind)
		}
	}
	if e.Kind == KindRedirect && len(e.Headers) > 0 {
		return fmt.Errorf("wiregen: endpoint %s is kind redirect, which a navigation follows with no headers of its own", e.Name)
	}
	return validateStatus(e)
}

// validateStatus checks e's success status against its kind: a redirect
// answers 301, 302, 303, 307 or 308; anything else a 2xx, 204 exactly when it
// has no body.
func validateStatus(e *Endpoint) error {
	st := e.successStatus()
	if e.Kind == KindRedirect {
		switch st {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			return nil
		}
		return fmt.Errorf("wiregen: endpoint %s is kind redirect and has status %d, which is not a redirect", e.Name, st)
	}
	if st < 200 || st > 299 || st == http.StatusResetContent {
		return fmt.Errorf("wiregen: endpoint %s has success status %d, outside 2xx", e.Name, st)
	}
	noBody := e.Kind == KindJSON && e.Response == (WireType{})
	if noBody != (st == http.StatusNoContent) {
		return fmt.Errorf("wiregen: endpoint %s has status %d; 204 is exactly the status of a JSON endpoint with no Response", e.Name, st)
	}
	return nil
}

// validateExtensions rejects an extension key outside OpenAPI's x- space
// (https://spec.openapis.org/oas/v3.1.0#specification-extensions).
func validateExtensions(owner string, ext map[string]any) error {
	for _, k := range slices.Sorted(maps.Keys(ext)) {
		if !strings.HasPrefix(k, "x-") || len(k) == 2 {
			return fmt.Errorf("wiregen: %s has extension %q, which must start with x-", owner, k)
		}
	}
	return nil
}

// validateEndpointErrors checks e's error list and records each code's first
// listing in catalog, rejecting a listing that disagrees with it.
func validateEndpointErrors(e *Endpoint, catalog map[string]errorCatalogEntry) error {
	seen := map[string]bool{}
	for _, er := range e.Errors {
		if !isErrorCode(er.Code) {
			return fmt.Errorf("wiregen: endpoint %s has error code %q, which must be lower snake case ([a-z][a-z0-9_]*)", e.Name, er.Code)
		}
		if seen[er.Code] {
			return fmt.Errorf("wiregen: endpoint %s lists error %s twice", e.Name, er.Code)
		}
		seen[er.Code] = true
		if er.Status < 400 || er.Status > 599 {
			return fmt.Errorf("wiregen: endpoint %s error %s has status %d, outside 400 to 599", e.Name, er.Code, er.Status)
		}
		if err := validateExtensions("endpoint "+e.Name+" error "+er.Code, er.Extensions); err != nil {
			return err
		}
		prev, ok := catalog[er.Code]
		if !ok {
			catalog[er.Code] = errorCatalogEntry{endpoint: e.Name, status: er.Status, ext: er.Extensions}
			continue
		}
		if prev.status != er.Status || !reflect.DeepEqual(normExt(prev.ext), normExt(er.Extensions)) {
			return fmt.Errorf("wiregen: error %s differs between endpoints %s and %s; a code has one status and one set of extensions", er.Code, prev.endpoint, e.Name)
		}
	}
	return nil
}

// normExt makes a nil and an empty extension map compare equal.
func normExt(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	return m
}

// isErrorCode reports whether code is lower snake case.
func isErrorCode(code string) bool {
	if code == "" || code[0] < 'a' || code[0] > 'z' {
		return false
	}
	for i := 1; i < len(code); i++ {
		c := code[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// validateHeaders checks e's declared headers.
func validateHeaders(e *Endpoint) error {
	names := map[string]bool{}
	args := map[string]bool{}
	for _, h := range e.Headers {
		if !isHTTPToken(h.Name) {
			return fmt.Errorf("wiregen: endpoint %s header %q is not an HTTP field name", e.Name, h.Name)
		}
		lower := strings.ToLower(h.Name)
		if names[lower] {
			return fmt.Errorf("wiregen: endpoint %s declares header %s twice", e.Name, h.Name)
		}
		names[lower] = true
		if !isValidTSIdent(h.Arg) {
			return fmt.Errorf("wiregen: endpoint %s header %s has Arg %q, which is not a valid TS identifier", e.Name, h.Name, h.Arg)
		}
		if args[h.Arg] {
			return fmt.Errorf("wiregen: endpoint %s has two headers with Arg %s", e.Name, h.Arg)
		}
		args[h.Arg] = true
		if h.MinBytes < 0 || h.MaxBytes < 1 || h.MinBytes > h.MaxBytes {
			return fmt.Errorf("wiregen: endpoint %s header %s needs 0 <= MinBytes <= MaxBytes and MaxBytes >= 1, got %d and %d", e.Name, h.Name, h.MinBytes, h.MaxBytes)
		}
	}
	return nil
}

// isHTTPToken reports whether s is an RFC 9110 token, the grammar of a field
// name (https://www.rfc-editor.org/rfc/rfc9110#section-5.6.2).
func isHTTPToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !isAlnum(c) && !strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			return false
		}
	}
	return true
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// validatePath checks that path is absolute, holds only RFC 3986 path
// characters outside its placeholders, and that its {name} placeholders are
// balanced, unnested, unique TS identifiers. The character rule is what lets
// the client embed the path in a string or template literal unescaped.
func validatePath(epName, path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("wiregen: endpoint %s path %q must start with /", epName, path)
	}
	seen := map[string]bool{}
	for rest := path; rest != ""; {
		i := strings.IndexAny(rest, "{}")
		if i < 0 {
			i = len(rest)
		}
		if bad := badPathLiteral(rest[:i]); bad >= 0 {
			c, _ := utf8.DecodeRuneInString(rest[bad:])
			return fmt.Errorf("wiregen: endpoint %s path %q has %q, which is not a URL path character or a complete pct-encoding", epName, path, string(c))
		}
		if i == len(rest) {
			break
		}
		n, err := placeholderAt(epName, path, rest[i:], seen)
		if err != nil {
			return err
		}
		rest = rest[i+n:]
	}
	return nil
}

// placeholderAt checks the placeholder rest starts with, records its name in
// seen and returns its length in bytes.
func placeholderAt(epName, path, rest string, seen map[string]bool) (int, error) {
	if rest[0] == '}' {
		return 0, fmt.Errorf("wiregen: endpoint %s path %q has unbalanced '}'", epName, path)
	}
	end := strings.IndexByte(rest, '}')
	if end < 0 {
		return 0, fmt.Errorf("wiregen: endpoint %s path %q has unbalanced '{'", epName, path)
	}
	name := rest[1:end]
	switch {
	case strings.Contains(name, "{"):
		return 0, fmt.Errorf("wiregen: endpoint %s path %q has nested '{'", epName, path)
	case !isValidTSIdent(name):
		return 0, fmt.Errorf("wiregen: endpoint %s path %q placeholder {%s} is not a valid identifier", epName, path, name)
	case seen[name]:
		return 0, fmt.Errorf("wiregen: endpoint %s path %q has duplicate placeholder {%s}", epName, path, name)
	}
	seen[name] = true
	return end + 1, nil
}

// badPathLiteral returns the offset of the first byte of lit that is not an
// RFC 3986 pchar, a segment slash or the start of a "%" HEXDIG HEXDIG
// pct-encoding (https://www.rfc-editor.org/rfc/rfc3986#section-3.3), or -1.
func badPathLiteral(lit string) int {
	for i := 0; i < len(lit); i++ {
		c := lit[i]
		switch {
		case c == '%':
			if i+2 >= len(lit) || !isHexDigit(lit[i+1]) || !isHexDigit(lit[i+2]) {
				return i
			}
			i += 2
		case !isAlnum(c) && !strings.ContainsRune("-._~!$&'()*+,;=:@/", rune(c)):
			return i
		}
	}
	return -1
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// argField is one member of an endpoint's arguments object.
type argField struct {
	name     string
	tsType   string
	optional bool
}

// endpointArgs is the arguments object of one endpoint: its path params, its
// query fields, its body and its header arguments, in that order.
type endpointArgs struct {
	body    string // the body's TS type; "" when the endpoint takes none
	params  []argField
	query   []argField
	headers []Header
}

func (a *endpointArgs) empty() bool {
	return len(a.params)+len(a.query)+len(a.headers) == 0 && a.body == ""
}

// optional reports whether every member is optional, so the whole object may
// be omitted.
func (a *endpointArgs) optional() bool {
	if len(a.params) > 0 || a.body != "" {
		return false
	}
	for _, f := range a.query {
		if !f.optional {
			return false
		}
	}
	for _, h := range a.headers {
		if h.Required {
			return false
		}
	}
	return true
}

// validateEndpointTypes checks what needs the loaded types: that Params
// fields match the placeholders and are required scalars, that Query fields
// are scalars, and that no two arguments share a name. It records each endpoint's arguments for the emitter.
func (g *generator) validateEndpointTypes(engine *astEngine) (map[string]*endpointArgs, error) {
	out := make(map[string]*endpointArgs, len(g.Endpoints))
	for i := range g.Endpoints {
		e := &g.Endpoints[i]
		args, err := g.resolveArgs(e, engine)
		if err != nil {
			return nil, err
		}
		out[e.Name] = args
	}
	return out, nil
}

// resolveArgs builds e's arguments object from its loaded Params and Query,
// refusing two arguments of one name.
func (g *generator) resolveArgs(e *Endpoint, engine *astEngine) (*endpointArgs, error) {
	args := &endpointArgs{headers: e.Headers}
	var err error
	if e.Params != (WireType{}) {
		if args.params, err = paramsFields(e, engine); err != nil {
			return nil, err
		}
	}
	if e.Query != (WireType{}) {
		if args.query, err = scalarFields(e, "Query", e.Query, engine); err != nil {
			return nil, err
		}
	}
	type member struct{ name, role string }
	var members []member
	for _, f := range args.params {
		members = append(members, member{f.name, "Params"})
	}
	for _, f := range args.query {
		members = append(members, member{f.name, "Query"})
	}
	if e.Request != (WireType{}) {
		args.body = g.tsName(e.Request.Name)
		members = append(members, member{"body", "Request"})
	}
	for _, h := range e.Headers {
		members = append(members, member{h.Arg, "header " + h.Name})
	}
	used := map[string]string{}
	for _, m := range members {
		if prev, dup := used[m.name]; dup {
			return nil, fmt.Errorf("wiregen: endpoint %s has argument %s from both %s and %s", e.Name, m.name, prev, m.role)
		}
		used[m.name] = m.role
	}
	return args, nil
}

// paramsFields returns e's Params fields, which must be required and name
// exactly the path's placeholders.
func paramsFields(e *Endpoint, engine *astEngine) ([]argField, error) {
	fields, err := scalarFields(e, "Params", e.Params, engine)
	if err != nil {
		return nil, err
	}
	got := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.optional {
			return nil, fmt.Errorf("wiregen: endpoint %s Params field %s is optional, but a path segment is always present", e.Name, f.name)
		}
		got = append(got, f.name)
	}
	want := pathParams(e.Path)
	if !slices.Equal(slices.Sorted(slices.Values(want)), slices.Sorted(slices.Values(got))) {
		return nil, fmt.Errorf("wiregen: endpoint %s Params fields %v differ from its path placeholders %v", e.Name, got, want)
	}
	return fields, nil
}

// scalarFields returns wt's fields as arguments, refusing any that is not a
// string, number, boolean or enum.
func scalarFields(e *Endpoint, role string, wt WireType, engine *astEngine) ([]argField, error) {
	ti := engine.byName[wt.Name]
	if ti.Union != nil {
		return nil, fmt.Errorf("wiregen: endpoint %s %s type %s is a union; URL arguments are a plain struct's fields", e.Name, role, wt.Name)
	}
	fields := make([]argField, 0, len(ti.Fields))
	for i := range ti.Fields {
		f := &ti.Fields[i]
		ts := f.TSType
		if !urlScalar(f) {
			return nil, fmt.Errorf("wiregen: endpoint %s %s field %s.%s is %s; a URL argument is a string, number, boolean or enum", e.Name, role, wt.Name, f.GoName, ts)
		}
		if !isValidTSIdent(f.WireName) {
			return nil, fmt.Errorf("wiregen: endpoint %s %s field %s.%s has wire name %q, which is not a valid TS identifier", e.Name, role, wt.Name, f.GoName, f.WireName)
		}
		fields = append(fields, argField{name: f.WireName, tsType: ts, optional: f.optional()})
	}
	return fields, nil
}

// urlScalar reports whether f is a string, number, boolean or enum, which a
// URL argument carries as its text.
func urlScalar(f *fieldInfo) bool {
	switch f.Kind {
	case kindSlice, kindMap, kindStruct, kindRaw, kindBytes, kindRefused:
		return false
	case kindMapped:
		return f.TSType == tsString || f.TSType == tsNumber || f.TSType == tsBoolean
	}
	return true
}

// --- client generation ---

// clientState accumulates which shared plumbing the emitted endpoints use.
type clientState struct {
	used      *usedIdents
	functions bool
	qs        bool
	blob      bool
}

// generateClient writes the client module: per endpoint an arguments
// interface, a descriptor whose request() builds the transport's
// EndpointSpec, the error union, and for JSON and raw endpoints a function
// returning ApiResult.
func (g *generator) generateClient(w *strings.Builder, args map[string]*endpointArgs) {
	st := &clientState{used: newUsedIdents()}
	var body strings.Builder
	for i := range g.Endpoints {
		e := &g.Endpoints[i]
		g.emitEndpoint(&body, e, args[e.Name], st)
	}

	w.WriteString(g.o.headerComment)
	g.emitClientImports(w, st)
	if st.functions {
		w.WriteString("/** Options accepted by every generated client function. */\n")
		w.WriteString("export interface ClientOpts {\n  readonly signal?: globalThis.AbortSignal;\n}\n\n")
	}
	if st.qs {
		w.WriteString("function qs(entries: readonly (readonly [string, string | number | boolean | undefined])[]): string {\n")
		w.WriteString("  const p = new globalThis.URLSearchParams();\n")
		w.WriteString("  for (const [k, v] of entries) {\n")
		w.WriteString("    if (v !== undefined) {\n      p.append(k, globalThis.String(v));\n    }\n")
		w.WriteString("  }\n")
		w.WriteString("  const s = p.toString();\n")
		w.WriteString("  return s === \"\" ? \"\" : `?${s}`;\n")
		w.WriteString("}\n\n")
	}
	if st.blob {
		w.WriteString("function decodeBlob(v: unknown): globalThis.Blob {\n")
		w.WriteString("  if (!(v instanceof globalThis.Blob)) {\n    throw new globalThis.TypeError(\"$: expected Blob\");\n  }\n")
		w.WriteString("  return v;\n")
		w.WriteString("}\n\n")
	}
	w.WriteString(strings.TrimSuffix(body.String(), "\n"))
}

// emitEndpoint writes one endpoint's arguments interface, codes and error
// union, descriptor and, for JSON and raw endpoints, its function.
func (g *generator) emitEndpoint(w *strings.Builder, e *Endpoint, args *endpointArgs, st *clientState) {
	argsType := pascal(e.Name) + "Args"
	errType := pascal(e.Name) + "Error"
	desc := descriptorName(e.Name)
	codes := codesName(e.Name)
	if !args.empty() {
		emitArgsInterface(w, argsType, args, st.used)
	}
	w.WriteString("const " + codes + " = [")
	for i, er := range e.Errors {
		if i > 0 {
			w.WriteString(", ")
		}
		w.WriteString("\"" + er.Code + "\"")
	}
	w.WriteString("] as const;\n\n")
	w.WriteString("export type " + errType + " = (typeof " + codes + ")[number];\n\n")

	param := ""
	if !args.empty() {
		param = "args: " + argsType
	}
	doc := jsDoc(e.Summary, e.Description)
	w.WriteString(doc)
	w.WriteString("export const " + desc + " = {\n")
	w.WriteString("  method: \"" + e.Method + "\",\n")
	w.WriteString("  path: \"" + e.Path + "\",\n")
	w.WriteString("  codes: " + codes + ",\n")
	if e.Kind == KindRaw {
		w.WriteString("  contentType: \"" + tsStringLiteral(e.ContentType) + "\",\n")
	}
	w.WriteString("  request(" + param + "): EndpointSpec {\n")
	emitRequestBody(w, e, codes, args, st)
	w.WriteString("  },\n")
	result := ""
	if e.hasFunction() {
		var decode string
		result, decode = g.endpointResult(e, st)
		w.WriteString("  decode: " + decode + ",\n")
	}
	w.WriteString("} as const;\n\n")
	if !e.hasFunction() {
		return
	}
	st.functions = true
	w.WriteString(doc)
	sig := param
	call := "args"
	switch {
	case args.empty():
		call = ""
	case args.optional():
		sig = "args: " + argsType + " = {}"
	}
	if sig != "" {
		sig += ", "
	}
	w.WriteString("export function " + e.Name + "(" + sig + "opts?: ClientOpts): globalThis.Promise<ApiResult<" + result + ", " + errType + ">> {\n")
	w.WriteString("  return clientRequest<" + result + ", " + errType + ">(" + desc + ".request(" + call + "), " + desc + ".decode, opts);\n")
	w.WriteString("}\n\n")
}

// emitArgsInterface writes the exported arguments interface.
func emitArgsInterface(w *strings.Builder, name string, args *endpointArgs, used *usedIdents) {
	w.WriteString("export interface " + name + " {\n")
	member := func(n, ts string, optional bool) {
		q := ""
		if optional {
			q = "?"
		}
		w.WriteString("  readonly " + n + q + ": " + ts + ";\n")
	}
	for _, group := range [][]argField{args.params, args.query} {
		for _, f := range group {
			useArgType(f.tsType, used)
			member(f.name, f.tsType, f.optional)
		}
	}
	if args.body != "" {
		used.typeRef(args.body)
		member("body", args.body, false)
	}
	for _, h := range args.headers {
		member(h.Arg, tsString, !h.Required)
	}
	w.WriteString("}\n\n")
}

// useArgType records an enum-typed argument's import.
func useArgType(ts string, used *usedIdents) {
	if ts != tsString && ts != tsNumber && ts != tsBoolean {
		used.typeRef(ts)
	}
}

// emitRequestBody writes request()'s statements, building the EndpointSpec.
func emitRequestBody(w *strings.Builder, e *Endpoint, codes string, args *endpointArgs, st *clientState) {
	if len(args.headers) > 0 {
		w.WriteString("    const headers: globalThis.Record<string, string> = {};\n")
		for _, h := range args.headers {
			set := "headers[\"" + h.Name + "\"] = args." + h.Arg + ";"
			if h.Required {
				w.WriteString("    " + set + "\n")
				continue
			}
			w.WriteString("    if (args." + h.Arg + " !== undefined) {\n      " + set + "\n    }\n")
		}
	}
	w.WriteString("    return {\n")
	w.WriteString("      method: \"" + e.Method + "\",\n")
	w.WriteString("      path: " + pathExpr(e.Path, args, st) + ",\n")
	if e.Kind == KindRaw {
		w.WriteString("      raw: true,\n")
	}
	if args.body != "" {
		w.WriteString("      body: args.body,\n")
	}
	if len(args.headers) > 0 {
		w.WriteString("      headers,\n")
	}
	w.WriteString("      codes: " + codes + ",\n")
	w.WriteString("    };\n")
}

// pathExpr renders the request path: the path with each placeholder
// percent-encoded from args, then the query folded in.
func pathExpr(path string, args *endpointArgs, st *clientState) string {
	expr := "\"" + path + "\""
	if len(args.params) > 0 {
		expr = path
		for _, p := range args.params {
			expr = strings.Replace(expr, "{"+p.name+"}", "${globalThis.encodeURIComponent(globalThis.String(args."+p.name+"))}", 1)
		}
		expr = "`" + expr + "`"
	}
	if len(args.query) == 0 {
		return expr
	}
	st.qs = true
	entries := make([]string, 0, len(args.query))
	for _, f := range args.query {
		entries = append(entries, "[\""+f.name+"\", args."+f.name+"]")
	}
	return expr + " + qs([" + strings.Join(entries, ", ") + "])"
}

// endpointResult returns a function endpoint's success type and its decode
// expression.
func (g *generator) endpointResult(e *Endpoint, st *clientState) (tsType, decode string) {
	if e.Kind == KindRaw {
		st.blob = true
		return "globalThis.Blob", "decodeBlob"
	}
	if e.Response == (WireType{}) {
		return "void", "(): void => undefined"
	}
	tn := g.tsName(e.Response.Name)
	dec := g.decoderName(e.Response.Name)
	st.used.typeRef(tn)
	st.used.decoderRef(dec)
	if e.Array {
		st.used.helper(helperDecodeArray)
		return tn + "[]", "(v: unknown): " + tn + "[] => decodeArray(v, " + dec + ", \"$\")"
	}
	return tn, dec
}

// jsDoc renders summary and description as a JSDoc block, or "" when both
// are empty. A "*/" in either is broken so it cannot close the comment.
func jsDoc(summary, description string) string {
	text := strings.TrimSpace(strings.TrimSpace(summary) + "\n\n" + strings.TrimSpace(description))
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "*/", "*\\/")
	var b strings.Builder
	b.WriteString("/**\n")
	for line := range strings.SplitSeq(text, "\n") {
		b.WriteString(strings.TrimRight(" * "+line, " ") + "\n")
	}
	b.WriteString(" */\n")
	return b.String()
}

// emitClientImports writes the client's imports: the transport, the
// validators helper, the decoders and the types the endpoints use.
func (g *generator) emitClientImports(w *strings.Builder, st *clientState) {
	transport := tsStringLiteral(g.o.transportImport)
	if st.functions {
		w.WriteString("import { clientRequest } from \"" + transport + "\";\n")
		w.WriteString("import type { ApiResult, EndpointSpec } from \"" + transport + "\";\n")
	} else {
		w.WriteString("import type { EndpointSpec } from \"" + transport + "\";\n")
	}
	if st.used.helpers[helperDecodeArray] {
		w.WriteString("import { decodeArray } from \"" + g.validatorsSpec(g.o.clientFilename) + "\";\n")
	}
	if decoders := slices.Sorted(maps.Keys(st.used.decoders)); len(decoders) > 0 {
		w.WriteString("import { " + strings.Join(decoders, ", ") + " } from \"" + g.decodersSpec(g.o.clientFilename) + "\";\n")
	}
	if types := slices.Sorted(maps.Keys(st.used.types)); len(types) > 0 {
		w.WriteString("import type { " + strings.Join(types, ", ") + " } from \"" + g.typesSpec(g.o.clientFilename) + "\";\n")
	}
	w.WriteString("\n")
}
