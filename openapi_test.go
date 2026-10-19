package wiregen_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/client"
	"github.com/cplieger/wiregen/v4/testdata/openapi"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// docURL is where the tests load the emitted document from.
const docURL = "file:///openapi.json"

// fullReg is a registry using every part of the document: each kind, params,
// query, headers, errors, security, extensions, events, an envelope, a union,
// enums, bounds and a mapping.
func fullReg() *testRegistry {
	r := newTestRegistry(wiregen.WithTransportImport("./api-client.js"))
	r.Types = []wiregen.WireType{
		wiregen.TypeRef[openapi.Item](), wiregen.TypeRef[openapi.Shared](),
		wiregen.TypeRef[openapi.Detail](), wiregen.TypeRef[openapi.Create](),
	}
	r.Enums = []wiregen.EnumRef{wiregen.TextEnum[openapi.Level](), wiregen.StringEnum[openapi.Kind]()}
	r.Unions = []wiregen.Union{{
		Type: wiregen.TypeRef[openapi.Shift](), Tag: "kind",
		Cases: map[string][]string{"none": {}, "shift": {"ms"}},
	}}
	r.Mappings = map[string]wiregen.Mapping{
		openapiPkg + ".Stamp": {TS: "number", Decoder: "reqNum", Schema: map[string]any{"type": "integer"}},
	}
	r.ErrorEnvelope = wiregen.TypeRef[openapi.Problem]()
	r.Events = []wiregen.Event{{Name: "tick", Data: wiregen.TypeRef[openapi.Tick]()}}
	r.OpenAPI = wiregen.OpenAPIInfo{
		Title: "Fixture API", Version: "0", Description: "The fixture.",
		SecuritySchemes: map[string]wiregen.SecurityScheme{
			"bearer":  {Type: "http", Scheme: "bearer"},
			"session": {Type: "apiKey", In: "cookie", Name: "sid"},
		},
	}
	params := wiregen.TypeRef[openapi.ItemParams]()
	internal := wiregen.ErrorRef{Code: "internal_error", Status: 500}
	r.Endpoints = []wiregen.Endpoint{
		{
			Name: "getItem", Method: "GET", Path: "/api/items/{id}", Kind: wiregen.KindJSON,
			Params: params, Response: wiregen.TypeRef[openapi.Detail](),
			Summary: "Read an item.", Description: "The whole row.",
			Errors: []wiregen.ErrorRef{
				{Code: "not_found", Status: 404, Extensions: map[string]any{"x-no-effect": true}},
				{Code: "gone", Status: 404},
				internal,
			},
			Security:   []string{"bearer", "session"},
			Extensions: map[string]any{"x-subflux-access": "read"},
		},
		{
			Name: "listItems", Method: "GET", Path: "/api/items", Kind: wiregen.KindJSON,
			Query: wiregen.TypeRef[openapi.ListQuery](), Response: wiregen.TypeRef[openapi.Item](), Array: true,
		},
		{
			Name: "createItem", Method: "POST", Path: "/api/items", Kind: wiregen.KindJSON, Status: 202,
			Request: wiregen.TypeRef[openapi.Create](), Response: wiregen.TypeRef[openapi.Item](),
			Headers: []wiregen.Header{{Name: "Idempotency-Key", Arg: "idempotencyKey", MinBytes: 1, MaxBytes: 255, Description: "Replays the job."}},
			Errors:  []wiregen.ErrorRef{{Code: "body_invalid", Status: 400}, internal},
		},
		{Name: "deleteItem", Method: "DELETE", Path: "/api/items/{id}", Kind: wiregen.KindJSON, Params: params},
		{
			Name: "retime", Method: "PUT", Path: "/api/items/{id}/timing", Kind: wiregen.KindJSON, Params: params,
			Request: wiregen.TypeRef[openapi.Retime](), Response: wiregen.TypeRef[openapi.Stamped](),
		},
		{Name: "poster", Method: "GET", Path: "/api/items/{id}/poster", Kind: wiregen.KindRaw, Params: params, ContentType: "image/jpeg"},
		{Name: "events", Method: "GET", Path: "/api/events", Kind: wiregen.KindEventStream},
		{Name: "signIn", Method: "GET", Path: "/api/sign-in", Kind: wiregen.KindRedirect, Status: 303},
	}
	return r
}

// compiled compiles the schema at ref in doc, the emitted document.
func compiled(t testing.TB, doc, ref string) *jsonschema.Schema {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(docURL, v); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(docURL + ref)
	if err != nil {
		t.Fatalf("compile %s: %v", ref, err)
	}
	return s
}

// validates returns why the JSON body fails s, or nil.
func validates(t testing.TB, s *jsonschema.Schema, body string) error {
	t.Helper()
	return s.Validate(instance(t, body))
}

// instance decodes body as jsonschema reads an instance.
func instance(t testing.TB, body string) any {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// mustJSON encodes v as encoding/json does.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOpenAPIDocumentValidatesAgainstOAS31(t *testing.T) {
	doc := mustGen(t, fullReg().openapiFile)
	oas, err := os.ReadFile(filepath.Join("testdata", "openapi", "oas-3.1-schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	ov, err := jsonschema.UnmarshalJSON(bytes.NewReader(oas))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	const oasURL = "https://spec.openapis.org/oas/3.1/schema/2022-10-07"
	if err := c.AddResource(oasURL, ov); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(oasURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := validates(t, s, doc); err != nil {
		t.Errorf("document fails the OAS 3.1 schema: %v\n%s", err, doc)
	}
	var d struct {
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatal(err)
	}
	for name := range d.Components.Schemas {
		compiled(t, doc, "#/components/schemas/"+name) // a 2020-12 schema, checked by its metaschema
	}
}

// TestOpenAPIComponents pins which components exist: each type in the
// directions it crosses, the request form suffixed Input where the two differ.
func TestOpenAPIComponents(t *testing.T) {
	doc := mustGen(t, fullReg().openapiFile)
	got := componentNames(t, doc)
	want := []string{
		"CreateInput", "Detail", "Item", "Kind", "Level", "Problem", "RetimeInput",
		"Shared", "SharedInput", "ShiftInput", "ShiftInput.none", "ShiftInput.shift", "Stamped", "Tick",
	}
	if !slices.Equal(got, want) {
		t.Errorf("components = %v, want %v", got, want)
	}
	if lv := component(t, doc, "Level"); mustJSON(t, lv) != `{"enum":["low","high"],"type":"string"}` {
		t.Errorf("Level = %v, want exactly the named members", lv)
	}
	t.Run("a type no endpoint reaches keeps its response form", func(t *testing.T) {
		if got := componentNames(t, mustGen(t, boundsReg().openapiFile)); !slices.Equal(got, []string{"Create", "Item", "Level", "Shared"}) {
			t.Errorf("components = %v, want the response forms only", got)
		}
	})
}

// componentNames returns doc's component schema names, sorted.
func componentNames(t *testing.T, doc string) []string {
	t.Helper()
	var d struct {
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range d.Components.Schemas {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// TestNullabilityByDirection holds the schemas to encoding/json in each
// direction, validating what the encoder writes and the decoder admits.
func TestNullabilityByDirection(t *testing.T) {
	doc := mustGen(t, fullReg().openapiFile)
	detail := compiled(t, doc, "#/components/schemas/Detail")
	create := compiled(t, doc, "#/components/schemas/CreateInput")
	full := openapi.Detail{Level: openapi.LevelHigh, Raw: json.RawMessage(`{"a":1}`)}
	validCreate := map[string]any{"labels": map[string]string{}, "files": []string{"a"}, "shared": map[string]any{}, "limit": 1, "level": "low"}
	with := func(k string, v any) string {
		m := maps.Clone(validCreate)
		m[k] = v
		return mustJSON(t, m)
	}
	t.Run("a response nil pointer is required and null", func(t *testing.T) {
		if err := validates(t, detail, mustJSON(t, full)); err != nil {
			t.Errorf("the encoding of a zero Detail fails: %v", err)
		}
		if !slices.Contains(component(t, doc, "Detail")["required"].([]any), any("item")) {
			t.Error("Detail.item is not required, but the encoder always writes it")
		}
		if err := validates(t, detail, strings.Replace(mustJSON(t, full), `"item":null,`, "", 1)); err == nil {
			t.Error("a Detail without item validates")
		}
	})
	t.Run("a request pointer is optional", func(t *testing.T) {
		if err := validates(t, create, mustJSON(t, validCreate)); err != nil {
			t.Errorf("a Create without item fails: %v", err)
		}
	})
	t.Run("a response slice is null or empty", func(t *testing.T) {
		empty := full
		empty.Items = []openapi.Item{}
		for _, v := range []openapi.Detail{full, empty} {
			if err := validates(t, detail, mustJSON(t, v)); err != nil {
				t.Errorf("%s fails: %v", mustJSON(t, v), err)
			}
		}
	})
	t.Run("a request slice with a lower Count bound refuses null", func(t *testing.T) {
		if err := validates(t, create, with("files", nil)); err == nil {
			t.Error(`{"files": null} validates`)
		}
		if got := decodeVerdicts(t, boundsReg(), "decodeCreate", []string{with("files", nil)}); !strings.Contains(got[0], "expected array, got null") {
			t.Errorf("the generated validator answered %q to null files", got[0])
		}
	})
	t.Run("a request map is nullable", func(t *testing.T) {
		if err := validates(t, create, with("labels", nil)); err != nil {
			t.Errorf(`{"labels": null} fails: %v`, err)
		}
	})
	t.Run("a omitempty response field is not nullable", func(t *testing.T) {
		if err := validates(t, detail, strings.Replace(mustJSON(t, full), `"item":null`, `"item":null,"note":null`, 1)); err == nil {
			t.Error("a null note validates, but the encoder omits a nil one")
		}
	})
	t.Run("differing schemas get an Input component", func(t *testing.T) {
		shared, input := component(t, doc, "Shared"), component(t, doc, "SharedInput")
		if !slices.Contains(shared["required"].([]any), any("hint")) || input["required"] != nil {
			t.Errorf("Shared required %v, SharedInput required %v; want hint only in the response form", shared["required"], input["required"])
		}
		if got := prop(t, component(t, doc, "CreateInput"), "shared"); got != `{"$ref":"#/components/schemas/SharedInput"}` {
			t.Errorf("CreateInput.shared = %s, want the Input form", got)
		}
	})
}

func TestUnionDiscriminator(t *testing.T) {
	doc := mustGen(t, fullReg().openapiFile)
	if got, want := mustJSON(t, component(t, doc, "ShiftInput")), `{"discriminator":{"mapping":{"none":"#/components/schemas/ShiftInput.none","shift":"#/components/schemas/ShiftInput.shift"},"propertyName":"kind"},"oneOf":[{"$ref":"#/components/schemas/ShiftInput.none"},{"$ref":"#/components/schemas/ShiftInput.shift"}]}`; got != want {
		t.Errorf("ShiftInput = %s, want %s", got, want)
	}
	s := compiled(t, doc, "#/components/schemas/ShiftInput")
	for body, ok := range map[string]bool{
		`{"kind":"none"}`:           true,
		`{"kind":"shift","ms":3}`:   true,
		`{"kind":"shift"}`:          true,
		`{"kind":"none","ms":3}`:    false,
		`{"kind":"other"}`:          false,
		`{"ms":3}`:                  false,
		`{"kind":"shift","x":1}`:    false,
		`{"kind":"shift","ms":"3"}`: false,
	} {
		if err := validates(t, s, body); (err == nil) != ok {
			t.Errorf("%s: valid = %v, want %v (%v)", body, err == nil, ok, err)
		}
	}
}

// operation returns the operation at method and path of doc.
func operation(t *testing.T, doc, method, path string) map[string]any {
	t.Helper()
	var d struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatal(err)
	}
	op, ok := d.Paths[path][method]
	if !ok {
		t.Fatalf("no %s %s in:\n%s", method, path, doc)
	}
	return op
}

func TestSecurityAndExtensions(t *testing.T) {
	doc := mustGen(t, fullReg().openapiFile)
	get := operation(t, doc, "get", "/api/items/{id}")
	if got := mustJSON(t, get["security"]); got != `[{"bearer":[]},{"session":[]}]` {
		t.Errorf("getItem security = %s", got)
	}
	if get["x-subflux-access"] != "read" {
		t.Errorf("getItem lacks its extension: %v", get)
	}
	if got := mustJSON(t, operation(t, doc, "get", "/api/items")["security"]); got != `[]` {
		t.Errorf("a public operation's security = %s, want []", got)
	}
	var d struct {
		Components struct {
			SecuritySchemes map[string]any `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, d.Components.SecuritySchemes); got != `{"bearer":{"scheme":"bearer","type":"http"},"session":{"in":"cookie","name":"sid","type":"apiKey"}}` {
		t.Errorf("securitySchemes = %s", got)
	}
	t.Run("an undeclared scheme is refused", func(t *testing.T) {
		r := fullReg()
		r.Endpoints[0].Security = []string{"oauth"}
		requireGenerateError(t, r, `names security scheme "oauth"`)
	})
	t.Run("a malformed scheme is refused", func(t *testing.T) {
		r := fullReg()
		r.OpenAPI.SecuritySchemes["bad"] = wiregen.SecurityScheme{Type: "apiKey", In: "body", Name: "k"}
		requireGenerateError(t, r, `security scheme "bad": type apiKey needs a Name and In of header, query or cookie`)
	})
}

func TestOpenAPIOperations(t *testing.T) {
	doc := mustGen(t, fullReg().openapiFile)
	for _, tc := range []struct {
		name, method, path, key, want string
	}{
		{"path and header parameters", "post", "/api/items", "parameters", `[{"description":"Replays the job.","in":"header","name":"Idempotency-Key","required":false,"schema":{"maxLength":255,"minLength":1,"pattern":"^[\\x20-\\x7E]*$","type":"string"}}]`},
		{"query parameters", "get", "/api/items", "parameters", `[{"in":"query","name":"limit","required":false,"schema":{"maximum":500,"minimum":1,"type":"integer"}},{"in":"query","name":"cursor","required":true,"schema":{"type":"string"}}]`},
		{"path parameter", "delete", "/api/items/{id}", "parameters", `[{"in":"path","name":"id","required":true,"schema":{"type":"integer"}}]`},
		{"request body", "post", "/api/items", "requestBody", `{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/CreateInput"}}},"required":true}`},
		{"declared success status", "post", "/api/items", "responses", `{"202":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Item"}}},"description":"Accepted"},"400":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Problem"}}},"description":"Bad Request: body_invalid","x-error-codes":{"body_invalid":{}}},"500":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Problem"}}},"description":"Internal Server Error: internal_error","x-error-codes":{"internal_error":{}}}}`},
		{"errors grouped by status", "get", "/api/items/{id}", "responses", `{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Detail"}}},"description":"OK"},"404":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Problem"}}},"description":"Not Found: not_found, gone","x-error-codes":{"gone":{},"not_found":{"x-no-effect":true}}},"500":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Problem"}}},"description":"Internal Server Error: internal_error","x-error-codes":{"internal_error":{}}}}`},
		{"array response", "get", "/api/items", "responses", `{"200":{"content":{"application/json":{"schema":{"items":{"$ref":"#/components/schemas/Item"},"type":"array"}}},"description":"OK"}}`},
		{"no body is 204", "delete", "/api/items/{id}", "responses", `{"204":{"description":"No Content"}}`},
		{"raw", "get", "/api/items/{id}/poster", "responses", `{"200":{"content":{"image/jpeg":{}},"description":"OK"}}`},
		{"event stream", "get", "/api/events", "responses", `{"200":{"content":{"text/event-stream":{"schema":{"type":"string"},"x-events":{"tick":{"$ref":"#/components/schemas/Tick"}}}},"description":"OK"}}`},
		{"redirect", "get", "/api/sign-in", "responses", `{"303":{"description":"See Other","headers":{"Location":{"required":true,"schema":{"format":"uri-reference","type":"string"}}}}}`},
		{"summary", "get", "/api/items/{id}", "summary", `"Read an item."`},
		{"operation id", "get", "/api/items/{id}", "operationId", `"getItem"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustJSON(t, operation(t, doc, tc.method, tc.path)[tc.key]); got != tc.want {
				t.Errorf("%s = %s\nwant %s", tc.key, got, tc.want)
			}
		})
	}
	if got := prop(t, component(t, doc, "Stamped"), "at"); got != `{"type":"integer"}` {
		t.Errorf("a mapped field's schema = %s, want the mapping's", got)
	}
}

func TestEndpointStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*wiregen.Endpoint)
		want string
	}{
		{"204 with a body", func(e *wiregen.Endpoint) { e.Status = 204 }, "204 is exactly the status of a JSON endpoint with no Response"},
		{"200 without a body", func(e *wiregen.Endpoint) { e.Response = wiregen.WireType{}; e.Status = 200 }, "204 is exactly"},
		{"3xx on JSON", func(e *wiregen.Endpoint) { e.Status = 302 }, "success status 302, outside 2xx"},
		{"205", func(e *wiregen.Endpoint) { e.Status = 205 }, "success status 205"},
		{"2xx on a redirect", func(e *wiregen.Endpoint) {
			e.Kind, e.Response, e.Status = wiregen.KindRedirect, wiregen.WireType{}, 200
		}, "is kind redirect and has status 200"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := get("x", "/api/x")
			e.Response = wiregen.TypeRef[client.Item]()
			tc.edit(&e)
			r := newEndpointRegistry(e)
			requireGenerateError(t, r, tc.want)
		})
	}
}

func TestOpenAPIRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*testRegistry)
		want string
	}{
		{"no title", func(r *testRegistry) { r.OpenAPI.Title = "" }, "needs Registry.OpenAPI's Title and Version"},
		{"mapping without a schema", func(r *testRegistry) {
			r.Mappings[openapiPkg+".Stamp"] = wiregen.Mapping{TS: "number"}
		}, "mapping " + openapiPkg + ".Stamp has no Schema"},
		{"one route twice", func(r *testRegistry) { r.Endpoints[1].Path = "/api/events" }, "endpoints listItems and events are both GET /api/events"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fullReg()
			tc.edit(r)
			_, err := r.openapiFile(t.Context())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestGenerateAtomicPass: the TS files and the OpenAPI documents are staged
// together, so an OpenAPI path that cannot be written leaves every target as
// it was and no staged temp behind.
func TestGenerateAtomicPass(t *testing.T) {
	root := t.TempDir()
	tsDir := filepath.Join(root, "wire")
	if err := os.Mkdir(tsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(tsDir, "types.gen.ts")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(root, "docs")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(root, "openapi.json")
	r := fullReg()
	err := r.Generate(t.Context(), append(r.opts, wiregen.WithTSDir(tsDir),
		wiregen.WithOpenAPIFiles(good, filepath.Join(blocker, "openapi.json")))...)
	if err == nil {
		t.Fatal("Generate wrote through a path whose parent is a file")
	}
	if b, _ := os.ReadFile(old); string(b) != "old" {
		t.Errorf("types.gen.ts = %q, want it untouched", b)
	}
	for _, dir := range []string{root, tsDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if n := e.Name(); n != "wire" && n != "docs" && n != "types.gen.ts" {
				t.Errorf("%s left behind in %s", n, dir)
			}
		}
	}
	t.Run("a good pass writes every output", func(t *testing.T) {
		second := filepath.Join(root, "copy.json")
		if err := r.Generate(t.Context(), append(r.opts, wiregen.WithTSDir(tsDir), wiregen.WithOpenAPIFiles(good, second))...); err != nil {
			t.Fatal(err)
		}
		a, errA := os.ReadFile(good)
		b, errB := os.ReadFile(second)
		if errA != nil || errB != nil || !bytes.Equal(a, b) || !json.Valid(a) {
			t.Errorf("the two documents differ or are not JSON (%v, %v)", errA, errB)
		}
	})
}

// Every $ref the document writes resolves to a component, including from a
// type reached from no endpoint that names a request-only or a
// parameters-only type.
func TestOpenAPIRefsResolve(t *testing.T) {
	probe := fullReg()
	probe.Types = append(probe.Types, wiregen.TypeRef[openapi.Wrapper]())
	probe.Endpoints = append(probe.Endpoints, wiregen.Endpoint{
		Name: "putInner", Method: "PUT", Path: "/api/inner/{id}", Kind: wiregen.KindJSON,
		Params: wiregen.TypeRef[openapi.ItemParams](), Request: wiregen.TypeRef[openapi.Inner](),
		Response: wiregen.TypeRef[openapi.Item](),
	})
	for name, r := range map[string]*testRegistry{"full": fullReg(), "unreached": probe} {
		t.Run(name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal([]byte(mustGen(t, r.openapiFile)), &doc); err != nil {
				t.Fatal(err)
			}
			schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
			for _, ref := range collectRefs(doc) {
				if _, ok := schemas[strings.TrimPrefix(ref, "#/components/schemas/")]; !ok {
					t.Errorf("$ref %s resolves to no component", ref)
				}
			}
			if _, ok := schemas["Wrapper"]; name == "unreached" && !ok {
				t.Error("a registered type reached from no endpoint is not documented")
			}
		})
	}
}

// collectRefs returns every $ref value under v.
func collectRefs(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if s, ok := child.(string); ok && k == "$ref" {
				out = append(out, s)
				continue
			}
			out = append(out, collectRefs(child)...)
		}
	case []any:
		for _, child := range x {
			out = append(out, collectRefs(child)...)
		}
	}
	return out
}

// A $ref the document would carry, such as one a Mappings Schema gives, that
// is not a string or not an RFC 6901 pointer to a value in the document is
// refused before any file is written; one that names a value passes.
func TestOpenAPI_refusesDanglingRef(t *testing.T) {
	cases := map[string]struct {
		ref  any
		want string
	}{
		"a missing component":          {"#/components/schemas/Missing", `$ref "#/components/schemas/Missing" at /components/schemas/Stamped/properties/at`},
		"another document":             {"https://example.com/stamp.json", `$ref "https://example.com/stamp.json"`},
		"a number":                     {1, `the $ref at /components/schemas/Stamped/properties/at is not a string`},
		"an index with a leading zero": {"#/components/schemas/ShiftInput/oneOf/01", `$ref "#/components/schemas/ShiftInput/oneOf/01"`},
		"an index with a sign":         {"#/components/schemas/ShiftInput/oneOf/+0", `$ref "#/components/schemas/ShiftInput/oneOf/+0"`},
		"the index past the end":       {"#/components/schemas/ShiftInput/oneOf/-", `$ref "#/components/schemas/ShiftInput/oneOf/-"`},
		"a tilde escaping nothing":     {"#/paths/~1api~1items~1{id}/get/x-keys/a~b", `$ref "#/paths/~1api~1items~1{id}/get/x-keys/a~b"`},
		"a tilde escaping a tilde":     {"#/paths/~1api~1items~1{id}/get/x-keys/a~0b", ""},
		"an emitted component":         {"#/components/schemas/Item", ""},
		"an array element":             {"#/components/schemas/ShiftInput/oneOf/0", ""},
		"an escaped name":              {"#/paths/~1api~1items~1{id}", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := fullReg()
			r.Mappings = map[string]wiregen.Mapping{
				openapiPkg + ".Stamp": {TS: "number", Decoder: "reqNum", Schema: map[string]any{"$ref": tc.ref}},
			}
			r.Endpoints[0].Extensions = map[string]any{"x-keys": map[string]any{"a~b": 1}}
			dir := t.TempDir()
			tsDir := filepath.Join(dir, "ts")
			err := r.Generate(t.Context(), append(r.opts, wiregen.WithTSDir(tsDir), wiregen.WithOpenAPIFiles(filepath.Join(dir, "openapi.json")))...)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Generate refused a $ref to an emitted component: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Generate = %v, want a refusal naming %s", err, tc.want)
			}
			if entries, _ := os.ReadDir(dir); len(entries) > 0 {
				t.Errorf("a refused Generate wrote %d entries", len(entries))
			}
		})
	}
}
