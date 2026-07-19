package wiregen_test

import (
	"fmt"

	"github.com/cplieger/wiregen/v2"
	"github.com/cplieger/wiregen/v2/testdata/basic"
)

func Example() {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./validators.js"),
		wiregen.WithBusImport("./bus.js"),
	)
	r.PackagePaths = []string{"github.com/cplieger/wiregen/v2/testdata/basic"}
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	r.SSEEvents = []wiregen.SSERegEntry{
		{EventType: "addr", TypeName: "Address"},
	}

	ts, err := r.GenerateTypes()
	if err != nil {
		fmt.Println("generate:", err)
		return
	}
	fmt.Println(ts != "")
	// Output:
	// true
}
