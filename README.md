# wiregen

[![Go Reference](https://pkg.go.dev/badge/github.com/cplieger/wiregen/v2.svg)](https://pkg.go.dev/github.com/cplieger/wiregen/v2)
[![Go version](https://img.shields.io/github/go-mod/go-version/cplieger/wiregen)](https://github.com/cplieger/wiregen/blob/main/go.mod)
[![Test coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/cplieger/wiregen/badges/coverage.json)](https://github.com/cplieger/wiregen/actions/workflows/coverage.yml)
[![Mutation](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/cplieger/wiregen/badges/mutation.json)](https://github.com/cplieger/wiregen/issues?q=label%3Agremlins-tracker)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/13226/badge)](https://www.bestpractices.dev/projects/13226)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/cplieger/wiregen/badge)](https://scorecard.dev/viewer/?uri=github.com/cplieger/wiregen)

Generate TypeScript interfaces, decoders, and an SSE registry from Go types via AST analysis.

wiregen is a standalone Go library that takes a set of registered Go types and enum definitions and emits fully-typed TypeScript: interface declarations, runtime decoder functions with validation, and an SSE event→decoder registry. It analyzes your Go source with `go/packages` + `go/types` + `go/ast`, so it carries **doc comments through to JSDoc** on the generated interfaces. Its only build-time dependency is `golang.org/x/tools`; nothing it produces is a runtime dependency of your app.

## Install

```
go get github.com/cplieger/wiregen/v2@latest
```

Upgrading from v1: the module path is now `…/wiregen/v2`. Every per-file string generator returns `(string, error)` instead of panicking on config errors. Non-`omitempty` map fields are now required in the emitted types (matching `encoding/json`), nested collection elements are validated recursively, and the validators module is library-owned generated output (see `WithValidatorsFile`).

## Usage

Create a registry with `NewRegistry` (functional options configure behavior knobs), then set payload data via the exported fields:

```go
package main

import "github.com/cplieger/wiregen/v2"

type Status string

type User struct {
	// ID is the user's unique identifier.
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status Status `json:"status"`
}

func main() {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./validators.js"),
		wiregen.WithBusImport("./bus.js"),
	)

	// PackagePaths is optional; derived from the registered types when omitted.
	// Types are registered by identity via TypeRef (no reflect.Type needed).
	r.Types = []wiregen.WireType{wiregen.TypeRef[User]()}
	// Enum Values are optional; auto-discovered from the type's const block.
	r.Enums = map[string]wiregen.EnumDef{"Status": {}}
	r.SSEEvents = []wiregen.SSERegEntry{
		{EventType: "user", TypeName: "User"},
	}

	if err := r.Generate("./wire"); err != nil {
		panic(err)
	}
}
```

The `ID` doc comment above becomes a `/** ID is the user's unique identifier. */` JSDoc line on the generated `User` interface.

## API

### NewRegistry

```go
func NewRegistry(opts ...Option) *Registry
```

Creates a `*Registry` with behavior configured via functional options. Payload data (types, enums, constants, mappings) is then assigned to the returned registry's exported fields.

### Functional options

| Option | Description |
| --- | --- |
| `WithValidatorsImport(v string)` | **Required.** Import path for the validators module. |
| `WithBusImport(v string)` | **Required** (unless `WithSelfContainedRegistry(true)`). Import path for the bus module. |
| `WithTransportImport(v string)` | **Required with `Endpoints`.** Transport-module import path for the client. |
| `WithTypesImportPath(v string)` | Import path for the types file used in decoders (default: `"./types.gen.js"`). |
| `WithHeaderComment(v string)` | Header comment prepended to every generated file. |
| `WithRegisterFuncName(v string)` | Function name imported from the bus module (default: `"registerSSEDecoder"`). |
| `WithRegistryFuncName(v string)` | Exported function name in the registry file (default: `"registerAllSSEDecoders"`). |
| `WithSelfContainedRegistry(v bool)` | Use a self-contained Map-based registry instead of importing from BusImport. |
| `WithFilenames(types, decoders, registry, constants string)` | Override output filenames (pass `""` to keep defaults). |
| `WithClientFilename(v string)` | Override the generated client filename (default: `"client.gen.ts"`). |
| `WithValidatorsFile(v string)` | Write the library-owned validators module at this outDir-relative path on every run. |

### Registry fields (payload data)

Payload types are set via exported fields after construction:

| Field | Type | Description |
| --- | --- | --- |
| `PackagePaths` | `[]string` | Import paths the AST engine loads + parses. **Optional**; derived from the registered types' packages when omitted. Set it explicitly only to load extra packages. |
| `Types` | `[]WireType` | Go types to generate TS interfaces and decoders for. Register via `TypeRef[T]()`. |
| `Enums` | `map[string]EnumDef` | Named string enums (keyed by Go type name). `Values` is **optional**; auto-discovered from the type's `const` block (source order) when omitted. Explicit `Values` win. |
| `EnumTSName` | `map[string]string` | Override the TS name for an enum (Go name → TS name). |
| `TSNameOverride` | `map[string]string` | Override the TS interface name for a struct (Go name → TS name). |
| `PathNameOverride` | `map[string]string` | Override the decoder path segment for a type (keyed by TS name). |
| `TypeMappings` | `map[string]string` | Custom Go type → TS type overrides, keyed by full `importpath.Type` (e.g. `"…/uuid.UUID"` → `"string"`). |
| `DecoderMappings` | `map[string]string` | Custom Go type → decoder helper name (full `importpath.Type` key). When set, the decoder emits a validation call instead of a bare cast. |
| `DiscriminatorMap` | `map[string]map[string]string` | Per-union discriminator→variant decoder mapping; emit a union decoder for a sealed-interface union (see below). |
| `SSEEvents` | `[]SSERegEntry` | Maps SSE event type strings to registered struct names. |
| `Constants` | `[]WireConst` | Integer constants to emit into a constants file. |
| `Endpoints` | `[]Endpoint` | HTTP endpoint table; when non-empty, `Generate` also emits a typed client (`client.gen.ts`) and enables `GenerateGoPaths`. See "Endpoint table + generated client". |

Discriminated unions are declared in Go **source** with a directive on the sealed interface, `//wiregen:union discriminator=type variants=A,B,C`, which emits `export type X = A | B | C`. When `DiscriminatorMap[X]` is set, two runtime decoders are emitted: the 2-argument `decodeX(disc: string, v: unknown): X` (for callers that already extracted the discriminator, e.g. from an SSE event name) and the 1-argument payload adapter `decodeXPayload: Decoder<X>` (reads the discriminator key off the payload object itself). A union type can be registered in `SSEEvents`: the registry binds its payload adapter. Registering a union SSE event **without** a `DiscriminatorMap` entry fails `Generate` (there would be no runtime decoder to bind).

### Methods

One error model across the whole surface: every generator returns an error on a config problem; nothing exported panics.

- `(*Registry).Generate(outDir string) error`: writes all generated files to `outDir`. Each file is written atomically, and a staging failure (e.g. disk full) leaves the directory untouched; the pass is not a multi-file transaction, so a failure partway through can leave a mix of old and new files. `client.gen.ts` is written only when `Endpoints` is non-empty; the validators module only when `WithValidatorsFile` is set. Returns an error and writes nothing when: a required import is missing (`ValidatorsImport` empty; `BusImport` empty while SSE events are registered and `SelfContainedRegistry` is false; `TransportImport` empty while endpoints are registered); a bare type name is registered twice (the engine keys types by bare name, so two same-named types from different packages are rejected); two enums resolve to the same TS type name or const-array name; a registered `WireConst`'s `TSName` sanitizes to an empty TS identifier; the endpoint table is invalid (see "Endpoint table + generated client" below); or a `//wiregen:union` type is registered in `SSEEvents` without a `DiscriminatorMap` entry.
- `(*Registry).GenerateTypes() (string, error)`: types file content.
- `(*Registry).GenerateDecoders() (string, error)`: decoders file content. Errors if `ValidatorsImport` is empty.
- `(*Registry).GenerateRegistry() (string, error)`: registry file content. Errors if `BusImport` is empty while `SelfContainedRegistry` is false, or if `SelfContainedRegistry` is true while `ValidatorsImport` is empty.
- `(*Registry).GenerateConstants() (string, error)`: constants file content. Errors on an unsanitizable `TSName`.
- `(*Registry).GenerateClient() (string, error)`: typed-client file content. Errors if `TransportImport` or `ValidatorsImport` is empty or the endpoint table is invalid.
- `(*Registry).GenerateGoPaths(pkgName string) (string, error)`: a gofmt-formatted Go file of `Path*` constants (one per endpoint). Errors on an invalid endpoint table or package name.
- `(*Registry).GenerateValidators() string`: the library-owned validators module (the full function contract is listed under "Validators contract" below), under the same DO-NOT-EDIT banner as every other generated file. Content is constant (registry-independent), so this method alone cannot fail. Prefer `WithValidatorsFile` so `Generate` keeps the file current on every run.

### Types

```go
// WireType identifies a registered Go type by package path + name.
type WireType struct {
    PkgPath string
    Name    string
}

// TypeRef registers a type by identity (the only use of reflect, for the
// {PkgPath, Name} pair; the field walk is done from source via the AST).
func TypeRef[T any]() WireType

type WireConst struct {
    TSName string
    Value  int
}

type EnumDef struct{ Values []string }

type UnionDef struct {
    Discriminator string
    Variants      []string
}

type SSERegEntry struct {
    EventType string
    TypeName  string
}
```

## Endpoint table + generated client

Registering `Endpoints` puts the HTTP contract in the same registry as the
types. `Generate` then also emits `client.gen.ts`: one `PATH_*` constant per
endpoint (placeholders kept verbatim; non-JSON flows are consumed exclusively
through these) and, per `KindJSON` endpoint, a typed function pair,
`name(...): Promise<T | null>` and `nameRaw(...): Promise<ApiResult<T>>`,
with the response decoder bound when a `Response` type is registered (an
endpoint without one gets an OK-flag `Promise<boolean>` flavor instead).

```go
type Endpoint struct {
    Name      string       // TS function name + PATH_/Go constant base
    Method    string       // GET, POST, PUT, PATCH, DELETE
    Path      string       // "/api/scan/series/{id}"; {name} segments become typed args
    AuthGroup string       // opaque consumer tag for a routes-consistency check
    Kind      EndpointKind // "" = KindJSON; KindRaw / KindSSE emit only a PATH_ constant
    RespShape RespShape    // "" = RespObject; RespArray / RespRecord / RespStringArray
    Doc       string       // optional JSDoc line
    Request   WireType     // typed JSON request body (registered type)
    Response  WireType     // decoded 2xx response body (registered type)
    HasBody   bool         // untyped JSON body (body: unknown)
    Query     bool         // trailing query?: Record<string, QueryValue> argument
}
```

Validation happens before any file is written: unknown methods/kinds/shapes,
duplicate names, names that collide after case conversion (`configYaml` vs
`configYAML` would emit the same `PATH_CONFIG_YAML` / `PathConfigYAML`
constant), malformed `{placeholder}` syntax, and request/response types that
are not registered all fail `Generate` with a named error.

`AuthGroup` is never interpreted by wiregen. It exists so the consumer can
write a consistency test comparing the table against its server's actual route
registrations (the server stays authoritative for permissions).

**Client-transport contract.** The module at `TransportImport` must export:

- `clientRequest<T>(method, path, body, decoder, signal?): Promise<T | null>`
- `clientRequestOK(method, path, body?, signal?): Promise<boolean>`
- `clientRequestRaw<T>(method, path, body?, decoder?, signal?): Promise<ApiResult<T>>`
- `interface ApiResult<T>` (whatever envelope shape the consumer uses)

**Go path constants.** `GenerateGoPaths` (see "Methods" above) returns a Go
source file declaring one `Path*` string constant per endpoint, so a CLI in
the same binary shares the exact path table the TS client was generated from.

## Validators contract

The validators module (at `ValidatorsImport`) is **library-owned generated
output**: set `WithValidatorsFile` and `Generate` writes it on every run, or
scaffold it once with `GenerateValidators()`; either way, don't hand-edit it.
The contract below is what the generated decoders import by name; it is also a
stable, hand-written-decoder-friendly API (consumer code may import and build
on these helpers freely). The module exports:

- `asObject(v: unknown, path: string): Record<string, unknown>`
- `asArray(v: unknown, path: string): unknown[]`
- `reqStr(o: Record<string, unknown>, key: string, path: string): string`
- `reqNum(o: Record<string, unknown>, key: string, path: string): number`
- `reqBool(o: Record<string, unknown>, key: string, path: string): boolean`
- `optStr(o: Record<string, unknown>, key: string, path: string): string | undefined`
- `optNum(o: Record<string, unknown>, key: string, path: string): number | undefined`
- `optBool(o: Record<string, unknown>, key: string, path: string): boolean | undefined`
- `reqOneOf<T extends string>(o: Record<string, unknown>, key: string, values: readonly T[], path: string): T`
- `decodeArray<T>(v: unknown, decoder: Decoder<T>, path: string): T[]`
- `decodeRecord<T>(v: unknown, decoder: Decoder<T>, path: string): Record<string, T>`
- `type Decoder<T> = (v: unknown) => T`

## Behavior notes

- **Doc comments** on registered structs and their fields are carried through to `/** … */` JSDoc on the generated interfaces (the AST engine reads them from source).
- **Unexported fields** are skipped (matching `encoding/json` behavior).
- **`time.Time`** maps to `string`; **`json.RawMessage`** and `interface{}` map to `unknown`.
- **`json.Number`** maps to `number`.
- **`[]byte`** maps to `string` (JSON encodes `[]byte` as base64).
- **`omitzero`** (Go 1.24+) is treated the same as `omitempty`: the field becomes optional.
- **Map fields keep their source optionality** (pointer / `omitempty` / `omitzero` → optional, otherwise required), exactly like every other field kind. A required map's JSON `null` decodes to `{}` (below).
- **Nested collection elements are validated recursively.** `[][]T`, `map[string][]T`, and deeper compositions decode with real per-level checks: each level accepts `null` as its empty value and validates its own elements; a malformed inner array/map throws with the element path.
- **JSON `null` decodes as the zero value, not an error.** encoding/json marshals a nil pointer/slice/map (and a nil `[]byte`) to `null` when the field lacks `omitempty`; the generated decoders accept that output. An optional field decodes present-`null` as `undefined`, a required slice/map decodes `null` as empty (`[]`/`{}`), and a required `[]byte` decodes `null` as `""`. `json.RawMessage` and `interface{}` fields pass `null` through as data. There is no null-vs-absent distinction (nullable-vs-optional is a non-goal, below).
- **`json:",string"`** causes the field to be typed as `string` and decoded with `reqStr`/`optStr`, matching `encoding/json`'s string-wrapping behavior for numbers and booleans.
- **Map keys** are always `string` in generated TS because JSON object keys are strings regardless of the Go map key type.
- **Embedded named structs** are flattened into the embedding interface, and field promotion matches `encoding/json`'s rules: the shallowest field wins, a tagged field dominates an untagged one at equal depth, and a field reachable through two sibling embeds at equal depth (a "diamond") is dropped as an ambiguous promotion.
- **Generated identifiers are always valid TypeScript.** Consumer- or source-derived strings that land in an identifier position (struct/enum name overrides, the registry function-name knobs, a `//wiregen:union` discriminator, field wire names, and decoder local variables) are sanitized to a valid TS identifier, with a safe fallback when a value sanitizes to empty. A JSON key that isn't a valid identifier (e.g. `content-type`) is emitted as a quoted property and bracket access (`out["content-type"]`). Values that are already valid identifiers are emitted unchanged, so output stays byte-identical for the common case.
- **A zero-value enum** (no discoverable `const` values and no explicit `Values`) emits `export type X = never;` rather than the invalid `export type X = ;`.
- **`Generate`** writes `types.gen.ts` + `decoders.gen.ts` always; `registry.gen.ts` only when `SSEEvents` is non-empty; `constants.gen.ts` only when `Constants` is non-empty.

## Unsupported by Design

The following are intentionally not supported:

| Feature | Reason |
| --- | --- |
| **Go generics (type parameters)** | The Go type system can't represent uninstantiated generic types here. Register concrete instantiations instead. |
| **Nullable vs optional distinction** | `T \| null` vs `?:`; current consumers treat null and absent identically. Pointer/omitempty → optional only. |
| **`tstype` struct tag hints** | `TypeMappings` provides the same escape hatch at the registry level. |
| **Inline anonymous struct fields** | A field whose type is an inline `struct { … }` literal maps to `unknown`. Register it as a named type instead. (Embedded _named_ structs are flattened, not unknown.) |

## Contributing

Issues and PRs are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the
conventions and how to run the checks locally.

## Disclaimer

This project is built with care and follows security best practices, but it is intended for personal / self-hosted use. No guarantees of fitness for production environments. Use at your own risk.

This project was built with AI-assisted tooling using [Claude](https://claude.com), [GPT](https://openai.com), and [Kiro](https://kiro.dev). The human maintainer defines architecture, supervises implementation, and makes all final decisions.

## License

GPL-3.0. See [LICENSE](LICENSE).
