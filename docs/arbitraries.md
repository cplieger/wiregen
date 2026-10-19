# Property-test arbitraries

This page describes the optional fast-check module wiregen can write. It is for a developer who wants property tests over every wire type without writing one generator per type by hand.

## The module

`WithArbitrariesFile("arbitraries.gen.ts")` writes a [fast-check](https://fast-check.dev) arbitrary for each registered type and enum, plus a lookup table. The arbitraries come from the same parsed model as the decoders:

```ts
export const arbUser: fc.Arbitrary<User> = arbModel.User;
export const arbStatus: fc.Arbitrary<Status> = fc.constantFrom("active", "banned");
export const ARBITRARY_BY_TYPE: globalThis.Record<string, fc.Arbitrary<unknown>> = { User: arbUser, ... };
```

The module imports fast-check as its default export, `fc`, from `"fast-check"` unless `WithArbitrariesImport` changes the specifier.

A property test iterates the table instead of keeping one arbitrary per type by hand. It can also assert that every struct row has an exported decoder and every exported decoder has a row. Enum rows have no decoder, so that check skips them. A hand-kept list can fall behind the registry without anyone noticing. A deleted wire type takes its generated arbitrary with it, and a new type that no decoder matches fails the check.

## How values are drawn

For fields wiregen can model, each arbitrary comes from the same metadata as its decoder, so the drawn values pass the generated checks:

- An enum draws from its registered values, which is what `reqOneOf` accepts.
- A number is finite, because `reqNum` rejects `NaN` and `Infinity` and JSON cannot spell them.
- A `[]byte` is a base64 string.
- A `json.RawMessage` is any JSON value.
- A slice or a map is a small collection over its element's arbitrary.
- A bounded type draws within its bounds: an integer within its `Range`, a string matching its `Pattern` and within its `MaxBytes`, and a slice within its `Count`.
- A union draws from any of its cases, the tag fixed to the case's value.

## Properties of the module

- Every type is tied through one `fc.letrec`, so a self reference resolves, and so does a mutual cycle such as `A` holding a `B` that holds an `A`. A type that can reach itself draws from `fc.oneof` with `maxDepth: 5`. Its first branches are leaves, with recursive optional fields absent and recursive collections empty. Every emitted collection and every such `oneof` share one `depthIdentifier`, so depth counts across the whole model. At depth 5 a recursive type is forced to its leaf.
- An optional field is absent, never set to `undefined`. `fc.record`'s `requiredKeys` lists the required fields. The two are the same to JSON, and absence is the only one assignable to the emitted `key?: T` under `exactOptionalPropertyTypes`.
- A map key is any string, `"__proto__"` included, which the decoders keep as an own property.
- A pointer element of a slice or map is drawn as `null` some of the time, as `encoding/json` writes a nil one.
- A type with a `Mappings` entry is an exception to the drawn values passing the generated checks, unless it maps to a primitive TypeScript type. A mapping is a TypeScript expression, so its shape is not in wiregen's model. The field gets an arbitrary JSON value and a cast, the same trust the emitted decoder already gives with `own(o, k) as T`. If your mapped decoder checks its input, write that type's arbitrary by hand.
