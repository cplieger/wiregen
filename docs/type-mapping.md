# Type mapping

This page states how wiregen turns each Go type into TypeScript and a decoder. It is for a developer checking that the generated code matches what their server sends. wiregen reads the registered packages from source with `go/packages`, `go/types` and `go/ast`, and its field walk follows `encoding/json`.

## Field rules

- Doc comments on registered structs and their fields become `/** ... */` JSDoc on the generated interfaces.
- Unexported fields are skipped, as `encoding/json` skips them.
- `time.Time` maps to `string`. `json.RawMessage` and `interface{}` map to `unknown`. `json.Number` maps to `number`.
- `[]byte` maps to `string`, because JSON encodes `[]byte` as base64.
- `omitzero` is treated the same as `omitempty`, and the field becomes optional.
- A map field keeps the optionality of its source. A pointer, `omitempty` or `omitzero` map is optional, and any other map is required, like every other field kind. A required map's JSON `null` decodes to `{}`.
- Nested collection elements are checked at every level. `[][]T`, `map[string][]T` and deeper compositions accept `null` as each level's empty value, check their own elements, and throw with the element's path when an inner array or map is malformed.
- JSON `null` decodes as the zero value and never as an error. `encoding/json` writes a nil pointer, slice, map or `[]byte` as `null` when the field has no `omitempty`, and the decoders accept that output. An optional field decodes a present `null` as `undefined`, a required slice or map decodes `null` as `[]` or `{}`, and a required `[]byte` decodes `null` as `""`. `json.RawMessage` and `interface{}` fields pass `null` through as data.
- `json:",string"` types the field as `string` and decodes it with `reqStr` or `optStr`, matching how `encoding/json` wraps numbers and booleans in a string.
- Map keys are always `string` in the TypeScript, whatever the Go key type, because JSON object keys are strings.
- Embedded named structs are flattened into the embedding interface with the promotion rules of `encoding/json`. The shallowest field wins, a tagged field beats an untagged one at the same depth, and a field reached through two sibling embeds at the same depth is dropped as an ambiguous promotion.
- An enum with no discoverable `const` values and no explicit `Values` emits `export type X = never;`.

## Type and decoder mappings

`TypeMappings` sets the TypeScript type for a Go type, and `DecoderMappings` sets the decoder helper the generated code calls for it. Both are keyed by full `importpath.Type`, such as `".../uuid.UUID"`.

A key may name a type alias or the type it resolves to, and both work. `go/types` resolves an alias past its own name, so the resolved spelling differs from the one your source shows, and the standard library moves it. `json.RawMessage` is a named type through Go 1.26 and an alias for `encoding/json/jsontext.Value` from Go 1.27, so either key keeps a mapping in force across a toolchain upgrade.

## Identifiers

Every generated identifier is valid TypeScript. Strings that land in an identifier position are cleaned to a valid identifier, with a safe fallback when nothing valid is left. Those strings are the struct and enum name overrides, the two registry function names, a `//wiregen:union` discriminator, field wire names and decoder local variables.

A JSON key that is not a valid identifier, such as `content-type`, is emitted as a quoted property and read with bracket access, `out["content-type"]`. A value that is already a valid identifier is emitted unchanged.

## Discriminated unions

Declare a union in Go source with a directive on a sealed interface, one with an unexported method:

```go
//wiregen:union discriminator=type variants=CoverageEvent,NotifyEvent,ScanEvent
type EventData interface{ eventData() }
```

The types file then carries `export type EventData = CoverageEvent | NotifyEvent | ScanEvent`. When `DiscriminatorMap["EventData"]` is set, wiregen also writes two runtime decoders:

- `decodeEventData(disc: string, v: unknown): EventData`, for a caller that already holds the discriminator, such as an SSE event name.
- `decodeEventDataPayload: Decoder<EventData>`, which reads the discriminator key off the payload object itself.

A union type can be registered in `SSEEvents`, and the registry then binds its payload decoder. `Generate` fails when a union is registered in `SSEEvents` without a `DiscriminatorMap` entry, because no runtime decoder would exist to bind.

## Unsupported by design

These are deliberate non-goals.

| Feature | Reason |
| --- | --- |
| Go generics (type parameters) | wiregen cannot represent an uninstantiated generic type. Register concrete instantiations instead. |
| Nullable versus optional | wiregen never emits `T \| null`. A pointer or `omitempty` field becomes optional, `?:`, and a present `null` decodes as `undefined`. |
| `tstype` struct tag hints | `TypeMappings` provides the same escape hatch at the registry level. |
| Inline anonymous struct fields | A field typed as an inline `struct { ... }` literal maps to `unknown`. Register a named type instead. Embedded named structs are still flattened. |
