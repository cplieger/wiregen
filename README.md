# wiregen
> Generate TypeScript interfaces, decoders, and an SSE registry from Go types via reflection.

wiregen is a standalone Go library that, given a set of registered `reflect.Type` values and enum definitions, emits fully-typed TypeScript: interface declarations, runtime decoder functions with validation, and an SSE event→decoder registry. It has zero dependencies beyond the Go standard library.

## Install
<!-- TODO: registry/pull link -->
Go: `go get github.com/cplieger/wiregen@latest`

## Usage
```go
package main

import (
	"fmt"
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
	r := &wiregen.Registry{
		WireTypes: []reflect.Type{reflect.TypeFor[User]()},
		Enums: map[string]wiregen.EnumDef{
			"Status": {Values: []string{"active", "inactive"}},
		},
	}
	if err := r.Generate("./wire"); err != nil {
		fmt.Println(err)
	}
}
```

## API

- `wiregen.Registry` — holds all configuration: `WireTypes`, `Enums`, `EnumTSName`, `TSNameOverride`, `PathNameOverride`, `SSEEvents`, `ValidatorsImport`, `BusImport`.
- `(*Registry).Generate(outDir string) error` — writes `types.gen.ts`, `decoders.gen.ts`, `registry.gen.ts` to `outDir`.
- `(*Registry).GenerateTypes() string` — returns types file content.
- `(*Registry).GenerateDecoders() string` — returns decoders file content.
- `(*Registry).GenerateRegistry() string` — returns registry file content.

## License
GPL-3.0 — see [LICENSE](LICENSE).
