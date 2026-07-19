package wiregen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v2"
	"github.com/cplieger/wiregen/v2/testdata/crossref"
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
			err := r.Generate(t.TempDir())
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
	err := r.Generate(t.TempDir())
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
	out := mustGen(t, r.GenerateClient)

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
	out := mustGen(t, r.GenerateClient)

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
	out := mustGen(t, r.GenerateClient)

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
	out := mustGen(t, r.GenerateClient)

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

func TestGenerate_WritesClientFile(t *testing.T) {
	dir := t.TempDir()
	r := newEndpointRegistry()
	r.Endpoints = []wiregen.Endpoint{{
		Name: "getItem", Method: "GET", Path: "/api/item",
		Response: wiregen.TypeRef[crossref.Item](),
	}}
	if err := r.Generate(dir); err != nil {
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
	if err := r2.Generate(dir2); err != nil {
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
