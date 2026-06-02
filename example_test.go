package wiregen_test

import (
	"fmt"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/basic"
)

func Example() {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./validators.js"),
		wiregen.WithBusImport("./bus.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{
		{EventType: "addr", TypeName: "Address"},
	}

	ts := r.GenerateTypes()
	fmt.Println(ts != "")
	// Output:
	// true
}
