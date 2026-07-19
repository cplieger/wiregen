package wiregen

import (
	"errors"
	"fmt"
	"go/format"
	"sort"
	"strings"
)

// EndpointKind classifies how an endpoint's payload flows through the
// generated client.
//
//   - KindJSON (the default): a typed client function pair is generated,
//     decoder-bound when a Response is registered.
//   - KindRaw: a non-JSON escape hatch (file upload, YAML body, video
//     stream, redirect flow). No client function is generated; the endpoint
//     is emitted as a PATH_ constant and participates in consistency checks.
//   - KindSSE: an EventSource stream. Same emission as KindRaw; the kind
//     tag documents why no function exists.
type EndpointKind string

// Endpoint kinds.
const (
	KindJSON EndpointKind = "json"
	KindRaw  EndpointKind = "raw"
	KindSSE  EndpointKind = "sse"
)

// RespShape describes the JSON shape of a KindJSON endpoint's 2xx response
// relative to its registered Response type.
type RespShape string

// Response shapes.
const (
	// RespObject decodes the body as one Response object (the default).
	RespObject RespShape = "object"
	// RespArray decodes the body as an array of Response objects.
	RespArray RespShape = "array"
	// RespRecord decodes the body as a string-keyed record of Response objects.
	RespRecord RespShape = "record"
	// RespStringArray decodes the body as a bare array of strings. Response
	// must be the zero WireType.
	RespStringArray RespShape = "stringArray"
)

// Endpoint describes one HTTP endpoint of the app's wire contract. The
// endpoint table is data: the server's route registration stays authoritative
// for permissions; consumers typically add a consistency test comparing the
// two (see the AuthGroup field).
type Endpoint struct {
	// Name is the generated TS function name (KindJSON) and the base for the
	// PATH_/Go path-constant names. Must be a valid TS identifier.
	Name string
	// Method is the HTTP method (GET, POST, PUT, PATCH, DELETE).
	Method string
	// Path is the URL path. Parameter segments use {name} placeholders
	// (e.g. "/api/scan/series/{id}") and become typed function arguments.
	Path string
	// AuthGroup is an opaque consumer-defined tag (e.g. "admin",
	// "userConfigured"). wiregen never interprets it; it exists so a
	// consumer's consistency check can compare the table against the
	// server's route registration.
	AuthGroup string
	// Kind classifies the payload flow; empty means KindJSON.
	Kind EndpointKind
	// RespShape refines Response decoding; empty means RespObject.
	RespShape RespShape
	// Doc is an optional JSDoc line emitted above the generated functions.
	Doc string
	// Request is the registered wire type of the JSON request body. Zero
	// means no typed body; set HasBody for an untyped (unknown) body.
	Request WireType
	// Response is the registered wire type of the 2xx response body. Zero
	// means the response is not decoded (the OK-flag function flavor is
	// generated instead of the typed pair).
	Response WireType
	// HasBody marks an untyped JSON request body (body: unknown) for
	// endpoints whose request has no registered Go struct.
	HasBody bool
	// Query adds a trailing optional query?: Record<string, QueryValue>
	// argument, serialized with URLSearchParams (undefined values skipped).
	Query bool
}

// kindOrDefault returns the endpoint kind with the empty default applied.
func (e *Endpoint) kindOrDefault() EndpointKind {
	if e.Kind == "" {
		return KindJSON
	}
	return e.Kind
}

// respShapeOrDefault returns the response shape with the empty default applied.
func (e *Endpoint) respShapeOrDefault() RespShape {
	if e.RespShape == "" {
		return RespObject
	}
	return e.RespShape
}

// endpointMethods is the closed set of allowed Endpoint.Method values.
var endpointMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

// pathParams extracts the {name} placeholder names from path in order.
// Malformed placeholders are reported by validateEndpoints; this helper
// assumes a validated path.
func pathParams(path string) []string {
	var params []string
	for i := 0; i < len(path); {
		open := strings.IndexByte(path[i:], '{')
		if open < 0 {
			break
		}
		open += i
		closing := strings.IndexByte(path[open:], '}')
		if closing < 0 {
			break
		}
		closing += open
		params = append(params, path[open+1:closing])
		i = closing + 1
	}
	return params
}

// validateEndpoints rejects a structurally invalid endpoint table before any
// file is written: unknown methods/kinds/shapes, unsanitizable or duplicate
// names, malformed paths or placeholders, and Response/Request types that are
// not registered (a KindJSON response without a registered decoder would emit
// a call to a nonexistent decoder function).
func (r *Registry) validateEndpoints() error {
	seenNames := map[string]bool{}
	seenPathConst := map[string]string{}
	seenGoConst := map[string]string{}
	for i := range r.Endpoints {
		e := &r.Endpoints[i]
		if err := r.validateEndpoint(e); err != nil {
			return err
		}
		if seenNames[e.Name] {
			return fmt.Errorf("wiregen: duplicate endpoint name %q", e.Name)
		}
		seenNames[e.Name] = true
		// Distinct validated names can still collide after case conversion
		// (e.g. "configYaml" and "configYAML" both become PATH_CONFIG_YAML /
		// PathConfigYAML); previously the second constant was silently
		// dropped from the generated output.
		cn := pathConstName(e.Name)
		if prev := seenPathConst[cn]; prev != "" {
			return fmt.Errorf("wiregen: endpoints %q and %q both emit path constant %s", prev, e.Name, cn)
		}
		seenPathConst[cn] = e.Name
		gn := goPathConstName(e.Name)
		if prev := seenGoConst[gn]; prev != "" {
			return fmt.Errorf("wiregen: endpoints %q and %q both emit Go path constant %s", prev, e.Name, gn)
		}
		seenGoConst[gn] = e.Name
	}
	return nil
}

// validateEndpoint checks a single endpoint (everything except cross-endpoint
// name uniqueness, which validateEndpoints owns).
func (r *Registry) validateEndpoint(e *Endpoint) error {
	if e.Name == "" || !isValidTSIdent(e.Name) {
		return fmt.Errorf("wiregen: endpoint name %q is not a valid TS identifier", e.Name)
	}
	if !endpointMethods[e.Method] {
		return fmt.Errorf("wiregen: endpoint %s has unknown method %q", e.Name, e.Method)
	}
	if !strings.HasPrefix(e.Path, "/") {
		return fmt.Errorf("wiregen: endpoint %s path %q must start with /", e.Name, e.Path)
	}
	if err := validatePathPlaceholders(e.Name, e.Path); err != nil {
		return err
	}
	switch e.kindOrDefault() {
	case KindJSON:
		return r.validateJSONEndpoint(e)
	case KindRaw, KindSSE:
		if e.Request != (WireType{}) || e.Response != (WireType{}) || e.HasBody {
			return fmt.Errorf("wiregen: endpoint %s is kind %q and cannot bind request/response types", e.Name, e.kindOrDefault())
		}
		return nil
	default:
		return fmt.Errorf("wiregen: endpoint %s has unknown kind %q", e.Name, e.Kind)
	}
}

// placeholderScan carries the mutable state of one validatePathPlaceholders
// walk: brace depth, the current placeholder name, and the names seen.
type placeholderScan struct {
	seen  map[string]bool
	cur   strings.Builder
	depth int
}

// open handles a '{' rune.
func (s *placeholderScan) open(epName, path string) error {
	s.depth++
	if s.depth > 1 {
		return fmt.Errorf("wiregen: endpoint %s path %q has nested '{'", epName, path)
	}
	s.cur.Reset()
	return nil
}

// close handles a '}' rune, validating the completed placeholder name.
func (s *placeholderScan) close(epName, path string) error {
	s.depth--
	if s.depth < 0 {
		return fmt.Errorf("wiregen: endpoint %s path %q has unbalanced '}'", epName, path)
	}
	name := s.cur.String()
	if !isValidTSIdent(name) {
		return fmt.Errorf("wiregen: endpoint %s path %q placeholder {%s} is not a valid identifier", epName, path, name)
	}
	if s.seen[name] {
		return fmt.Errorf("wiregen: endpoint %s path %q has duplicate placeholder {%s}", epName, path, name)
	}
	s.seen[name] = true
	return nil
}

// validatePathPlaceholders checks {name} segment syntax: balanced braces, no
// nesting, non-empty TS-identifier names, no duplicates.
func validatePathPlaceholders(epName, path string) error {
	s := placeholderScan{seen: map[string]bool{}}
	for _, c := range path {
		var err error
		switch c {
		case '{':
			err = s.open(epName, path)
		case '}':
			err = s.close(epName, path)
		default:
			if s.depth == 1 {
				s.cur.WriteRune(c)
			}
		}
		if err != nil {
			return err
		}
	}
	if s.depth != 0 {
		return fmt.Errorf("wiregen: endpoint %s path %q has unbalanced '{'", epName, path)
	}
	return nil
}

// validateJSONEndpoint checks the type bindings of a KindJSON endpoint.
func (r *Registry) validateJSONEndpoint(e *Endpoint) error {
	shape := e.respShapeOrDefault()
	switch shape {
	case RespObject, RespArray, RespRecord:
		if e.Response != (WireType{}) && !r.typeNames[e.Response.Name] {
			return fmt.Errorf("wiregen: endpoint %s response type %s is not a registered type", e.Name, e.Response.Name)
		}
		if e.Response == (WireType{}) && shape != RespObject {
			return fmt.Errorf("wiregen: endpoint %s has shape %q but no Response type", e.Name, shape)
		}
	case RespStringArray:
		if e.Response != (WireType{}) {
			return fmt.Errorf("wiregen: endpoint %s has shape stringArray and must not set Response", e.Name)
		}
	default:
		return fmt.Errorf("wiregen: endpoint %s has unknown response shape %q", e.Name, e.RespShape)
	}
	if e.Request != (WireType{}) {
		if !r.typeNames[e.Request.Name] {
			return fmt.Errorf("wiregen: endpoint %s request type %s is not a registered type", e.Name, e.Request.Name)
		}
		if e.HasBody {
			return fmt.Errorf("wiregen: endpoint %s sets both Request and HasBody", e.Name)
		}
	}
	return nil
}

// --- client generation ---

// GenerateClient returns the client.gen.ts content as a string, rejecting a
// config error (missing TransportImport / ValidatorsImport, invalid endpoint
// table) with the same validation Generate applies.
func (r *Registry) GenerateClient() (string, error) {
	if err := r.init(); err != nil {
		return "", err
	}
	if r.TransportImport == "" {
		return "", errors.New("wiregen: TransportImport must be set when endpoints are registered")
	}
	if r.ValidatorsImport == "" {
		return "", errors.New("wiregen: ValidatorsImport must be set")
	}
	if err := r.validateEndpoints(); err != nil {
		return "", err
	}
	var b strings.Builder
	r.generateClient(&b)
	return b.String(), nil
}

// pathConstName converts an endpoint Name to its SCREAMING_SNAKE PATH_
// constant name (e.g. "configYAML" → "PATH_CONFIG_YAML").
func pathConstName(name string) string {
	var b strings.Builder
	b.WriteString("PATH_")
	runes := []rune(name)
	for i, ru := range runes {
		if ru >= 'A' && ru <= 'Z' {
			if needsWordBreak(runes, i) {
				b.WriteByte('_')
			}
			b.WriteRune(ru)
			continue
		}
		if ru >= 'a' && ru <= 'z' {
			b.WriteRune(ru - 32)
			continue
		}
		b.WriteRune(ru)
	}
	return b.String()
}

// goPathConstName converts an endpoint Name to its Go constant name
// (e.g. "stateStats" → "PathStateStats").
func goPathConstName(name string) string {
	runes := []rune(name)
	return "Path" + strings.ToUpper(string(runes[0])) + string(runes[1:])
}

// tsPathExpr renders the endpoint path as a TS expression: a plain string
// literal when it has no placeholders, otherwise a template literal with
// encodeURIComponent(String(param)) interpolations.
func tsPathExpr(path string, params []string) string {
	if len(params) == 0 {
		return "\"" + tsStringLiteral(path) + "\""
	}
	expr := path
	for _, p := range params {
		expr = strings.Replace(expr, "{"+p+"}", "${encodeURIComponent(String("+p+"))}", 1)
	}
	return "`" + expr + "`"
}

// clientResponse describes the emitted TS response typing of one endpoint.
type clientResponse struct {
	tsType  string // TS type argument (e.g. "Stats", "Stats[]", "string[]")
	decoder string // decoder expression, "" when the response is not decoded
}

// clientResponseFor resolves the response typing and decoder expression for a
// validated KindJSON endpoint, recording used identifiers as it goes.
func (r *Registry) clientResponseFor(e *Endpoint, used *usedIdents) clientResponse {
	shape := e.respShapeOrDefault()
	if shape == RespStringArray {
		used.helper("decodeArray")
		return clientResponse{
			tsType:  "string[]",
			decoder: "(v) => decodeArray(v, (s) => { if (typeof s !== \"string\") { throw new TypeError(\"expected string\"); } return s; }, \"$\")",
		}
	}
	if e.Response == (WireType{}) {
		return clientResponse{}
	}
	tn := r.tsName(e.Response.Name)
	dec := r.decoderName(e.Response.Name)
	used.typeRef(tn)
	used.decoderRef(dec)
	switch shape {
	case RespArray:
		used.helper("decodeArray")
		return clientResponse{tsType: tn + "[]", decoder: "(v) => decodeArray(v, " + dec + ", \"$\")"}
	case RespRecord:
		used.helper("decodeRecord")
		return clientResponse{tsType: "Record<string, " + tn + ">", decoder: "(v) => decodeRecord(v, " + dec + ", \"$\")"}
	default:
		return clientResponse{tsType: tn, decoder: dec}
	}
}

// clientArgs renders the generated function's parameter list and the
// body/query argument expressions.
type clientArgs struct {
	paramList string // full TS parameter list including trailing opts
	bodyExpr  string // expression passed as the transport body argument
	pathExpr  string // TS expression for the request path
}

// clientArgsFor assembles the parameter list for a validated KindJSON
// endpoint: path params first, then the typed/untyped body, then the optional
// query record, then the trailing opts.
func (r *Registry) clientArgsFor(e *Endpoint, used *usedIdents) clientArgs {
	params := pathParams(e.Path)
	var parts []string
	for _, p := range params {
		parts = append(parts, p+": string | number")
	}
	bodyExpr := "undefined"
	if e.Request != (WireType{}) {
		tn := r.tsName(e.Request.Name)
		used.typeRef(tn)
		parts = append(parts, "body: "+tn)
		bodyExpr = "body"
	} else if e.HasBody {
		parts = append(parts, "body: unknown")
		bodyExpr = "body"
	}
	pathExpr := tsPathExpr(e.Path, params)
	if e.Query {
		parts = append(parts, "query?: Record<string, QueryValue>")
		pathExpr += " + qs(query)"
	}
	parts = append(parts, "opts?: ClientOpts")
	return clientArgs{paramList: strings.Join(parts, ", "), bodyExpr: bodyExpr, pathExpr: pathExpr}
}

// clientEmitState accumulates which optional plumbing the emitted functions
// need (query serializer, OK-flag transport, typed transport).
type clientEmitState struct {
	needsQS    bool
	needsOK    bool
	needsTyped bool
}

// emitClientPathConsts writes one PATH_ constant per endpoint. The raw/SSE
// endpoints have no generated function, so their hand-authored call sites
// consume these; JSON endpoints get them too so declarative dispatch layers
// (an actions framework, EventSource wiring) can share the single path table.
// Parameterized paths keep their {name} placeholders.
func (r *Registry) emitClientPathConsts(body *strings.Builder) {
	if len(r.Endpoints) == 0 {
		return
	}
	body.WriteString("// One PATH_ constant per endpoint (placeholders kept verbatim). The\n")
	body.WriteString("// non-JSON flows (kind: raw/sse) have no generated function and are\n")
	body.WriteString("// consumed exclusively through these.\n")
	for i := range r.Endpoints {
		// validateEndpoints guarantees post-conversion uniqueness.
		e := &r.Endpoints[i]
		body.WriteString("export const " + pathConstName(e.Name) + " = \"" + tsStringLiteral(e.Path) + "\";\n")
	}
	body.WriteString("\n")
}

// emitClientFunction writes the function pair for one KindJSON endpoint and
// updates the emit state.
func (r *Registry) emitClientFunction(body *strings.Builder, e *Endpoint, used *usedIdents, st *clientEmitState) {
	args := r.clientArgsFor(e, used)
	resp := r.clientResponseFor(e, used)
	if e.Query {
		st.needsQS = true
	}
	if e.Doc != "" {
		body.WriteString("/** " + tsStringLiteral(e.Doc) + " */\n")
	}
	if resp.tsType == "" {
		// Undecoded response: OK-flag flavor + raw envelope flavor.
		st.needsOK = true
		body.WriteString("export function " + e.Name + "(" + args.paramList + "): Promise<boolean> {\n")
		body.WriteString("  return clientRequestOK(\"" + e.Method + "\", " + args.pathExpr + ", " + args.bodyExpr + ", opts?.signal);\n")
		body.WriteString("}\n\n")
		body.WriteString("export function " + e.Name + "Raw(" + args.paramList + "): Promise<ApiResult<unknown>> {\n")
		body.WriteString("  return clientRequestRaw(\"" + e.Method + "\", " + args.pathExpr + ", " + args.bodyExpr + ", undefined, opts?.signal);\n")
		body.WriteString("}\n\n")
		return
	}
	st.needsTyped = true
	body.WriteString("export function " + e.Name + "(" + args.paramList + "): Promise<" + resp.tsType + " | null> {\n")
	body.WriteString("  return clientRequest(\"" + e.Method + "\", " + args.pathExpr + ", " + args.bodyExpr + ", " + resp.decoder + ", opts?.signal);\n")
	body.WriteString("}\n\n")
	body.WriteString("export function " + e.Name + "Raw(" + args.paramList + "): Promise<ApiResult<" + resp.tsType + ">> {\n")
	body.WriteString("  return clientRequestRaw(\"" + e.Method + "\", " + args.pathExpr + ", " + args.bodyExpr + ", " + resp.decoder + ", opts?.signal);\n")
	body.WriteString("}\n\n")
}

// generateClient writes the typed client module: transport + decoder imports,
// the ClientOpts/QueryValue plumbing, PATH_ constants for the raw/SSE
// endpoints, and one function (pair) per KindJSON endpoint.
func (r *Registry) generateClient(w *strings.Builder) {
	used := newUsedIdents()
	var body strings.Builder
	var st clientEmitState

	r.emitClientPathConsts(&body)
	for i := range r.Endpoints {
		if e := &r.Endpoints[i]; e.kindOrDefault() == KindJSON {
			r.emitClientFunction(&body, e, used, &st)
		}
	}

	needsQS, needsOK, needsTyped := st.needsQS, st.needsOK, st.needsTyped
	w.WriteString(r.HeaderComment)
	r.emitClientImports(w, used, needsOK, needsTyped)

	w.WriteString("/** Options accepted by every generated client function. */\n")
	w.WriteString("export interface ClientOpts {\n  signal?: AbortSignal;\n}\n\n")
	if needsQS {
		w.WriteString("/** Query values: undefined entries are skipped at serialization. */\n")
		w.WriteString("export type QueryValue = string | number | boolean | undefined;\n\n")
		w.WriteString("function qs(query?: Record<string, QueryValue>): string {\n")
		w.WriteString("  if (!query) {\n    return \"\";\n  }\n")
		w.WriteString("  const p = new URLSearchParams();\n")
		w.WriteString("  for (const [k, v] of Object.entries(query)) {\n")
		w.WriteString("    if (v !== undefined) {\n      p.set(k, String(v));\n    }\n")
		w.WriteString("  }\n")
		w.WriteString("  const s = p.toString();\n")
		w.WriteString("  return s === \"\" ? \"\" : `?${s}`;\n")
		w.WriteString("}\n\n")
	}
	w.WriteString(strings.TrimSuffix(body.String(), "\n"))
}

// emitClientImports writes client.gen.ts's import header: the transport
// functions actually used, the ApiResult type, the validators helpers, the
// decoder imports, and the referenced type imports.
func (r *Registry) emitClientImports(w *strings.Builder, used *usedIdents, needsOK, needsTyped bool) {
	var transportFns []string
	if needsTyped {
		transportFns = append(transportFns, "clientRequest")
	}
	if needsOK {
		transportFns = append(transportFns, "clientRequestOK")
	}
	transportFns = append(transportFns, "clientRequestRaw")
	w.WriteString("import { " + strings.Join(transportFns, ", ") + " } from \"" + tsStringLiteral(r.TransportImport) + "\";\n")
	w.WriteString("import type { ApiResult } from \"" + tsStringLiteral(r.TransportImport) + "\";\n")

	var helpers []string
	for _, h := range []string{"decodeArray", "decodeRecord"} {
		if used.helpers[h] {
			helpers = append(helpers, h)
		}
	}
	if len(helpers) > 0 {
		w.WriteString("import { " + strings.Join(helpers, ", ") + " } from \"" + tsStringLiteral(r.ValidatorsImport) + "\";\n")
	}

	decoders := make([]string, 0, len(used.decoders))
	for d := range used.decoders {
		decoders = append(decoders, d)
	}
	sort.Strings(decoders)
	if len(decoders) > 0 {
		mod := strings.TrimSuffix(r.DecodersFilename, ".ts") + ".js"
		w.WriteString("import { " + strings.Join(decoders, ", ") + " } from \"./" + tsStringLiteral(mod) + "\";\n")
	}

	types := make([]string, 0, len(used.types))
	for t := range used.types {
		types = append(types, t)
	}
	sort.Strings(types)
	if len(types) > 0 {
		w.WriteString("import type { " + strings.Join(types, ", ") + " } from \"" + tsStringLiteral(r.TypesImportPath) + "\";\n")
	}
	w.WriteString("\n")
}

// --- Go path constants generation ---

// GenerateGoPaths returns a generated Go source file declaring one Path*
// string constant per endpoint, for the consumer's CLI/server code to share
// the same path table as the TS client. pkgName is the target package name.
// Parameterized paths keep their {name} placeholders verbatim. The output is
// gofmt-formatted. Returns an error on an invalid endpoint table or package
// name (the same validation Generate applies).
func (r *Registry) GenerateGoPaths(pkgName string) (string, error) {
	if err := r.init(); err != nil {
		return "", err
	}
	if err := r.validateEndpoints(); err != nil {
		return "", err
	}
	if pkgName == "" || !isValidTSIdent(pkgName) {
		return "", fmt.Errorf("wiregen: GenerateGoPaths package name %q is not a valid identifier", pkgName)
	}
	var b strings.Builder
	b.WriteString("// Code generated by wiregen. DO NOT EDIT.\n\n")
	b.WriteString("package " + pkgName + "\n\n")
	if len(r.Endpoints) > 0 {
		b.WriteString("// HTTP API path constants generated from the endpoint table.\n")
		b.WriteString("// Parameterized paths keep their {name} placeholders.\n")
		b.WriteString("const (\n")
		for i := range r.Endpoints {
			// validateEndpoints guarantees post-conversion uniqueness.
			e := &r.Endpoints[i]
			b.WriteString("\t" + goPathConstName(e.Name) + " = \"" + e.Path + "\"\n")
		}
		b.WriteString(")\n")
	}
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		// The emitter produced invalid Go — a wiregen bug, not a consumer
		// config error — but it still surfaces as an error, never a panic.
		return "", fmt.Errorf("wiregen: GenerateGoPaths produced unformattable Go: %w", err)
	}
	return string(formatted), nil
}
