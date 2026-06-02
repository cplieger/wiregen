package wiregen_test

import (
	"fmt"
	"reflect"

	"github.com/cplieger/wiregen"
)

type ExStatus string

type ExUser struct {
	ID     int      `json:"id"`
	Name   string   `json:"name"`
	Status ExStatus `json:"status"`
}

func Example() {
	r := wiregen.NewRegistry(
		wiregen.WithValidatorsImport("./validators.js"),
		wiregen.WithBusImport("./bus.js"),
	)

	// Payload types are set via exported fields on the Registry.
	r.WireTypes = []reflect.Type{reflect.TypeFor[ExUser]()}
	r.Enums = map[string]wiregen.EnumDef{
		"ExStatus": {Values: []string{"active", "inactive"}},
	}
	r.SSEEvents = []wiregen.SSERegEntry{
		{EventType: "user", TypeName: "ExUser"},
	}

	// Verify generation works without writing to disk.
	ts := r.GenerateTypes()
	fmt.Println(ts != "")
	// Output:
	// true
}
