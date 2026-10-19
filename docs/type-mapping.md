# Type mapping

This page states how wiregen turns each Go type into TypeScript, a decoder and an OpenAPI schema. It is for a developer checking that the generated code matches what their server sends. wiregen reads the registered packages from source with `go/packages`, `go/types` and `go/ast`, and its field walk follows `encoding/json`.

## Field rules

- A registered type is a struct. Register an enum with `StringEnum` or `TextEnum`, and a type wiregen cannot read with a mapping.
- Doc comments on registered structs and their fields become `/** ... */` JSDoc on the generated interfaces.
- Unexported fields are skipped, as `encoding/json` skips them. An embedded struct, exported or not, is flattened with the promotion rules of `encoding/json`: the shallowest field wins, a tagged field beats an untagged one at the same depth, and a field reached through two sibling embeds at the same depth is dropped as ambiguous.
- A field with no `json` tag takes the name `encoding/json` gives it, its Go name. With `WithRequireJSONTags()` such a field is a generation error naming its path. A tag name may hold letters, digits, spaces and the punctuation `!#$%&()*+-./:;<=>?@[]^_{|}~`.
- A field `encoding/json` may leave out, or send as `null` for a nil pointer, is optional in TypeScript, `key?: T`: a pointer, a field tagged `omitzero`, a field tagged `omitempty` whose type `omitempty` can find empty (never a struct), and a field promoted through an embedded pointer, which is skipped when nil. Every other field is required.
- A pointer element of a slice or map is `T | null`, as `encoding/json` writes a nil one as `null`.
- `time.Time` maps to `string`, and `json.Number` to `number`. A slice of `byte`, or of any other `uint8` type without a marshaler, maps to `string`, because JSON encodes it as base64.
- `json.RawMessage` (and `jsontext.Value`, which it aliases from Go 1.27) maps to `unknown`. It is the one opaque type wiregen lets cross the wire.
- A type implementing `encoding.TextMarshaler` with a value receiver maps to `string`, a fixed-size array included. Marshalers are found by their method signatures, promoted methods included, as `encoding/json` finds them.
- `json:",string"` types the field as `string`, as `encoding/json` writes the value's JSON encoding as text. It applies to a boolean, number or string with no marshaler, under at most one pointer. The decoder and the schema's `pattern` hold the text to that encoding's grammar: `"5"` for an integer, `"true"` for a boolean, `"\"a\""` for a string.
- A map is `{ [key: string]: V }` in the TypeScript, because JSON object keys are strings.
- Nested collections are checked at every level, and a malformed inner array or map throws with the element's path.

## What Generate refuses

`Generate` refuses a field whose JSON shape it cannot check, so nothing typed `unknown` reaches the client by accident. It collects every such field and reports them together, each as `importpath.Type.Field: reason`, before writing anything:

- a `time.Duration`, which `encoding/json` writes as integer nanoseconds. Send an `int64` of milliseconds instead, named with an `_ms` suffix.
- an interface or `any`, named or not. Use `json.RawMessage`.
- an inline anonymous struct. Declare and register a named struct.
- a named struct that is neither registered nor mapped.
- a type implementing `json.Marshaler`, whose output can take any shape, unless it is mapped.
- a type with a pointer-receiver `MarshalText`, which `encoding/json` calls only for addressable values, so one field encodes two ways.
- a registered struct implementing `json.Marshaler` or `encoding.TextMarshaler`, through its own methods or promoted ones such as an embedded `time.Time`'s, since `encoding/json` writes the marshaler's output and not its fields. Map it instead.
- the `,string` option on a field it does not apply to, which `encoding/json` ignores, on a mapped type, whose mapping states a wire form the option rewrites, or on a bounded type, whose bounds do not describe the text.
- a tag name with a character outside the set above, such as a backslash, a quote or `€`, which the Go releases' `encoding/json` implementations read differently.
- a fixed-size array, a channel, a function, a complex number or an `unsafe.Pointer`.
- a map whose key is not a string, an integer or an `encoding.TextMarshaler`.
- a bound method wiregen cannot read, or one that does not fit the field (see below).

In a type an endpoint's `Request`, `Params` or `Query` carries, or one those reach, it also refuses a field `encoding/json` cannot decode into:

- a field promoted through an embedded pointer to an unexported struct, which `encoding/json` cannot allocate.
- a type implementing `json.Marshaler` without `json.Unmarshaler`, or `encoding.TextMarshaler` without `encoding.TextUnmarshaler`, mapped or not. A `TextEnum` in a request needs `UnmarshalText` on its pointer.
- a map key implementing `encoding.TextMarshaler` without `encoding.TextUnmarshaler`.

The rules apply at any depth of slices and maps.

## Enums

An enum is registered by its type, and its members are the constants of exactly that type declared in the type's own package, in source order. The package is loaded whether or not a registered struct lives there.

- `StringEnum[T]()` registers a string type, whose members are the constants' values.
- `TextEnum[T]()` registers a `uint8` type with a `MarshalText` method, whose members are the names `MarshalText` gives the constants. A constant `MarshalText` refuses, such as an unnamed zero, is no member.

A string enum with a marshaler, or an integer enum with `MarshalJSON`, is refused, since `encoding/json` would write something other than its members.

Repeated values count once, two values with one wire name are refused, and an enum with no members is refused. Enum and struct names share one namespace. A field of an enum type that is not registered is a plain `string`. The TypeScript is a string union, the decoder checks membership with `reqOneOf`, and the OpenAPI component is `{"type": "string", "enum": [...]}`.

## Bounds

A field type can declare its own limits through methods with a value receiver whose body is one `return` of constants. wiregen reads the constants from source without running the code, and the server's binder can call the same methods, so each limit has one source.

| Method | On | Schema | Generated check |
| --- | --- | --- | --- |
| `Range() (lo, hi int64)` | an integer type | `minimum`, `maximum` | `checkRange`: an integer from lo to hi |
| `MaxBytes() int` | a string type | `x-max-bytes`, plus `maxLength` at the same number | `checkBytes`: at most that many UTF-8 bytes |
| `Pattern() string` | a string type | `pattern` | `checkPattern`: an ECMA-262 match, unanchored like JSON Schema's |
| `Count() (lo, hi int)` | a slice type | `minItems`, `maxItems` | `checkCount`: lo to hi items |

A pattern is checked by Go's `regexp` in the binder and by ECMA-262 in the decoder, so write it in the syntax both share: no inline flags such as `(?i)`, no POSIX classes such as `[[:alpha:]]`, and `\p{L}` with braces. `Generate` refuses a pattern Go's `regexp` does not compile.

`maxLength` counts code points, so a generic JSON Schema validator checks only a coarse form of `MaxBytes`. The generated decoder checks the exact byte count. A slice whose `Count` lower bound is 1 or more refuses `null`, in the decoder and in the schema, because `encoding/json` decodes `null` to an empty slice below the bound. Under a pointer, `null` is the nil pointer and stays allowed.

A pointer-receiver bound method, one whose body is not a single return of constants, an empty range, or a method on the wrong kind of type, such as `Range` on a string, is refused with the field's path.

## Null and presence

The decoders follow what `encoding/json` writes:

- An optional field reads an absent key or `null` as absent. A mapped decoder is never called on either.
- A required slice or map reads `null` as `[]` or `{}`, and a required `[]byte` reads it as `""`, unless a `Count` lower bound refuses it.
- A pointer element of a slice or map reads `null` as `null`. Any other nil element reads as empty, as a required field does.
- A `json.RawMessage` field passes `null` through as data.
- A decoder reads only the object's own properties: a key such as `constructor` that every JavaScript object inherits is absent unless the JSON sends it, and a `__proto__` key, in a map or as a field, is kept as an own property.

The OpenAPI schemas say the same thing in each direction:

- A response field is required unless `encoding/json` can leave it out: `omitzero`, `omitempty` on a type it can find empty, or a field promoted through an embedded pointer. A request field is also optional when it is a pointer, as the decoder leaves it nil.
- A response field is nullable, `{"type": [T, "null"]}` or an `anyOf` with `null` around a `$ref`, when `encoding/json` can write `null` for it: a nil pointer or collection no omit option drops, or anything nil under a non-nil pointer, such as a `*[]string` holding a nil slice or a `**T` holding a nil `*T`. A request field is nullable when it is a pointer, slice, map or `[]byte`, since `null` decodes to nil. A `Count` lower bound of 1 or more makes a collection with no pointer above it never nullable.
- A type whose two schemas differ gets two components, the request one suffixed `Input`, and the document references each in its own direction. A registered type no endpoint reaches is documented in its response form, and so is every type its fields name.

## Unions

`Registry.Unions` declares a registered struct a union tagged by one of its fields:

```go
wiregen.Union{
    Type: wiregen.TypeRef[wire.Timing](),
    Tag:  "kind",
    Cases: map[string][]string{
        "identity": {},
        "shift":    {"ms"},
        "linear":   {"scale", "offset_ms"},
    },
}
```

`Tag` and the field lists are wire names. The tag is a required string or enum field, and an enum tag's cases are exactly its members. Each case lists the other fields it allows, every field belongs to some case, and a listed field keeps its own presence rule. A field some case leaves out must be tagged `omitempty` or `omitzero`, since otherwise `encoding/json` writes it in every case and the decoder and the case schema would refuse that output. Cases are emitted in the enum's order, or by value for a string tag.

- The types file carries `export type Timing = | { kind: "identity" } | { kind: "shift"; ms?: number } | ...`.
- `decodeTiming` reads the tag, refuses a present field another case owns, and decodes the case's fields.
- The document carries a `oneOf` with a `discriminator` over one closed component per case, `Timing.shift` say, each with `additionalProperties: false`.

## Mappings

`Registry.Mappings` gives a Go type the consumer's own wire form, in place of wiregen's reading of it. It is keyed by full `importpath.Type`, such as `".../uuid.UUID"`:

```go
r.Mappings = map[string]wiregen.Mapping{
    "example.com/app/id.UUID": {
        TS:      "string",
        Decoder: "reqStr",
        Schema:  map[string]any{"type": "string", "format": "uuid"},
    },
}
```

- `TS` is the TypeScript type expression.
- `Decoder` is a TypeScript expression called as `(obj, key, path)` that returns the field's value or throws. An empty one casts the value unchecked. The validators module's helpers, such as `reqStr`, are in scope.
- `Schema` is the value's JSON Schema, required when an OpenAPI document is written. A `$ref` in it must be a string whose JSON pointer ([RFC 6901](https://www.rfc-editor.org/rfc/rfc6901)) names something in the document, such as `#/components/schemas/Item`. `Generate` refuses one that does not, or that points into another document.

A key may name a type alias or the type it resolves to, and both work. `go/types` resolves an alias past its own name, and the standard library re-homes types behind aliases: `json.RawMessage` is an alias for `encoding/json/jsontext.Value` from Go 1.27, so either key keeps a mapping in force across a toolchain upgrade.

## Identifiers

Every generated identifier is valid TypeScript. The struct and enum name overrides are cleaned to a valid identifier, with a safe fallback when nothing valid is left.

The generated modules import one another, so every name they bind shares one namespace: each type's and enum's TypeScript name, each decoder (`decode<Name>`), each enum's member list (`<NAME>S`), each arbitrary, each endpoint's bindings, the validators module's exports, and the fixed exports of the events, client and arbitraries modules. `Generate` refuses two of them with one name before writing anything. Rename one through `TSNameOverride` or `EnumTSName`. The generated code names every global it uses through `globalThis`, such as `globalThis.Object`, so a type may take a global's name. A keyword, a predefined type name such as `string`, `undefined` or `globalThis` takes the suffix `Val` as a type name and is refused as an endpoint name.

A JSON key that is not a valid identifier, such as `content-type`, is emitted as a quoted property and read with bracket access, `out["content-type"]`.

## Unsupported by design

| Feature | Reason |
| --- | --- |
| Go generics (type parameters) | wiregen cannot represent an uninstantiated generic type. Register concrete instantiations instead. |
| `T \| null` on a field | A field that can be absent or `null` is optional, `?:`, and a present `null` decodes as `undefined`. The OpenAPI document carries the exact nullability per direction. |
| `tstype` struct tag hints | `Mappings` does the same job at the registry level. |
| Interface-typed unions | Strict mode refuses an interface field. Declare a tagged struct and register it in `Unions`. |
