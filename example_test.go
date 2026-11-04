package wiregen_test

import (
	"context"
	"fmt"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/basic"
)

func Example() {
	r := newTestRegistry(
		wiregen.WithValidatorsImport("./validators.js"),
	)
	r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
	r.Events = []wiregen.Event{
		r.event("addr", "Address"),
	}

	ts, err := r.typesFile(context.Background())
	if err != nil {
		fmt.Println("generate:", err)
		return
	}
	fmt.Println(ts != "")
	// Output:
	// true
}
