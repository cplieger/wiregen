# Typed HTTP client

This page is for a developer whose front end calls a Go JSON API. It shows how to put your HTTP endpoints in the registry, so wiregen writes a typed fetch client and a matching set of Go path constants.

## The endpoint table

Registering `Endpoints` puts the HTTP contract in the same registry as the types. `Generate` then also writes `client.gen.ts`, and `GenerateGoPaths` becomes available.

```go
type Endpoint struct {
    Name      string       // TS function name, and the base of the PATH_ and Go constant names
    Method    string       // GET, POST, PUT, PATCH or DELETE
    Path      string       // "/api/scan/series/{id}"; each {name} segment becomes a typed argument
    AuthGroup string       // your own tag for a routes-consistency test
    Kind      EndpointKind // "" means KindJSON; KindRaw and KindSSE emit only a PATH_ constant
    RespShape RespShape    // "" means RespObject; or RespArray, RespRecord, RespStringArray
    Doc       string       // optional JSDoc line
    Request   WireType     // typed JSON request body, a registered type
    Response  WireType     // decoded 2xx response body, a registered type
    HasBody   bool         // untyped JSON body, typed body: unknown
    Query     bool         // adds a trailing query?: Record<string, QueryValue> argument
}
```

## The generated client

`client.gen.ts` holds one `PATH_<NAME>` constant per endpoint, with placeholders kept verbatim, so code that needs the raw path, such as an `EventSource` or a form action, uses the same table. `KindRaw` and `KindSSE` endpoints get only that constant.

Each `KindJSON` endpoint also gets a pair of functions:

- With a `Response` type, `name(...): Promise<R | null>` and `nameRaw(...): Promise<ApiResult<R>>`, with the decoder bound. `R` is the type for `RespObject`, `T[]` for `RespArray` and `Record<string, T>` for `RespRecord`.
- A `RespStringArray` endpoint sets no `Response` and returns `Promise<string[] | null>` and `Promise<ApiResult<string[]>>`.
- Any other endpoint without a `Response` gets `name(...): Promise<boolean>` and `nameRaw(...): Promise<ApiResult<unknown>>`.

Path placeholders become typed arguments, passed through `encodeURIComponent`. With `Query: true`, the query record is serialized with `URLSearchParams` and `undefined` values are skipped. Every function takes an optional last argument, `opts?: ClientOpts`, whose `signal` is passed to the transport.

## Validation

Endpoint validation runs before any file is written, and each failure is a named error from `Generate`. It rejects:

- a name that is empty, not a valid TypeScript identifier, or used twice
- two names that collide after case conversion, such as `configYaml` and `configYAML`, which would both emit `PATH_CONFIG_YAML` and `PathConfigYAML`
- an unknown method, kind or response shape
- a path that does not start with `/`, or a malformed `{placeholder}`
- a `RespArray` or `RespRecord` endpoint with no `Response`, a `RespStringArray` endpoint that sets one, or an endpoint that sets both `Request` and `HasBody`
- a request or response type that is not registered, or a `KindRaw` or `KindSSE` endpoint that sets one.

`AuthGroup` is never read by wiregen. It lets you write a test that compares the table with the routes your server registers, and the server stays the authority on permissions.

## Client-transport module

The module at `WithTransportImport` must export:

- `clientRequest<T>(method, path, body, decoder, signal?): Promise<T | null>`
- `clientRequestOK(method, path, body?, signal?): Promise<boolean>`
- `clientRequestRaw<T>(method, path, body?, decoder?, signal?): Promise<ApiResult<T>>`
- `interface ApiResult<T>`, in whatever envelope shape your app uses

## Go path constants

`GenerateGoPaths(pkgName)` returns a gofmt-formatted Go file with one `Path*` string constant per endpoint. A command-line tool in the same binary can then call the exact paths the TypeScript client was generated from.
