package wiregen_test

import (
	"context"
	"fmt"

	"github.com/cplieger/wiregen/v3"
	"github.com/cplieger/wiregen/v3/testdata/basic"
)

func Example() {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./validators.js"),
		wiregen.WithBusImport("./bus.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/v3/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{
		{EventType: "addr", TypeName: "Address"},
	}

	ts, err := r.GenerateTypes(context.Background())
	if err != nil {
		fmt.Println("generate:", err)
		return
	}
	fmt.Println(ts != "")
	// Output:
	// true
}
