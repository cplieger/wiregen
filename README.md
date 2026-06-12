# wiregen

[![CI](https://github.com/cplieger/wiregen/actions/workflows/ci.yaml/badge.svg)](https://github.com/cplieger/wiregen/actions/workflows/ci.yaml)
[![Go Reference](https://pkg.go.dev/badge/github.com/cplieger/wiregen.svg)](https://pkg.go.dev/github.com/cplieger/wiregen)
[![Go Report Card](https://goreportcard.com/badge/github.com/cplieger/wiregen)](https://goreportcard.com/report/github.com/cplieger/wiregen)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/cplieger/wiregen/badge)](https://scorecard.dev/viewer/?uri=github.com/cplieger/wiregen)
[![License: GPL-3.0](https://img.shields.io/badge/License-GPL--3.0-blue.svg)](LICENSE)

# wiregen

Generate TypeScript interfaces, decoders, and an SSE registry from Go types via AST analysis.

wiregen is a standalone Go library that, given a set of registered Go types and enum definitions, emits fully-typed TypeScript: interface declarations, runtime decoder functions with validation, and an SSE event→decoder registry. It analyzes your Go source with `go/packages` + `go/types` + `go/ast`, so it carries **doc comments through to JSDoc** on the generated interfaces. Its only build-time dependency is `golang.org/x/tools`; nothing it produces is a runtime dependency of your app.

## Install

```
go get github.com/cplieger/wiregen@latest
```

## Usage

Create a registry with `NewRegistry` (functional options configure behavior knobs), then set payload data via the exported fields:

```go
package main

import "github.com/cplieger/wiregen"

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

	// PackagePaths is optional — derived from the registered types when omitted.
	// Types are registered by identity via TypeRef (no reflect.Type needed).
	r.Types = []wiregen.WireType{wiregen.TypeRef[User]()}
	// Enum Values are optional — auto-discovered from the type's const block.
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
|--------|-------------|
| `WithValidatorsImport(v string)` | **Required.** Import path for the validators module. |
| `WithBusImport(v string)` | **Required** (unless `WithSelfContainedRegistry(true)`). Import path for the bus module. |
| `WithTypesImportPath(v string)` | Import path for the types file used in decoders (default: `"./types.gen.js"`). |
| `WithHeaderComment(v string)` | Header comment prepended to every generated file. |
| `WithRegisterFuncName(v string)` | Function name imported from the bus module (default: `"registerSSEDecoder"`). |
| `WithRegistryFuncName(v string)` | Exported function name in the registry file (default: `"registerAllSSEDecoders"`). |
| `WithSelfContainedRegistry(v bool)` | Use a self-contained Map-based registry instead of importing from BusImport. |
| `WithFilenames(types, decoders, registry, constants string)` | Override output filenames (pass `""` to keep defaults). |

### Registry fields (payload data)

Payload types are set via exported fields after construction:

| Field | Type | Description |
|-------|------|-------------|
| `PackagePaths` | `[]string` | Import paths the AST engine loads + parses. **Optional** — derived from the registered types' packages when omitted. |
| `Types` | `[]WireType` | Go types to generate TS interfaces and decoders for. Register via `TypeRef[T]()`. |
| `Enums` | `map[string]EnumDef` | Named string enums (keyed by Go type name). `Values` is **optional** — auto-discovered from the type's `const` block (source order) when omitted; explicit `Values` win. |
| `EnumTSName` | `map[string]string` | Override the TS name for an enum (Go name → TS name). |
| `TSNameOverride` | `map[string]string` | Override the TS interface name for a struct (Go name → TS name). |
| `PathNameOverride` | `map[string]string` | Override the decoder path segment for a type (keyed by TS name). |
| `TypeMappings` | `map[string]string` | Custom Go type → TS type overrides, keyed by full `importpath.Type` (e.g. `"…/uuid.UUID"` → `"string"`). |
| `DecoderMappings` | `map[string]string` | Custom Go type → decoder helper name (full `importpath.Type` key). When set, the decoder emits a validation call instead of a bare cast. |
| `DiscriminatorMap` | `map[string]map[string]string` | Per-union discriminator→variant decoder mapping; emit a union decoder for a sealed-interface union (see below). |
| `SSEEvents` | `[]SSERegEntry` | Maps SSE event type strings to registered struct names. |
| `Constants` | `[]WireConst` | Integer constants to emit into a constants file. |

Discriminated unions are declared in Go **source** with a directive on the sealed interface — `//wiregen:union discriminator=type variants=A,B,C` — which emits `export type X = A | B | C`. A runtime union decoder `(disc: string, v: unknown) => X` is emitted only when `DiscriminatorMap[X]` is set.

### Methods

- `(*Registry).Generate(outDir string) error` — writes all generated files to `outDir`.
- `(*Registry).GenerateTypes() string` — returns types file content.
- `(*Registry).GenerateDecoders() string` — returns decoders file content. Panics if `ValidatorsImport` is empty.
- `(*Registry).GenerateRegistry() string` — returns registry file content. Panics if `BusImport` is empty and `SelfContainedRegistry` is false.
- `(*Registry).GenerateConstants() string` — returns constants file content.

### Types

```go
// WireType identifies a registered Go type by package path + name.
type WireType struct {
    PkgPath string
    Name    string
}

// TypeRef registers a type by identity (the only use of reflect — for the
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

## Validators contract

The consumer's validators module (at `ValidatorsImport`) must export:

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
- **`[]byte`** maps to `string` (JSON encodes `[]byte` as base64).
- **`omitzero`** (Go 1.24+) is treated the same as `omitempty` — the field becomes optional.
- **`json:",string"`** causes the field to be typed as `string` and decoded with `reqStr`/`optStr`, matching `encoding/json`'s string-wrapping behavior for numbers and booleans.
- **Map keys** are always `string` in generated TS because JSON object keys are strings regardless of the Go map key type.
- **Embedded structs** are flattened into the embedding interface (matching `encoding/json`).
- **`Generate`** writes `types.gen.ts` + `decoders.gen.ts` always; `registry.gen.ts` only when there are SSE events; `constants.gen.ts` only when there are constants.
- **`PackagePaths`** defaults to the distinct packages of the registered types; set it explicitly only to load extra packages.
- **Enum `Values`** are auto-discovered from the named type's `const` declarations (in source order) when left empty; provide them explicitly to override the set or order.

## Unsupported by design

The following are intentionally not supported:

| Feature | Reason |
|---------|--------|
| **Go generics (type parameters)** | The Go type system can't represent uninstantiated generic types here. Register concrete instantiations instead. |
| **Nullable vs optional distinction** | `T \| null` vs `?:` — current consumers treat null and absent identically. Pointer/omitempty → optional only. |
| **`tstype` struct tag hints** | `TypeMappings` provides the same escape hatch at the registry level. |
| **Inline anonymous struct fields** | A field whose type is an inline `struct { … }` literal maps to `unknown`. Register it as a named type instead. (Embedded _named_ structs are flattened, not unknown.) |

## License

GPL-3.0 — see [LICENSE](LICENSE).
