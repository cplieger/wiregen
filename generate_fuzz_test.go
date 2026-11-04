package wiregen_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/basic"
)

func FuzzGenerate(f *testing.F) {
	f.Add("./validators.js", "./types.js", "notification", "Notification")
	f.Add("./v.js", "./t.js", "event:start", "Address")
	f.Add("", "./t.js", "test", "User")
	f.Add("./v.js", "", "x", "Address")
	f.Add("", "", "x", "HasTime")
	f.Add("", "", "x", "Status")
	f.Add("", "", "x", "Missing")
	f.Add("", "", "", "User")

	f.Fuzz(func(t *testing.T, validatorsImport, typesImport, eventType, typeName string) {
		r := newTestRegistry(
			wiregen.WithValidatorsImport(validatorsImport),
			wiregen.WithTypesImportPath(typesImport),
		)
		address := wiregen.TypeRef[basic.Address]()
		r.Types = []wiregen.WireType{
			address,
			wiregen.TypeRef[basic.User](),
			wiregen.TypeRef[basic.Notification](),
		}
		r.Enums = []wiregen.EnumRef{wiregen.StringEnum[basic.Status]()}
		// typeName is any string, resolved in basic's package: a listed or
		// unlisted struct, an enum, a non-struct or nothing.
		r.Events = []wiregen.Event{{Name: eventType, Data: wiregen.WireType{PkgPath: address.PkgPath, Name: typeName}}}
		r.Constants = []wiregen.WireConst{{TSName: "X", Value: 42}}

		// Generate must either refuse the registry, as every getter then does,
		// or write each file byte-identically to its getter.
		dir := t.TempDir()
		if err := r.generate(t.Context(), dir); err != nil {
			if _, gerr := r.typesFile(t.Context()); gerr == nil {
				t.Errorf("Generate refused (event=%q type=%q) with %v, but the types getter rendered", eventType, typeName, err)
			}
			return
		}
		files := []struct{ name, want string }{
			{"types.gen.ts", mustGen(t, r.typesFile)},
			{"decoders.gen.ts", mustGen(t, r.decodersFile)},
			{"events.gen.ts", mustGen(t, r.eventsFile)},
			{"constants.gen.ts", mustGen(t, r.constantsFile)},
		}
		for _, fc := range files {
			got, err := os.ReadFile(filepath.Join(dir, fc.name))
			if err != nil {
				t.Errorf("Generate(validators=%q types=%q) did not write %s: %v", validatorsImport, typesImport, fc.name, err)
				continue
			}
			if string(got) != fc.want {
				t.Errorf("Generate %s differs from getter (event=%q type=%q):\ndisk:\n%s\ngetter:\n%s", fc.name, eventType, typeName, got, fc.want)
			}
		}
	})
}
