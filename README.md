# wiregen

[![Go Reference](https://pkg.go.dev/badge/github.com/cplieger/wiregen/v4.svg)](https://pkg.go.dev/github.com/cplieger/wiregen/v4) [![Go version](https://img.shields.io/github/go-mod/go-version/cplieger/wiregen)](https://github.com/cplieger/wiregen/blob/main/go.mod) [![Mutation](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/cplieger/wiregen/badges/mutation.json)](https://github.com/cplieger/wiregen/issues?q=label%3Agremlins-tracker)

wiregen generates TypeScript types, validating JSON decoders, a typed HTTP client and an OpenAPI 3.1 document from your Go structs and endpoint table, so your TypeScript front end checks every payload your Go server sends.

It replaces hand-written TypeScript interfaces, type guards and API descriptions. It runs at build time from a short Go program and depends on `golang.org/x/tools` and `github.com/cplieger/atomicfile/v4`. The generated TypeScript needs no npm package except fast-check, for the optional tests. It needs Go 1.27 or later on Linux or macOS and is licensed under Apache-2.0.

## Why use it

It suits a Go server that sends JSON or Server-Sent Events to a TypeScript client, where both sides must agree on every field.

- Each decoder checks every field and throws a `TypeError` naming the JSON path, such as `$.user.id: expected number, got string`.
- Optional fields, `null` values and field names follow `encoding/json`, including `omitempty`, `omitzero`, embedded structs and `[]byte` as base64.
- A field whose JSON shape cannot be checked, such as an `any` or a `time.Duration`, fails generation instead of becoming `unknown`.
- Go doc comments become JSDoc, enum members come from the `const` block, and bound methods become schema and decoder checks.
- Client calls return per-endpoint error unions, and it can also write a decoder per Server-Sent Event and a fast-check arbitrary per type.
- A configuration error stops it before any file is written, and its output is byte-identical between runs.

Consider [tygo](https://github.com/gzuidhof/tygo) if you want TypeScript types alone from a `tygo.yaml` config. It keeps comments and supports Go generic types.

## Install

```sh
go get github.com/cplieger/wiregen/v4@latest
```

## Usage

You call wiregen from a short generator program that registers your types, then run that program with `go run` or `go generate`. wiregen ships no command of its own. Your types must live in an importable package, because wiregen reads them from source and cannot load `package main`.

```go
// Package wire holds the JSON types the server sends.
package wire

// Status is a user's account state.
type Status string

// User status values.
const (
	StatusActive Status = "active"
	StatusBanned Status = "banned"
)

// User is one account.
type User struct {
	// ID is the user's unique identifier.
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status Status `json:"status"`
}
```

```go
// cmd/wire-codegen/main.go
package main

import (
	"context"
	"log"

	"github.com/cplieger/wiregen/v4"

	"example.com/app/wire"
)

func main() {
	r := wiregen.Registry{
		Enums:  []wiregen.EnumRef{wiregen.StringEnum[wire.Status]()},
		Events: []wiregen.Event{{Name: "user", Data: wiregen.TypeRef[wire.User]()}},
	}
	if err := r.Generate(context.Background(), wiregen.WithTSDir("web/wire")); err != nil {
		log.Fatal(err)
	}
}
```

Running `go run ./cmd/wire-codegen` writes four files to `web/wire`: `types.gen.ts`, `decoders.gen.ts`, `events.gen.ts` and `validators.gen.ts`, the module the decoders import their checks from. The `ID` comment becomes JSDoc on the `User` interface, and the decoder reads:

```ts
const STATUSS = ["active", "banned"] as const;

export const decodeUser: Decoder<User> = (v) => {
  const o = asObject(v, "$.user");
  const out: User = {
    id: reqNum(o, "id", "$.user"),
    name: reqStr(o, "name", "$.user"),
    status: reqOneOf(o, "status", STATUSS, "$.user"),
  };
  return out;
};
```

`STATUSS` is the generated list of `Status` values that `reqOneOf` accepts. Add `//go:generate go run ./cmd/wire-codegen` to a Go file to regenerate with `go generate ./...`. [Generated files](docs/generated-files.md) covers the events module, and [Typed HTTP client](docs/http-client.md) covers the endpoint table, the client and the OpenAPI document.

## API

- A `Registry` is plain data: `Types`, `Enums`, `Unions`, `Mappings`, `Endpoints`, `Events`, `ErrorEnvelope`, `OpenAPI`, `Constants`, and name overrides. Register each struct with `TypeRef[T]()`. A type that an endpoint, event or union names is generated without being listed.
- Register an enum by its type: `StringEnum[T]()` for a string type, `TextEnum[T]()` for a `uint8` type whose `MarshalText` names its members.
- `Registry.Generate(ctx, opts ...Option)` writes every file under `WithTSDir(dir)`, which is required. `WithOpenAPIFiles(paths...)` adds the OpenAPI document, `WithRequireJSONTags()` refuses an untagged field, and the other `With*` options set import paths, file names and the optional modules.

`Generate` returns an error for a configuration problem, and nothing exported panics. [Options and fields](docs/configuration.md) lists each option with its default, and the full reference is on [pkg.go.dev](https://pkg.go.dev/github.com/cplieger/wiregen/v4).

## The endpoint table is authoritative

Bind your server's routes, generate the client and write the OpenAPI document from one `[]wiregen.Endpoint`, so no second route list can drift from it. Each endpoint gets an arguments interface, an error union from its declared codes, a descriptor, and a function over one transport you supply, `clientRequest(spec, decode, opts)`, returning `ApiResult<T, E>`. Declared request headers, such as `Idempotency-Key`, become typed arguments. [Typed HTTP client](docs/http-client.md) has the table, the client and the transport contract.

## Decoders follow encoding/json

wiregen reads your types from source with `go/packages` and applies the field rules of `encoding/json`, so the TypeScript matches what your server puts on the wire.

- Unexported fields are skipped. A field `encoding/json` can leave out, or a pointer field, such as an `omitempty` or `omitzero` field or one promoted through an embedded pointer, becomes optional, `key?: T`. An untagged field takes its Go name.
- `time.Time` becomes `string`, `json.Number` becomes `number`, `[]byte` becomes a base64 `string`, and a type implementing `encoding.TextMarshaler` becomes `string`.
- `json.RawMessage` becomes `unknown`, and is the one opaque type allowed.
- A JSON `null` decodes to the field's empty value instead of an error: `undefined` for an optional field, `[]` or `{}` for a required slice or map, and `""` for a required `[]byte`.
- A field the Go type does not declare is ignored, as `encoding/json` ignores it, and only the object's own properties are read, so a key such as `__proto__` or `constructor` is plain data.

## Strict mode

`Generate` refuses, listing every field path at once, a field whose shape the client could not check: an interface or `any`, an inline anonymous struct, an unregistered struct, a `json.Marshaler` or a registered struct with a marshaler, a fixed-size array, a `,string` option `encoding/json` ignores or that rewrites a mapped type, and a `time.Duration`. In a request body, parameters or query it also refuses a field `encoding/json` cannot decode into, such as one promoted through an embedded pointer to an unexported struct, or a `TextMarshaler` without `UnmarshalText`. Durations cross as `int64` milliseconds in a field named with an `_ms` suffix, because `encoding/json` writes a `time.Duration` as integer nanoseconds. `Mappings` gives a type wiregen cannot read your own TypeScript type, decoder and schema. [Type mapping](docs/type-mapping.md) has every rule, the enums, the bounds, the unions and the nullability the OpenAPI document states per direction.

## Unsupported by design

- Go generic types. Register each concrete instantiation instead.
- `T | null` on a field. A field that can be absent or `null` is optional, `key?: T`, and the OpenAPI document carries the exact nullability.
- `tstype` struct tags. `Mappings` does the same job for the whole registry.
- Windows. wiregen runs on Linux and macOS, because it writes every file atomically through atomicfile/v4, which does not support Windows.

## Documentation

- [Options and fields](docs/configuration.md) lists every option, registry field and helper type, with defaults.
- [Type mapping](docs/type-mapping.md) shows how each Go type becomes TypeScript and a schema, with strict mode, enums, bounds, unions and mappings.
- [Generated files](docs/generated-files.md) covers which files `Generate` writes, its errors, and the validators and events modules.
- [Typed HTTP client](docs/http-client.md) covers the endpoint table, the generated client, the transport module and the OpenAPI document.
- [Property-test arbitraries](docs/arbitraries.md) describes the fast-check module and how its values are drawn.

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Disclaimer

This project is built with care and follows security best practices, but it is intended for personal / self-hosted use. No guarantees of fitness for production environments. Use at your own risk.

This project was built with AI-assisted tooling using [Claude](https://claude.com), [GPT](https://openai.com), and [Kiro](https://kiro.dev). The human maintainer defines architecture, supervises implementation, and makes all final decisions.

## License

Apache-2.0. See [LICENSE](LICENSE).
