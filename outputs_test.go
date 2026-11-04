package wiregen_test

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/basic"
	"github.com/cplieger/wiregen/v4/testdata/enums"
)

// Two outputs at one path, however spelled, are refused before anything is
// written: the second would silently replace the first.
func TestGenerate_RefusesOutputCollisions(t *testing.T) {
	cases := []struct {
		name string
		opts func(t *testing.T, dir string) []wiregen.Option
		want string
	}{
		{"types and decoders", func(*testing.T, string) []wiregen.Option {
			return []wiregen.Option{wiregen.WithFilenames(wiregen.Filenames{Types: "same.ts", Decoders: "same.ts"})}
		}, "the types and decoders files"},
		{"validators onto types", func(*testing.T, string) []wiregen.Option {
			return []wiregen.Option{wiregen.WithValidatorsFile("types.gen.ts")}
		}, "the types and validators files"},
		{"a lexical alias", func(*testing.T, string) []wiregen.Option {
			return []wiregen.Option{wiregen.WithFilenames(wiregen.Filenames{Decoders: "a/../types.gen.ts"})}
		}, "the types and decoders files"},
		{"an OpenAPI path onto a TS file", func(t *testing.T, dir string) []wiregen.Option {
			return []wiregen.Option{wiregen.WithOpenAPIFiles(filepath.Join(dir, "types.gen.ts"))}
		}, "the types and openapi files"},
		{"two OpenAPI paths", func(t *testing.T, dir string) []wiregen.Option {
			return []wiregen.Option{wiregen.WithOpenAPIFiles(filepath.Join(dir, "x.json"), filepath.Join(dir, "..", filepath.Base(dir), "x.json"))}
		}, "the openapi and openapi files"},
		{"a symlinked directory onto the TS directory", func(t *testing.T, dir string) []wiregen.Option {
			if err := os.Symlink(".", filepath.Join(dir, "alias")); err != nil {
				t.Fatal(err)
			}
			return []wiregen.Option{wiregen.WithOpenAPIFiles(filepath.Join(dir, "alias", "types.gen.ts"))}
		}, "the types and openapi files"},
		{"an OpenAPI path climbing out of a symlink", func(t *testing.T, dir string) []wiregen.Option {
			for _, d := range []string{"a", "sub"} {
				if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(filepath.Join("..", "sub"), filepath.Join(dir, "a", "link")); err != nil {
				t.Fatal(err)
			}
			return []wiregen.Option{wiregen.WithOpenAPIFiles(filepath.Join(dir, "a", "link") + string(filepath.Separator) + ".." + string(filepath.Separator) + "types.gen.ts")}
		}, "the types and openapi files"},
		{"a TS name through a symlink inside the TS directory", func(t *testing.T, dir string) []wiregen.Option {
			if err := os.Symlink(".", filepath.Join(dir, "alias")); err != nil {
				t.Fatal(err)
			}
			return []wiregen.Option{wiregen.WithFilenames(wiregen.Filenames{Decoders: "alias/types.gen.ts"})}
		}, "the types and decoders files"},
		{"two names differing only in case", func(*testing.T, string) []wiregen.Option {
			return []wiregen.Option{wiregen.WithFilenames(wiregen.Filenames{Decoders: "Types.gen.ts"})}
		}, "the types and decoders files"},
		{"a case variant of an existing file", func(t *testing.T, dir string) []wiregen.Option {
			writeFile(t, filepath.Join(dir, "types.gen.ts"), "old")
			return []wiregen.Option{wiregen.WithFilenames(wiregen.Filenames{Decoders: "Types.gen.ts"})}
		}, "the types and decoders files"},
		{"a case variant of an existing directory", func(t *testing.T, dir string) []wiregen.Option {
			if err := os.Mkdir(filepath.Join(dir, "gen"), 0o755); err != nil {
				t.Fatal(err)
			}
			return []wiregen.Option{wiregen.WithFilenames(wiregen.Filenames{Types: "gen/types.gen.ts", Decoders: "Gen/types.gen.ts"})}
		}, "the types and decoders files"},
		{"a hard link to an existing file", func(t *testing.T, dir string) []wiregen.Option {
			writeFile(t, filepath.Join(dir, "types.gen.ts"), "old")
			if err := os.Link(filepath.Join(dir, "types.gen.ts"), filepath.Join(dir, "link.ts")); err != nil {
				t.Fatal(err)
			}
			return []wiregen.Option{wiregen.WithFilenames(wiregen.Filenames{Decoders: "link.ts"})}
		}, "the types and decoders files"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			r := newTestRegistry(c.opts(t, dir)...)
			before := tree(t, dir)
			r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
			r.OpenAPI = wiregen.OpenAPIInfo{Title: "t", Version: "1"}
			err := r.Generate(t.Context(), append(r.opts, wiregen.WithTSDir(dir))...)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Generate = %v, want a refusal naming %s", err, c.want)
			}
			if after := tree(t, dir); !maps.Equal(before, after) {
				t.Errorf("a refused Generate changed the directory from %v to %v", before, after)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tree maps every path below dir to its content, or to "dir" or "link".
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			got[rel] = "link"
		case d.IsDir():
			got[rel] = "dir"
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			got[rel] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Every generated module shares one namespace with the others, which it
// imports, so two generated names alike are refused instead of emitting TS
// that does not compile or shadows a binding.
func TestGenerate_RefusesIdentifierCollisions(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *testRegistry)
		want  string
	}{
		{"two types renamed alike", func(r *testRegistry) {
			r.Types = append(r.Types, wiregen.TypeRef[basic.User]())
			r.TSNameOverride = map[string]string{"User": "Address"}
		}, "type Address and type User both emit the TS identifier Address"},
		{"an enum renamed onto a type", func(r *testRegistry) {
			r.Enums = []wiregen.EnumRef{wiregen.TextEnum[enums.Level]()}
			r.EnumTSName = map[string]string{"Level": "Address"}
		}, "both emit the TS identifier Address"},
		{"a type renamed onto a decoder", func(r *testRegistry) {
			r.Types = append(r.Types, wiregen.TypeRef[basic.User]())
			r.TSNameOverride = map[string]string{"User": "decodeAddress"}
		}, "both emit the TS identifier decodeAddress"},
		{"a type renamed onto a validators export", func(r *testRegistry) {
			r.TSNameOverride = map[string]string{"Address": "asObject"}
		}, "the validators module and type Address"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newTestRegistry()
			r.Types = []wiregen.WireType{wiregen.TypeRef[basic.Address]()}
			c.setup(r)
			err := generateErr(t, r)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Generate = %v, want a refusal containing %q", err, c.want)
			}
		})
	}
}
