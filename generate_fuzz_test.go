package wiregen_test

import (
	"testing"

	"github.com/cplieger/wiregen"
	"github.com/cplieger/wiregen/testdata/basic"
)

func FuzzGenerate(f *testing.F) {
	f.Add("./validators.js", "./bus.js", "notification", "Notification")
	f.Add("./v.js", "./b.js", "event:start", "Address")
	f.Add("", "./b.js", "test", "User")
	f.Add("./v.js", "", "x", "Address")

	f.Fuzz(func(t *testing.T, validatorsImport, busImport, eventType, typeName string) {
		r := wiregen.NewRegistry(
			wiregen.WithValidatorsImport(validatorsImport),
			wiregen.WithBusImport(busImport),
		)
		r.PackagePaths = []string{"github.com/cplieger/wiregen/testdata/basic"}
		r.Types = []wiregen.WireType{
			wiregen.TypeRef[basic.Address](),
			wiregen.TypeRef[basic.User](),
			wiregen.TypeRef[basic.Notification](),
		}
		r.Enums = map[string]wiregen.EnumDef{
			"Status": {Values: []string{"active", "inactive"}},
		}
		r.SSEEvents = []wiregen.SSERegEntry{
			{EventType: eventType, TypeName: typeName},
		}

		// GenerateTypes should never panic
		_ = r.GenerateTypes()

		// GenerateDecoders should only panic if ValidatorsImport empty
		func() {
			defer func() { recover() }()
			_ = r.GenerateDecoders()
		}()

		// GenerateRegistry should only panic if BusImport empty (non-self-contained)
		func() {
			defer func() { recover() }()
			_ = r.GenerateRegistry()
		}()

		// GenerateConstants should never panic
		r.Constants = []wiregen.WireConst{{TSName: "X", Value: 42}}
		_ = r.GenerateConstants()
	})
}
