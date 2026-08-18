package wiregen_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cplieger/wiregen/v3"
	"github.com/cplieger/wiregen/v3/testdata/basic"
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
		r.PackagePaths = []string{"github.com/cplieger/wiregen/v3/testdata/basic"}
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
		r.Constants = []wiregen.WireConst{{TSName: "X", Value: 42}}

		// Generate must either fail cleanly (an empty required import) or write
		// every file byte-identically to its matching per-file getter. This
		// pins the write-orchestration (filename->content mapping plus the
		// SSE/constants gating) that the string API never exercises, turning a
		// crash-only target into one that asserts a real invariant.
		dir := t.TempDir()
		if err := r.Generate(dir); err != nil {
			return // empty ValidatorsImport/BusImport: the getters error the same way
		}
		files := []struct{ name, want string }{
			{"types.gen.ts", mustGen(t, r.GenerateTypes)},
			{"decoders.gen.ts", mustGen(t, r.GenerateDecoders)},
			{"registry.gen.ts", mustGen(t, r.GenerateRegistry)},
			{"constants.gen.ts", mustGen(t, r.GenerateConstants)},
		}
		for _, fc := range files {
			got, err := os.ReadFile(filepath.Join(dir, fc.name))
			if err != nil {
				t.Errorf("Generate(validators=%q bus=%q) did not write %s: %v", validatorsImport, busImport, fc.name, err)
				continue
			}
			if string(got) != fc.want {
				t.Errorf("Generate %s differs from getter (event=%q type=%q):\ndisk:\n%s\ngetter:\n%s", fc.name, eventType, typeName, got, fc.want)
			}
		}
	})
}
