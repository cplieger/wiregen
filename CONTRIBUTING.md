# Contributing to wiregen

wiregen is a build-time Go library that emits fully-typed TypeScript
(interfaces, validating decoders, an SSE event→decoder registry, and a
constants file) from registered Go types. This guide covers the parts a
contributor needs that the generic defaults don't: the codegen
architecture, the correctness contract, and the golden-file workflow.

## Architecture

The engine analyzes source with `go/packages` + `go/types` + `ast.Inspect`
(`ast_engine.go`), not reflection. The field walk reads the registered
packages' AST, which is why doc comments flow through to JSDoc and why
`PackagePaths` must cover every registered type's package.

A `Registry` is built in two phases (`wiregen.go`):

1. `NewRegistry(opts...)` configures **behavior knobs** via functional
   options (`WithValidatorsImport`, `WithBusImport`, `WithFilenames`, …).
2. **Payload data** is then assigned to the returned `*Registry`'s
   exported fields (`Types`, `PackagePaths`, `Enums`, `SSEEvents`,
   `Constants`, the various override maps).

Types are registered by identity with the compile-time-safe
`TypeRef[T]()` helper, which captures the `{PkgPath, Name}` pair. Then
`Generate(outDir)` writes the files **atomically** — it builds each
file's content in memory, then stages it to a temp sibling and renames it
into place (`writeFilesAtomically`), so a mid-run failure never leaves a
half-updated output directory — or the per-file generators
(`GenerateTypes`, `GenerateDecoders`, `GenerateRegistry`,
`GenerateConstants`) return strings.

`Generate` validates required imports up front and returns an error (it
does **not** panic). The string getters keep the panic-by-design
contract: `GenerateDecoders` panics if `ValidatorsImport` is empty, and
`GenerateRegistry` panics if `BusImport` is empty and
`SelfContainedRegistry` is false.

Discriminated unions are declared in Go source on a sealed interface with
a directive comment:

```go
//wiregen:union discriminator=type variants=CoverageEvent,NotifyEvent,ScanEvent
type EventData interface{ eventData() }
```

This emits `export type EventData = CoverageEvent | NotifyEvent |
ScanEvent`. A runtime union decoder is emitted only when
`DiscriminatorMap[EventData]` is set (see `testdata/unions/`).

## Contracts that must be preserved

These are load-bearing; changes that break them are bugs, not features.

- **encoding/json fidelity.** The AST field walk mirrors `encoding/json`
  exactly: unexported fields are skipped, `[]byte` → `string` (base64),
  `time.Time` → `string`, `json.RawMessage` and `interface{}` →
  `unknown`, `json.Number` → `number`, embedded named structs are
  flattened, `omitzero` (Go 1.24+) is treated like `omitempty`,
  `json:",string"` types the field as `string`, and map keys are always
  `string`. Field promotion follows the shallowest-wins rule: a tagged
  field dominates an untagged one at equal depth, and a field reachable
  through two sibling embeds at equal depth (a "diamond") is dropped as
  an ambiguous promotion. When extending the walk, match
  `encoding/json`'s behavior.
- **Generated identifiers are valid TypeScript.** Every consumer- or
  source-derived string that lands in an identifier position (the name
  overrides, the registry func-name knobs, a `//wiregen:union`
  discriminator, field wire names, decoder local variables) is sanitized
  to a valid TS identifier with a safe fallback, and a non-identifier
  JSON key is emitted as a quoted property + bracket access. Sanitizing
  is a no-op for already-valid identifiers, so don't "simplify" a sink
  back to a raw emit — that reintroduces non-compiling output for
  edge-case input.
- **Unsupported by design.** Go generics, the nullable-vs-optional
  distinction, `tstype` tag hints, and inline anonymous struct fields are
  deliberate non-goals, not TODOs. `TypeMappings` is the registry-level
  escape hatch for custom type mappings.

Output must also be **deterministic** — byte-identical across runs and
independent of type-registration order. Tests enforce this
(`TestDeterministic*`, `TestCloseout_Determinism*`); keep map iteration
sorted when you touch generation.

## Local development

The module targets the Go version in `go.mod` and
has zero runtime dependencies — the only build-time dependency is
`golang.org/x/tools`. Keep it that way.

Run the tests:

```sh
go test ./...
go test -race ./...
```

Fuzz targets live alongside the unit tests (`fuzz_test.go`,
`fuzz_completeness_test.go`). Run one with, e.g.:

```sh
go test -run '^$' -fuzz FuzzParseUnionDirective -fuzztime 30s
```

Lint and format with the v2 golangci-lint config (`.golangci.yaml`).
`golangci-lint run` reports unformatted files as issues, so formatting is
enforced; `golangci-lint fmt` applies the `gofumpt` (with `extra-rules`)
and `gci` formatters:

```sh
go vet ./...
golangci-lint run
golangci-lint fmt
```

## Golden files

`TestGolden_Types` and `TestGolden_Decoders` compare generated output
against committed fixtures in `testdata/golden/`. The test fixtures used
to drive generation live in `testdata/{basic,edges,unions}/`.

When a generation change legitimately alters the output, regenerate the
golden files with the package's `-update` flag and commit the diff:

```sh
go test -run TestGolden -update
```

Review the regenerated `testdata/golden/*.gen.ts` carefully — the diff is
the human-readable record of how the change affects emitted TypeScript.

## Commits & pull requests

This repo uses [Conventional Commits](https://www.conventionalcommits.org/);
git-cliff parses them for release notes and version bumps (see
`cliff.toml`). `feat:` → minor, `fix:`/`sec:` → patch, `feat!:` or a
`BREAKING CHANGE:` footer → major. `docs:`, `test:`, `fuzz:`, `chore:`,
and `ci:` don't trigger a release. Write the subject as the changelog
line a consumer would read. Open a PR against `main`; CI runs vet, lint,
race tests, and the golden-file checks.

## Conduct & security

By participating you agree to the org-wide
[Code of Conduct](https://github.com/cplieger/.github/blob/main/CODE_OF_CONDUCT.md).
Report security issues through the
[security policy](https://github.com/cplieger/.github/blob/main/SECURITY.md),
never in a public issue.
