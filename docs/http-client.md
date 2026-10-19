# Typed HTTP client

This page is for a developer whose front end calls a Go JSON API. It shows how to put your HTTP endpoints in the registry, so wiregen writes a typed client and an OpenAPI document from the same table your server binds its routes from.

## The endpoint table

The table is authoritative: build the server's routes, the client and the document from the same values, so no second route list can drift from it.

```go
type Endpoint struct {
    Extensions  map[string]any // OpenAPI x- members of the operation
    Params      WireType       // the path placeholders' struct
    Query       WireType       // the query string's struct
    Request     WireType       // the JSON request body
    Response    WireType       // the JSON response body; zero: none
    Name        string         // a TS identifier: the function's name
    Method      string         // GET, POST, PUT, PATCH or DELETE
    Path        string         // "/api/items/{id}"
    ContentType string         // the 2xx media type; KindRaw only, and required there
    Summary     string
    Description string
    Headers     []Header
    Errors      []ErrorRef
    Security    []string // OpenAPI security scheme names; nil: public
    Status      int      // the success status; zero: the kind's default
    Array       bool     // the body is a JSON array of Response
    Kind        Kind     // KindJSON, KindRaw, KindEventStream or KindRedirect
}

type ErrorRef struct {
    Extensions map[string]any // OpenAPI x- members of the code's entry
    Code       string         // lower snake case
    Status     int            // 400 to 599
}

type Header struct {
    Name, Arg, Description string // Arg: the TS argument's name
    MinBytes, MaxBytes     int    // printable ASCII, so bytes are characters
    Required               bool
}
```

Every type the table names is generated as if listed in `Types`.

- `Params` holds the path placeholders: its fields' wire names are exactly the `Path`'s `{name}` segments, each required. `Params` and `Query` hold only string, number, boolean and enum fields.
- `Kind` is required:
  - `KindJSON` answers `Response` as JSON, or no body when `Response` is zero.
  - `KindRaw` answers a body of `ContentType`, which the client reads as a `Blob`.
  - `KindEventStream` answers `text/event-stream` carrying `Registry.Events`.
  - `KindRedirect` answers a redirect a browser navigation follows.
- `Status` is the success status. Zero is 200, 204 for a `KindJSON` endpoint with no `Response`, and 302 for a redirect. A redirect's is 301, 302, 303, 307 or 308. Any other kind's is a 2xx, and 204 exactly when there is no body.
- A code means one thing across the registry, so every endpoint listing it gives the same `Status` and `Extensions`.

## The generated client

`client.gen.ts` holds, for each endpoint:

- `<Name>Args`, a readonly interface of its path parameters, query fields, `body` and header arguments.
- `<Name>Error`, the union of its error codes.
- `<name>Endpoint`, a descriptor `{method, path, codes, request(args), decode}`, whose `request` returns the transport's `EndpointSpec`: placeholders percent-encoded, the query folded into `path` with undefined values skipped, `raw: true` on a raw endpoint, the body, the declared headers when given, and `codes`, the error union's members.
- and for `KindJSON` and `KindRaw` endpoints, a function returning `globalThis.Promise<ApiResult<T, <Name>Error>>`.

```ts
export interface CreateItemArgs {
  readonly body: Create;
  readonly idempotencyKey?: string;
}

export function createItem(args: CreateItemArgs, opts?: ClientOpts): globalThis.Promise<ApiResult<Item, CreateItemError>> {
  return clientRequest<Item, CreateItemError>(createItemEndpoint.request(args), createItemEndpoint.decode, opts);
}
```

An event-stream or redirect endpoint gets its descriptor only, whose `request` names the URL to open or navigate to. The arguments object may be omitted when every member is optional. `ClientOpts` carries an optional `signal`.

## Declared headers

A `Header` is a typed string argument of its endpoint's function, named `Arg`, which the descriptor puts in `EndpointSpec.headers` under `Name`. It is also a header parameter in the OpenAPI document with `minLength`, `maxLength` and a printable-ASCII pattern. An endpoint without the header has no such argument, so `tsc` refuses a call that passes one.

## Validation

The table is validated before any file is written, and each failure is a named error from `Generate`. It rejects:

- a name that is not a valid TypeScript identifier, or an emitted identifier that collides with another generated name (see [Identifiers](type-mapping.md#identifiers)).
- an unknown method, a missing or unknown kind, or two endpoints with one method and path, placeholder names aside.
- two paths that differ only in placeholder names, such as `/a/{id}` and `/a/{name}`, which [OpenAPI forbids](https://spec.openapis.org/oas/v3.1.0#paths-object).
- a path that does not start with `/`, holds a character outside RFC 3986's path characters or a `%` not followed by two hex digits, or has a malformed, nested or repeated placeholder.
- `Params` set without placeholders or the reverse, a `Params` field that is optional or names no placeholder, or a URL argument that is not a scalar.
- two arguments of one name, such as a query field named `body` beside a body.
- a `Request` on a GET, a `Response` or `Array` on a non-JSON kind, a `ContentType` anywhere but a raw endpoint, an event-stream or redirect endpoint that is not a GET, or a redirect with headers.
- a success status that does not fit the kind.
- a header whose name is not an RFC 9110 token, whose `Arg` is not an identifier, or whose byte bounds are inverted, or a header or `Arg` declared twice.
- an error code that is not lower snake case, is listed twice, has a status outside 400 to 599, or differs from another endpoint's listing, or an extension key that does not start with `x-`.

## Transport module

The module at `WithTransportImport` is yours. It must export:

- `type EndpointSpec = {method, path, raw?: true, body?, headers?, codes: readonly string[]}`.
- `type ApiResult<T, E extends string>`, in whatever envelope shape your app uses.
- `clientRequest<T, E extends string>(spec: EndpointSpec, decode: (v: unknown) => T, opts?: {signal?: AbortSignal}): Promise<ApiResult<T, E>>`.

`clientRequest` sends the request, decodes a 2xx body with `decode` (a `Blob` for a raw endpoint, `undefined` for no body), and maps an error body's code to `E` when `spec.codes` lists it. A module importing only the types is enough when the table has no `KindJSON` or `KindRaw` endpoint.

## The OpenAPI document

`WithOpenAPIFiles` writes the table as an OpenAPI 3.1 document, in the JSON Schema 2020-12 dialect:

- each endpoint is an operation, `operationId` its name, with its `Summary`, `Description` and `Extensions`.
- its path, query and header parameters, and its request body as a reference to the request form of its type.
- its success response at its status: a JSON schema, a raw body's media type, `text/event-stream` with `x-events` naming each event's data schema, or a redirect's `Location` header.
- its errors grouped by status, each with the `ErrorEnvelope` schema and `x-error-codes` mapping each code to its extensions.
- `security` from `Endpoint.Security`, `[]` for a public endpoint, and `OpenAPI.SecuritySchemes` as `components.securitySchemes`.

[Type mapping](type-mapping.md#null-and-presence) covers how each type becomes a component.
