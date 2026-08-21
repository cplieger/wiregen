package wiregen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v3"
	"github.com/cplieger/wiregen/v3/testdata/crossref"
)

// newEndpointRegistry returns a registry with the crossref.Item type
// registered and both required client imports set.
func newEndpointRegistry() *wiregen.Registry {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("../validators.js"),
		wiregen.WithTransportImport("../api-client.js"),
	)
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Item]()}
	return r
}

// --- validation ---

func TestEndpoints_Validation(t *testing.T) {
	item := wiregen.TypeRef[crossref.Item]()
	cases := []struct {
		name    string
		eps     []wiregen.Endpoint
		wantErr string
	}{
		{"bad method", []wiregen.Endpoint{
			{Name: "x", Method: "FETCH", Path: "/api/x"},
		}, "unknown method"},
		{"bad name", []wiregen.Endpoint{
			{Name: "1x", Method: "GET", Path: "/api/x"},
		}, "not a valid TS identifier"},
		{"empty name", []wiregen.Endpoint{
			{Name: "", Method: "GET", Path: "/api/x"},
		}, "not a valid TS identifier"},
		{"duplicate name", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/x"},
			{Name: "x", Method: "POST", Path: "/api/y"},
		}, "duplicate endpoint name"},
		{"relative path", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "api/x"},
		}, "must start with /"},
		{"nested placeholder", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/{a{b}}"},
		}, "nested '{'"},
		{"unbalanced open", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/{id"},
		}, "unbalanced '{'"},
		{"unbalanced close", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/id}"},
		}, "unbalanced '}'"},
		{"empty placeholder", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/{}"},
		}, "not a valid identifier"},
		{"duplicate placeholder", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/{id}/sub/{id}"},
		}, "duplicate placeholder"},
		{"unregistered response", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/x", Response: wiregen.WireType{PkgPath: "p", Name: "Nope"}},
		}, "not a registered type"},
		{"unregistered request", []wiregen.Endpoint{
			{Name: "x", Method: "POST", Path: "/api/x", Request: wiregen.WireType{PkgPath: "p", Name: "Nope"}},
		}, "not a registered type"},
		{"request plus hasbody", []wiregen.Endpoint{
			{Name: "x", Method: "POST", Path: "/api/x", Request: item, HasBody: true},
		}, "both Request and HasBody"},
		{"unknown kind", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/x", Kind: "grpc"},
		}, "unknown kind"},
		{"raw with response", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/x", Kind: wiregen.KindRaw, Response: item},
		}, "cannot bind request/response"},
		{"shape without response", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/x", RespShape: wiregen.RespArray},
		}, "no Response type"},
		{"stringArray with response", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/x", Response: item, RespShape: wiregen.RespStringArray},
		}, "must not set Response"},
		{"unknown shape", []wiregen.Endpoint{
			{Name: "x", Method: "GET", Path: "/api/x", Response: item, RespShape: "blob"},
		}, "unknown response shape"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newEndpointRegistry()
			r.Endpoints = tc.eps
			err := r.Generate(t.Context(), t.TempDir())
			if err == nil {
				t.Fatalf("Generate() = nil error, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Generate() error = %q, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestEndpoints_TransportImportRequired(t *testing.T) {
	r := wiregen.NewRegistry(wiregen.WithValidatorsImport("../validators.js"))
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Item]()}
	r.Endpoints = []wiregen.Endpoint{{Name: "getItem", Method: "GET", Path: "/api/item"}}
	err := r.Generate(t.Context(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "TransportImport") {
		t.Errorf("Generate() error = %v, want TransportImport requirement", err)
	}
}

// --- client generation ---

func TestGenerateClient_TypedGet(t *testing.T) {
	r := newEndpointRegistry()
	r.Endpoints = []wiregen.Endpoint{{
		Name: "getItem", Method: "GET", Path: "/api/item/{id}",
		Response: wiregen.TypeRef[crossref.Item](),
		Doc:      "Fetch one item.",
	}}
	out := mustGenNoLoad(t, r.GenerateClient)

	for _, want := range []string{
		`import { clientRequest, clientRequestRaw } from "../api-client.js";`,
		`import type { ApiResult } from "../api-client.js";`,
		`import { decodeItem } from "./decoders.gen.js";`,
		`import type { Item } from "./types.gen.js";`,
		"/** Fetch one item. */",
		"export function getItem(id: string | number, opts?: ClientOpts): Promise<Item | null> {",
		"return clientRequest(\"GET\", `/api/item/${encodeURIComponent(String(id))}`, undefined, decodeItem, opts?.signal);",
		"export function getItemRaw(id: string | number, opts?: ClientOpts): Promise<ApiResult<Item>> {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("client missing %q:\n%s", want, out)
		}
	}
}

// TestGenerateClient_AdjacentPathPlaceholders pins placeholder extraction for
// two placeholders written with no separator between them (a filename split
// into stem and extension): both become parameters and both are interpolated,
// so the scan must resume inside the path rather than stopping at the second
// '{' it finds at the very start of the remaining path.
func TestGenerateClient_AdjacentPathPlaceholders(t *testing.T) {
	r := newEndpointRegistry()
	r.Endpoints = []wiregen.Endpoint{{
		Name: "exportFile", Method: "GET", Path: "/api/export/{name}{ext}",
		Response: wiregen.TypeRef[crossref.Item](),
	}}
	out := mustGenNoLoad(t, r.GenerateClient)

	for _, want := range []string{
		"export function exportFile(name: string | number, ext: string | number, opts?: ClientOpts): Promise<Item | null> {",
		"`/api/export/${encodeURIComponent(String(name))}${encodeURIComponent(String(ext))}`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("client for path %q missing %q:\n%s", r.Endpoints[0].Path, want, out)
		}
	}
}

// TestGenerateClient_OmitsUnusedImports pins the import header of a client
// that references nothing importable: a raw endpoint gets a path constant and
// no generated function, so no decoder, type or validators helper is used and
// none of those three modules may be imported. An empty import list would emit
// `import { } from "…"`, making the generated client resolve a module it never
// reads.
func TestGenerateClient_OmitsUnusedImports(t *testing.T) {
	r := newEndpointRegistry()
	r.Endpoints = []wiregen.Endpoint{
		{Name: "configYAML", Method: "PUT", Path: "/api/config", Kind: wiregen.KindRaw},
	}
	out := mustGenNoLoad(t, r.GenerateClient)

	for _, unwanted := range []string{
		`from "../validators.js"`,
		`from "./decoders.gen.js"`,
		`from "./types.gen.js"`,
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("client imports %s with nothing to import from it:\n%s", unwanted, out)
		}
	}
}

func TestGenerateClient_ArrayAndRecordShapes(t *testing.T) {
	r := newEndpointRegistry()
	r.Endpoints = []wiregen.Endpoint{
		{
			Name: "listItems", Method: "GET", Path: "/api/items",
			Response: wiregen.TypeRef[crossref.Item](), RespShape: wiregen.RespArray,
		},
		{
			Name: "itemMap", Method: "GET", Path: "/api/item-map",
			Response: wiregen.TypeRef[crossref.Item](), RespShape: wiregen.RespRecord,
		},
		{Name: "names", Method: "GET", Path: "/api/names", RespShape: wiregen.RespStringArray},
	}
	out := mustGenNoLoad(t, r.GenerateClient)

	for _, want := range []string{
		"Promise<Item[] | null>",
		"(v) => decodeArray(v, decodeItem, \"$\")",
		"Promise<Record<string, Item> | null>",
		"(v) => decodeRecord(v, decodeItem, \"$\")",
		"Promise<string[] | null>",
		`import { decodeArray, decodeRecord } from "../validators.js";`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("client missing %q:\n%s", want, out)
		}
	}
}

func TestGenerateClient_BodyQueryAndOKFlavor(t *testing.T) {
	r := newEndpointRegistry()
	r.Endpoints = []wiregen.Endpoint{
		{
			Name: "createItem", Method: "POST", Path: "/api/items",
			Request: wiregen.TypeRef[crossref.Item](), Response: wiregen.TypeRef[crossref.Item](),
		},
		{Name: "triggerScan", Method: "POST", Path: "/api/scan", HasBody: true},
		{
			Name: "search", Method: "GET", Path: "/api/search", Query: true,
			Response: wiregen.TypeRef[crossref.Item](),
		},
	}
	out := mustGenNoLoad(t, r.GenerateClient)

	for _, want := range []string{
		"export function createItem(body: Item, opts?: ClientOpts): Promise<Item | null> {",
		"return clientRequest(\"POST\", \"/api/items\", body, decodeItem, opts?.signal);",
		// No Response registered → OK-flag + raw flavors, no decoder.
		"export function triggerScan(body: unknown, opts?: ClientOpts): Promise<boolean> {",
		"return clientRequestOK(\"POST\", \"/api/scan\", body, opts?.signal);",
		"export function triggerScanRaw(body: unknown, opts?: ClientOpts): Promise<ApiResult<unknown>> {",
		// Query record + qs helper.
		"export function search(query?: Record<string, QueryValue>, opts?: ClientOpts): Promise<Item | null> {",
		"\"/api/search\" + qs(query)",
		"export type QueryValue = string | number | boolean | undefined;",
		"import { clientRequest, clientRequestOK, clientRequestRaw } from",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("client missing %q:\n%s", want, out)
		}
	}
}

func TestGenerateClient_RawAndSSEPathConstants(t *testing.T) {
	r := newEndpointRegistry()
	r.Endpoints = []wiregen.Endpoint{
		{Name: "configYAML", Method: "PUT", Path: "/api/config", Kind: wiregen.KindRaw},
		{Name: "events", Method: "GET", Path: "/api/events", Kind: wiregen.KindSSE},
		{Name: "getItem", Method: "GET", Path: "/api/item", Response: wiregen.TypeRef[crossref.Item]()},
	}
	out := mustGenNoLoad(t, r.GenerateClient)

	for _, want := range []string{
		`export const PATH_CONFIG_YAML = "/api/config";`,
		`export const PATH_EVENTS = "/api/events";`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("client missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "export function configYAML") {
		t.Errorf("raw endpoint must not generate a client function:\n%s", out)
	}
}

// TestGenerateClient_PathConstantCasing pins the SCREAMING_SNAKE path-constant
// name at both ends of each ASCII letter range: every letter is uppercased, and
// an uppercase letter opening a new word takes a leading underscore. Each row
// carries a letter sitting exactly on a range bound ('a', 'z', 'A', 'Z'), where
// an off-by-one in the char class drops a letter out of its branch and leaves
// it unconverted or unseparated.
func TestGenerateClient_PathConstantCasing(t *testing.T) {
	cases := []struct {
		name   string
		epName string
		path   string
		want   string
	}{
		{
			name:   "lowercase_a_and_uppercase_Z_bounds",
			epName: "scanZip",
			path:   "/api/scan-zip",
			want:   `export const PATH_SCAN_ZIP = "/api/scan-zip";`,
		},
		{
			name:   "lowercase_z_and_uppercase_A_bounds",
			epName: "zoneAdd",
			path:   "/api/zone-add",
			want:   `export const PATH_ZONE_ADD = "/api/zone-add";`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newEndpointRegistry()
			r.Endpoints = []wiregen.Endpoint{
				{Name: tc.epName, Method: "GET", Path: tc.path, Kind: wiregen.KindRaw},
			}
			out := mustGenNoLoad(t, r.GenerateClient)
			if !strings.Contains(out, tc.want) {
				t.Errorf("client for endpoint %q missing %q:\n%s", tc.epName, tc.want, out)
			}
		})
	}
}

func TestGenerate_WritesClientFile(t *testing.T) {
	dir := t.TempDir()
	r := newEndpointRegistry()
	r.Endpoints = []wiregen.Endpoint{{
		Name: "getItem", Method: "GET", Path: "/api/item",
		Response: wiregen.TypeRef[crossref.Item](),
	}}
	if err := r.Generate(t.Context(), dir); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "client.gen.ts"))
	if err != nil {
		t.Fatalf("client.gen.ts not written: %v", err)
	}
	if !strings.Contains(string(data), "export function getItem(") {
		t.Errorf("client.gen.ts missing generated function:\n%s", data)
	}

	// Without endpoints the file is not written.
	dir2 := t.TempDir()
	r2 := newEndpointRegistry()
	if err := r2.Generate(t.Context(), dir2); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir2, "client.gen.ts")); !os.IsNotExist(err) {
		t.Errorf("client.gen.ts written with no endpoints registered (stat err=%v)", err)
	}
}

// --- Go path constants ---

func TestGenerateGoPaths(t *testing.T) {
	r := newEndpointRegistry()
	r.Endpoints = []wiregen.Endpoint{
		{
			Name: "stateStats", Method: "GET", Path: "/api/state/stats",
			Response: wiregen.TypeRef[crossref.Item](),
		},
		{Name: "scanSeries", Method: "POST", Path: "/api/scan/series/{id}", HasBody: true},
		{Name: "events", Method: "GET", Path: "/api/events", Kind: wiregen.KindSSE},
	}
	out, err := r.GenerateGoPaths("apipaths")
	if err != nil {
		t.Fatalf("GenerateGoPaths: %v", err)
	}

	// gofmt aligns the const block, so normalize runs of spaces before
	// asserting on the assignments.
	normalized := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		"// Code generated by wiregen. DO NOT EDIT.",
		"package apipaths",
		"PathStateStats = \"/api/state/stats\"",
		"PathScanSeries = \"/api/scan/series/{id}\"",
		"PathEvents = \"/api/events\"",
	} {
		if !strings.Contains(normalized, want) {
			t.Errorf("Go paths missing %q:\n%s", want, out)
		}
	}
}

// TestGenerateGoPaths_NoEndpoints pins the empty-table output: with nothing to
// declare the file is the header and the package clause alone, not a const
// block with no constants in it.
func TestGenerateGoPaths_NoEndpoints(t *testing.T) {
	r := newEndpointRegistry()
	out, err := r.GenerateGoPaths("apipaths")
	if err != nil {
		t.Fatalf("GenerateGoPaths: %v", err)
	}
	want := "// Code generated by wiregen. DO NOT EDIT.\n\npackage apipaths\n"
	if out != want {
		t.Errorf("GenerateGoPaths(%q) with no endpoints = %q, want %q", "apipaths", out, want)
	}
}

func TestGenerateGoPaths_ErrorsOnBadPackage(t *testing.T) {
	r := newEndpointRegistry()
	if _, err := r.GenerateGoPaths("1bad"); err == nil {
		t.Error("GenerateGoPaths(\"1bad\") did not error")
	}
}

func TestGenerateClient_ErrorsWithoutTransport(t *testing.T) {
	r := wiregen.NewRegistry(wiregen.WithValidatorsImport("../validators.js"))
	r.Types = []wiregen.WireType{wiregen.TypeRef[crossref.Item]()}
	r.Endpoints = []wiregen.Endpoint{{Name: "x", Method: "GET", Path: "/api/x"}}
	if _, err := r.GenerateClient(); err == nil {
		t.Error("GenerateClient without TransportImport did not error")
	}
}
