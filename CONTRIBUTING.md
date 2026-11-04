# Contributing to wiregen

The [shared rules](https://github.com/cplieger/.github/blob/main/CONTRIBUTING.md) for commits, releases, dependencies and checks apply here.

## Checks

`go test ./...` runs generated decoders, so it needs Node.js 22.6 or later on `PATH`, which runs TypeScript by stripping its types. `go test -tags integration ./...` also type-checks the generated files and needs `tsc` on `PATH`.

After a change that alters the generated TypeScript on purpose, regenerate the golden files in `testdata/golden/` instead of editing them by hand:

```sh
go test -run TestGolden -update
```

Review the regenerated `testdata/golden/*.gen.ts` files before you commit them. The diff shows how the change alters the generated TypeScript.
