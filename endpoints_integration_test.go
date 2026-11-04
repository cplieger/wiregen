//go:build integration

package wiregen_test

import (
	"os"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/client"
)

// transportTS is the recording transport the generated client imports.
func transportTS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/client/api-client.ts")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const clientMain = `import { recorded, respondWith } from "./api-client.js";
import {
  adminUsers, deleteItem, eventsStreamEndpoint, getItem, listItems, oidcCallbackEndpoint, poster, startJob,
  startJobEndpoint,
} from "./client.gen.js";
import type { StartJobError } from "./client.gen.js";
import { decodeEventFrame } from "./events.gen.js";

const log = (label: string, v: unknown): void => console.log(label + " " + JSON.stringify(v));
const last = () => recorded[recorded.length - 1];

respondWith((spec) => spec.path.startsWith("/api/items/") && spec.method === "GET"
  ? { status: 200, body: { id: "a/b", name: "n" } }
  : { status: 200, body: [{ id: "1", name: "x" }] });
log("getItem", await getItem({ id: "a/b" }));
log("getItem.spec", last());
log("listItems", await listItems({ kind: "movie", limit: 5 }));
log("listItems.spec", last());
await listItems();
log("listItems.bare", last().path);

respondWith(() => ({ status: 202, body: { id: "job-1" } }));
log("startJob", await startJob({ body: { item: "a" }, idempotencyKey: "k" }));
log("startJob.spec", last());
await startJob({ body: { item: "a" } });
log("startJob.nokey", last().headers ?? null);

const codes: readonly StartJobError[] = startJobEndpoint.codes;
log("startJob.codes", codes);

respondWith(() => ({ status: 429, body: { code: "job_capacity" } }));
log("startJob.429", await startJob({ body: { item: "a" } }));
respondWith(() => ({ status: 409, body: { code: "not_listed" } }));
log("startJob.409", await startJob({ body: { item: "a" } }));

respondWith(() => ({ status: 204 }));
log("deleteItem", await deleteItem({ id: "x" }));
log("adminUsers.spec", (await adminUsers({ key: 7 }), last().path));

respondWith(() => ({ status: 200, body: "jpeg" }));
const p = await poster({ id: "r1" });
log("poster", p.ok && p.data instanceof Blob ? [p.data.type, await p.data.text()] : p);
log("poster.spec", last());

log("events.spec", eventsStreamEndpoint.request());
log("oidc.spec", oidcCallbackEndpoint.request());
log("frame", decodeEventFrame("position", { rev: 3 }));
try {
  decodeEventFrame("nope", {});
} catch (e) {
  log("frame.unknown", (e as Error).message);
}
try {
  decodeEventFrame("tick", { at: "x" });
} catch (e) {
  log("frame.bad", (e as Error).message);
}
`

// clientRegistry is a registry over every kind, with params, a query, a
// body, a declared header, an array response, no-body responses and events.
func clientRegistry() *testRegistry {
	r := newTestRegistry(wiregen.WithTransportImport("./api-client.js"))
	params := wiregen.TypeRef[client.ItemParams]()
	internal := wiregen.ErrorRef{Code: "internal_error", Status: 500}
	r.Endpoints = []wiregen.Endpoint{
		{
			Name: "getItem", Method: "GET", Path: "/api/items/{id}", Kind: wiregen.KindJSON,
			Params: params, Response: wiregen.TypeRef[client.Item](),
			Errors: []wiregen.ErrorRef{{Code: "item_unknown", Status: 404}, internal},
		},
		{
			Name: "listItems", Method: "GET", Path: "/api/items", Kind: wiregen.KindJSON,
			Query: wiregen.TypeRef[client.ListQuery](), Response: wiregen.TypeRef[client.Item](), Array: true,
			Errors: []wiregen.ErrorRef{internal},
		},
		{
			Name: "startJob", Method: "POST", Path: "/api/jobs", Kind: wiregen.KindJSON,
			Request: wiregen.TypeRef[client.StartRequest](), Response: wiregen.TypeRef[client.Job](),
			Headers: []wiregen.Header{{Name: "Idempotency-Key", Arg: "idempotencyKey", MinBytes: 1, MaxBytes: 255}},
			Errors:  []wiregen.ErrorRef{{Code: "job_capacity", Status: 429}, internal},
		},
		{
			Name: "deleteItem", Method: "DELETE", Path: "/api/items/{id}", Kind: wiregen.KindJSON,
			Params: params, Errors: []wiregen.ErrorRef{internal},
		},
		{
			Name: "adminUsers", Method: "GET", Path: "/api/users", Kind: wiregen.KindJSON,
			Query: wiregen.TypeRef[client.RequiredQuery](), Response: wiregen.TypeRef[client.Item](), Array: true,
		},
		{
			Name: "poster", Method: "GET", Path: "/api/items/{id}/poster", Kind: wiregen.KindRaw,
			Params: params, ContentType: "image/jpeg", Errors: []wiregen.ErrorRef{internal},
		},
		{Name: "eventsStream", Method: "GET", Path: "/api/events", Kind: wiregen.KindEventStream},
		{Name: "oidcCallback", Method: "GET", Path: "/api/oidc/callback", Kind: wiregen.KindRedirect},
	}
	r.Events = []wiregen.Event{
		{Name: "position", Data: wiregen.TypeRef[client.Position]()},
		{Name: "tick", Data: wiregen.TypeRef[client.Tick]()},
	}
	r.Enums = []wiregen.EnumRef{wiregen.StringEnum[client.Kind]()}
	r.ErrorEnvelope = wiregen.TypeRef[client.ErrorBody]()
	return r
}

// TestClientAgainstTransport compiles the generated client against a
// transport exporting only clientRequest, EndpointSpec and ApiResult, and
// runs every endpoint kind through it under node.
func TestClientAgainstTransport(t *testing.T) {
	out := runTS(t, clientRegistry(), map[string]string{"api-client.ts": transportTS(t), "main.ts": clientMain})
	want := strings.Join([]string{
		`getItem {"ok":true,"data":{"id":"a/b","name":"n"}}`,
		`getItem.spec {"method":"GET","path":"/api/items/a%2Fb","codes":["item_unknown","internal_error"]}`,
		`listItems {"ok":true,"data":[{"id":"1","name":"x"}]}`,
		`listItems.spec {"method":"GET","path":"/api/items?kind=movie&limit=5","codes":["internal_error"]}`,
		`listItems.bare "/api/items"`,
		`startJob {"ok":true,"data":{"id":"job-1"}}`,
		`startJob.spec {"method":"POST","path":"/api/jobs","body":{"item":"a"},"headers":{"Idempotency-Key":"k"},"codes":["job_capacity","internal_error"]}`,
		`startJob.nokey {}`,
		`startJob.codes ["job_capacity","internal_error"]`,
		`startJob.429 {"ok":false,"status":429,"code":"job_capacity"}`,
		`startJob.409 {"ok":false,"status":409,"code":"http"}`,
		`deleteItem {"ok":true}`,
		`adminUsers.spec "/api/users?key=7"`,
		`poster ["image/jpeg","jpeg"]`,
		`poster.spec {"method":"GET","path":"/api/items/r1/poster","raw":true,"codes":["internal_error"]}`,
		`events.spec {"method":"GET","path":"/api/events","codes":[]}`,
		`oidc.spec {"method":"GET","path":"/api/oidc/callback","codes":[]}`,
		`frame {"event":"position","data":{"rev":3}}`,
		`frame.unknown "unknown event \"nope\""`,
		`frame.bad "$.tick.at: expected number, got string"`,
		"",
	}, "\n")
	if out != want {
		t.Errorf("node output:\n%s\nwant:\n%s", out, want)
	}
}

// TestClientRefusedByTypes pins what the generated types forbid: each
// fixture must fail tsc naming its cause.
func TestClientRefusedByTypes(t *testing.T) {
	importing := func(body string) string {
		return `import { getItem, listItems, startJob } from "./client.gen.js";
import type { StartJobError } from "./client.gen.js";
void getItem; void listItems; void startJob;
` + body
	}
	cases := []struct {
		name, main, want string
		transport        func(string) string
	}{
		{"header argument on an endpoint without it", importing(`void listItems({ idempotencyKey: "k" });`), "idempotencyKey", nil},
		{"code outside the union", importing(`const e: StartJobError = "item_unknown"; void e;`), `"item_unknown"`, nil},
		{"function for an event stream", `import { eventsStream } from "./client.gen.js"; void eventsStream;`, "eventsStream", nil},
		{"function for a redirect", `import { oidcCallback } from "./client.gen.js"; void oidcCallback;`, "oidcCallback", nil},
		{"required param missing", importing(`void getItem({});`), "id", nil},
		{"transport without codes", importing(""), "codes", func(ts string) string {
			return strings.Replace(ts, "  readonly codes: readonly string[];\n", "", 1)
		}},
		{"transport with the v3 functions only", importing(""), "clientRequest", func(ts string) string {
			return strings.Replace(ts, "export function clientRequest<", "export function clientRequestV3<", 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport := transportTS(t)
			if tc.transport != nil {
				transport = tc.transport(transport)
			}
			tscFails(t, clientRegistry(), map[string]string{"api-client.ts": transport, "main.ts": tc.main}, tc.want)
		})
	}
}
