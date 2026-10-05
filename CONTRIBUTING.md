# Contributing to wiregen

The [shared rules](https://github.com/cplieger/.github/blob/main/CONTRIBUTING.md) for commits, releases, dependencies and checks apply here.

## Checks

After a change that alters the generated TypeScript on purpose, regenerate the golden files in `testdata/golden/` instead of editing them by hand:

```sh
go test -run TestGolden -update
```

Review the regenerated `testdata/golden/*.gen.ts` files before you commit them. The diff shows how the change alters the generated TypeScript.
