[![CI](https://github.com/cplieger/wiregen/actions/workflows/ci.yaml/badge.svg)](https://github.com/cplieger/wiregen/actions/workflows/ci.yaml)
[![Go Reference](https://pkg.go.dev/badge/github.com/cplieger/wiregen.svg)](https://pkg.go.dev/github.com/cplieger/wiregen)
[![License: GPL-3.0](https://img.shields.io/badge/License-GPL--3.0-blue.svg)](LICENSE)

# wiregen

Generate TypeScript interfaces, decoders, and an SSE registry from Go types via reflection.

wiregen is a standalone Go library that, given a set of registered `reflect.Type` values and enum definitions, emits fully-typed TypeScript: interface declarations, runtime decoder functions with validation, and an SSE event→decoder registry. It has zero dependencies beyond the Go standard library.

## Install

```
go get github.com/cplieger/wiregen@latest
```

## Usage

Create a registry with `NewRegistry` (functional options configure behavior knobs), then set payload data via the exported fields:

```go
package main

import (
	"reflect"

	"github.com/cplieger/wiregen"
)

type Status string

type User struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status Status `json:"status"`
}

func main() {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./validators.js"),
		wiregen.WithBusImport("./bus.js"),
	)

	// Payload types are set via exported fields on the Registry.
	r.WireTypes = []reflect.Type{reflect.TypeFor[User]()}
	r.Enums = map[string]wiregen.EnumDef{
		"Status": {Values: []string{"active", "inactive"}},
	}
	r.SSEEvents = []wiregen.SSERegEntry{
		{EventType: "user", TypeName: "User"},
	}

	if err := r.Generate("./wire"); err != nil {
		panic(err)
	}
}
```

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
| `WireTypes` | `[]reflect.Type` | Go struct types to generate TS interfaces and decoders for. |
| `Enums` | `map[string]EnumDef` | Named string enums with their valid values. |
| `EnumTSName` | `map[string]string` | Override the TS name for an enum (Go name → TS name). |
| `TSNameOverride` | `map[string]string` | Override the TS interface name for a struct. |
| `PathNameOverride` | `map[string]string` | Override the decoder path segment for a type. |
| `TypeMappings` | `map[reflect.Type]string` | Custom Go type → TS type overrides (e.g. `uuid.UUID` → `"string"`). |
| `DecoderMappings` | `map[reflect.Type]string` | Custom Go type → decoder helper name. When set, the decoder emits a validation call instead of a bare cast. |
| `SSEEvents` | `[]SSERegEntry` | Maps SSE event type strings to registered struct names. |
| `Constants` | `[]WireConst` | Integer constants to emit into a constants file. |

### Methods

- `(*Registry).Generate(outDir string) error` — writes all generated files to `outDir`.
- `(*Registry).GenerateTypes() string` — returns types file content.
- `(*Registry).GenerateDecoders() string` — returns decoders file content. Panics if `ValidatorsImport` is empty.
- `(*Registry).GenerateRegistry() string` — returns registry file content. Panics if `BusImport` is empty and `SelfContainedRegistry` is false.
- `(*Registry).GenerateConstants() string` — returns constants file content.

### Types

```go
type WireConst struct {
    TSName string
    Value  int
}

type EnumDef struct{ Values []string }

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

- **Unexported fields** are skipped (matching `encoding/json` behavior).
- **`[]byte`** maps to `string` (JSON encodes `[]byte` as base64).
- **`omitzero`** (Go 1.24+) is treated the same as `omitempty` — the field becomes optional.
- **`json:",string"`** causes the field to be typed as `string` and decoded with `reqStr`/`optStr`, matching `encoding/json`'s string-wrapping behavior for numbers and booleans.
- **Map keys** are always `string` in generated TS because JSON object keys are strings regardless of the Go map key type.

## Unsupported by design

The following features are intentionally not supported due to architectural constraints of the reflect-based approach:

| Feature | Reason |
|---------|--------|
| **Go generics (type parameters)** | `reflect` cannot represent uninstantiated generic types. Register concrete instantiations instead. |
| **Comment/doc passthrough** | `reflect` has no access to source comments. Would require AST parsing, contradicting the library's reflect-based design. |
| **Union/discriminated-union types** | Go's type system doesn't express unions at the struct-field level via reflect. `interface{}` → `unknown` is the correct mapping. |
| **Nullable vs optional distinction** | `T \| null` vs `?:` — current consumers treat null and absent identically. Pointer/omitempty → optional only. |
| **`tstype` struct tag hints** | `TypeMappings` provides the same escape hatch at the registry level. |
| **Nested anonymous struct types** | Register inline structs as named `WireTypes` instead. |

## License

GPL-3.0 — see [LICENSE](LICENSE).
