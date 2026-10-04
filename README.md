# wiregen

[![Go Reference](https://pkg.go.dev/badge/github.com/cplieger/wiregen/v3.svg)](https://pkg.go.dev/github.com/cplieger/wiregen/v3) [![Go version](https://img.shields.io/github/go-mod/go-version/cplieger/wiregen)](https://github.com/cplieger/wiregen/blob/main/go.mod) [![Mutation](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/cplieger/wiregen/badges/mutation.json)](https://github.com/cplieger/wiregen/issues?q=label%3Agremlins-tracker)

wiregen generates TypeScript types and validating JSON decoders from your Go structs, so your TypeScript front end checks every payload your Go server sends.

It replaces the TypeScript interfaces and type guards you would otherwise write by hand. It runs at build time from a short Go program you write, and its only direct dependency is `golang.org/x/tools`. The generated TypeScript needs no npm package except fast-check, for the optional test module. It needs Go 1.27 or later and is licensed under Apache-2.0.

## Why use it

wiregen is built for a Go server that sends JSON or Server-Sent Events to a TypeScript client, where both sides must agree on every field.

- Each decoder checks every field and throws a `TypeError` that names the JSON path, such as `$.user.id: expected number, got string`.
- Optional fields, `null` values and field names follow `encoding/json`, including `omitempty`, `omitzero`, embedded structs and `[]byte` as base64.
- Go doc comments become JSDoc, and an enum's values come from its `const` block.
- It can also write a registry from SSE event names to decoders, a typed fetch client from an endpoint table, and a fast-check arbitrary for each type.
- It reports a configuration error before it writes any file, and its output is byte-identical from run to run.

Consider [tygo](https://github.com/gzuidhof/tygo) if you want TypeScript types alone, from a `tygo.yaml` config and a `tygo generate` command. It keeps comments and supports Go generic types.

## Install

```sh
go get github.com/cplieger/wiregen/v3@latest
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

	"github.com/cplieger/wiregen/v3"

	"example.com/app/wire"
)

func main() {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./validators.js"),
		wiregen.WithValidatorsFile("validators.ts"),
		wiregen.WithSelfContainedRegistry(true),
	)
	r.Types = []wiregen.WireType{wiregen.TypeRef[wire.User]()}
	r.Enums = map[string]wiregen.EnumDef{"Status": {}}
	r.SSEEvents = []wiregen.SSERegEntry{{EventType: "user", TypeName: "User"}}

	if err := r.Generate(context.Background(), "web/wire"); err != nil {
		log.Fatal(err)
	}
}
```

`WithValidatorsFile` tells `Generate` where to write the validators module, and `WithValidatorsImport` is the path the decoders import it by.

Running `go run ./cmd/wire-codegen` writes four files to `web/wire`: `types.gen.ts`, `decoders.gen.ts`, `registry.gen.ts` and `validators.ts`. The `ID` comment becomes JSDoc on the `User` interface, and the decoder reads:

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

`STATUSS` is the generated list of `Status` values that `reqOneOf` accepts. Add `//go:generate go run ./cmd/wire-codegen` to a Go file to regenerate with `go generate ./...`. [Generated files](docs/generated-files.md) covers the SSE bus module you can supply instead of the self-contained registry, and [Typed HTTP client](docs/http-client.md) covers the fetch client.

## API

- `NewRegistry(opts ...Option)` builds a registry. The `With*` options set import paths, file names and the optional output modules.
- Exported fields on the registry hold what to generate: `Types`, `Enums`, `SSEEvents`, `Constants`, `Endpoints`, and name and type overrides. Register each type with `TypeRef[T]()`.
- `Generate(ctx, outDir)` writes every file. `GenerateTypes`, `GenerateDecoders`, `GenerateRegistry`, `GenerateConstants`, `GenerateClient`, `GenerateArbitraries`, `GenerateValidators` and `GenerateGoPaths` each return one file's content.
- A `//wiregen:union` comment on a sealed Go interface, one with an unexported method, declares a discriminated union.

Every generator that can fail returns an error for a configuration problem, and nothing exported panics. [Options and fields](docs/configuration.md) lists each option with its default, and the full reference is on [pkg.go.dev](https://pkg.go.dev/github.com/cplieger/wiregen/v3).

## Decoders follow encoding/json

wiregen reads your types from source with `go/packages` and applies the field rules of `encoding/json`, so the TypeScript matches what your server puts on the wire.

- Unexported fields are skipped. A pointer field, or a field tagged `omitempty` or `omitzero`, becomes optional, `key?: T`.
- `time.Time` becomes `string`, `json.Number` becomes `number` and `[]byte` becomes a base64 `string`. `json.RawMessage` and `interface{}` become `unknown`.
- A `json:",string"` field is typed `string`. Map keys are always `string`.
- Embedded structs are flattened with the promotion rules of `encoding/json`.
- A JSON `null` decodes to the field's empty value instead of an error. That is `undefined` for an optional field, `[]` or `{}` for a required slice or map, and `""` for a required `[]byte`.
- Nested slices and maps are checked at every level.
- A field the Go type does not declare is ignored and left out of the decoded value, as `encoding/json` ignores it.

`TypeMappings` and `DecoderMappings` map a type such as a UUID to your own TypeScript type and decoder. [Type mapping](docs/type-mapping.md) has every rule.

## Files your front end imports

The decoders import their checks from a validators module that wiregen writes when you set `WithValidatorsFile`, so you never write or edit it. Two modules are yours to supply, and only for the feature that needs them:

- For SSE events without `WithSelfContainedRegistry(true)`, a bus module at `WithBusImport` exports `registerSSEDecoder(eventType, decoder)`.
- For an endpoint table, a transport module at `WithTransportImport` exports `clientRequest`, `clientRequestOK`, `clientRequestRaw` and the `ApiResult<T>` type.

A configuration error writes nothing. Each file is replaced in one step, but the files are replaced one after another, so a failure partway through can leave some files updated and others not. [Generated files](docs/generated-files.md) lists every file, when it is written and each module's signatures.

## Unsupported by design

- Go generic types. Register each concrete instantiation instead.
- A difference between `null` and an absent field. Pointer and `omitempty` fields become optional, `key?: T`, never `T | null`.
- `tstype` struct tags. `TypeMappings` does the same job for the whole registry.
- Inline anonymous struct fields, which map to `unknown`. Give the struct a name and register it. Embedded named structs are flattened as usual.

[Type mapping](docs/type-mapping.md#unsupported-by-design) gives the reasons.

## Documentation

- [Options and fields](docs/configuration.md) lists every option, registry field and helper type, with defaults.
- [Type mapping](docs/type-mapping.md) shows how each Go type becomes TypeScript, plus unions and the non-goals.
- [Generated files](docs/generated-files.md) covers which files `Generate` writes, its errors, and the validators and bus modules.
- [Typed HTTP client](docs/http-client.md) covers the endpoint table, the generated client and the transport module.
- [Property-test arbitraries](docs/arbitraries.md) describes the fast-check module and how its values are drawn.

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the correctness rules and the golden-file workflow.

## Disclaimer

This project is built with care and follows security best practices, but it is intended for personal / self-hosted use. No guarantees of fitness for production environments. Use at your own risk.

This project was built with AI-assisted tooling using [Claude](https://claude.com), [GPT](https://openai.com), and [Kiro](https://kiro.dev). The human maintainer defines architecture, supervises implementation, and makes all final decisions.

## License

Apache-2.0. See [LICENSE](LICENSE).
