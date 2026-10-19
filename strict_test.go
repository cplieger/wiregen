package wiregen_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/strict"
)

const strictPkg = "github.com/cplieger/wiregen/v4/testdata/strict"

func strictReg(opts []wiregen.Option, types ...wiregen.WireType) *testRegistry {
	r := newTestRegistry(opts...)
	r.Types = types
	return r
}

// generateErr runs Generate into a fresh directory and returns its error,
// failing the test when Generate wrote anything despite failing.
func generateErr(t *testing.T, r *testRegistry) error {
	t.Helper()
	dir := t.TempDir()
	err := r.generate(t.Context(), dir)
	if entries, _ := os.ReadDir(dir); err != nil && len(entries) > 0 {
		t.Errorf("a refused Generate wrote %d entries under %s", len(entries), dir)
	}
	return err
}

func wantRefusals(t *testing.T, err error, paths ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Generate succeeded; want refusals of %v", paths)
	}
	for _, p := range paths {
		if !strings.Contains(err.Error(), strictPkg+"."+p+": ") {
			t.Errorf("error does not name %s:\n%v", p, err)
		}
	}
}

func TestStrictRefusesDuration(t *testing.T) {
	err := generateErr(t, strictReg(nil, wiregen.TypeRef[strict.HasDuration]()))
	wantRefusals(t, err, "HasDuration.Timeout")
	if err != nil && !strings.Contains(err.Error(), "milliseconds") {
		t.Errorf("refusal does not name the int64 milliseconds remedy:\n%v", err)
	}
}

func TestStrictRefusesUnknown(t *testing.T) {
	t.Run("one call reports every unknown shape", func(t *testing.T) {
		err := generateErr(t, strictReg(nil, wiregen.TypeRef[strict.Unknowns]()))
		wantRefusals(t, err, "Unknowns.Any", "Unknowns.Iface", "Unknowns.Inline", "Unknowns.Unresolved")
		if err != nil && !strings.Contains(err.Error(), "4 field(s)") {
			t.Errorf("want a count of 4:\n%v", err)
		}
	})
	t.Run("every other unencodable shape is refused", func(t *testing.T) {
		err := generateErr(t, strictReg(nil, wiregen.TypeRef[strict.Exotic]()))
		wantRefusals(t, err, "Exotic.Base.Timeout", "Exotic.Items", "Exotic.ByFloat", "Exotic.Ch",
			"Exotic.Fn", "Exotic.Cplx", "Exotic.Fixed", "Exotic.Custom", "Exotic.Text", "Exotic.Nested")
		if err != nil && strings.Contains(err.Error(), "Exotic.Skipped") {
			t.Errorf("a json:\"-\" field is refused:\n%v", err)
		}
	})
	t.Run("raw messages pass and a text marshaler is a string", func(t *testing.T) {
		out := mustGen(t, strictReg(nil, wiregen.TypeRef[strict.Opaque]()).typesFile)
		for _, want := range []string{"raw: unknown;", "value: unknown;", "level: string;", "by_level: { [key: string]: number };"} {
			if !strings.Contains(out, want) {
				t.Errorf("types missing %q:\n%s", want, out)
			}
		}
	})
}

func TestUntaggedFieldByOption(t *testing.T) {
	t.Run("without the option the Go name is the wire name", func(t *testing.T) {
		r := strictReg(nil, wiregen.TypeRef[strict.T]())
		if types := mustGen(t, r.typesFile); !strings.Contains(types, "Value: string;") {
			t.Errorf("types missing Value:\n%s", types)
		}
		if dec := mustGen(t, r.decodersFile); !strings.Contains(dec, `reqStr(o, "Value", "$.t")`) {
			t.Errorf("decoder does not read Value:\n%s", dec)
		}
	})
	t.Run("the option refuses an untagged field", func(t *testing.T) {
		r := strictReg([]wiregen.Option{wiregen.WithRequireJSONTags()}, wiregen.TypeRef[strict.T]())
		wantRefusals(t, generateErr(t, r), "T.Value")
	})
	t.Run("the option passes a tagged field", func(t *testing.T) {
		r := strictReg([]wiregen.Option{wiregen.WithRequireJSONTags()}, wiregen.TypeRef[strict.Tagged]())
		if err := generateErr(t, r); err != nil {
			t.Errorf("Generate: %v", err)
		}
	})
}

// encoding/json writes a type with a marshaler through it, so a registered
// type with one is refused, and a method with the wrong signature is no
// marshaler.
func TestStrictMarshalers(t *testing.T) {
	t.Run("a registered type with a marshaler is refused", func(t *testing.T) {
		err := generateErr(t, strictReg(nil, wiregen.TypeRef[strict.HoldsMarshalers](),
			wiregen.TypeRef[strict.JSONObj](), wiregen.TypeRef[strict.TextObj](), wiregen.TypeRef[strict.Stamped]()))
		wantRefusals(t, err, "HoldsMarshalers.O", "HoldsMarshalers.T")
		for _, want := range []string{
			strictPkg + ".JSONObj: is registered, but implements json.Marshaler",
			strictPkg + ".TextObj: is registered, but implements encoding.TextMarshaler",
			strictPkg + ".Stamped: is registered, but implements json.Marshaler",
		} {
			if err != nil && !strings.Contains(err.Error(), want) {
				t.Errorf("error lacks %q:\n%v", want, err)
			}
		}
	})
	t.Run("a wrong-signature MarshalText is no marshaler", func(t *testing.T) {
		err := generateErr(t, strictReg(nil, wiregen.TypeRef[strict.Codecs]()))
		wantRefusals(t, err, "Codecs.Item", "Codecs.Raw", "Codecs.ByKey")
		if err != nil && (strings.Contains(err.Error(), "Codecs.ID") || strings.Contains(err.Error(), "Codecs.Name")) {
			t.Errorf("a text-marshaled array or a plain string is refused:\n%v", err)
		}
	})
	t.Run("a text-marshaled array and a plain string pass", func(t *testing.T) {
		out := mustGen(t, strictReg(nil, wiregen.TypeRef[strict.TextArray]()).typesFile)
		for _, want := range []string{"id: string;", "name: string;"} {
			if !strings.Contains(out, want) {
				t.Errorf("types lack %q:\n%s", want, out)
			}
		}
		var got map[string]any
		if err := json.Unmarshal(must(json.Marshal(strict.TextArray{Name: "n"})), &got); err != nil {
			t.Fatal(err)
		}
		if _, ok := got["id"].(string); !ok || got["name"] != "n" {
			t.Errorf("encoding/json wrote %v, want both as strings", got)
		}
	})
}

// The ,string option applies where encoding/json applies it, a boolean,
// number or string under at most one pointer with no marshaler, and is
// refused elsewhere, where encoding/json ignores it.
func TestStrictQuoted(t *testing.T) {
	r := strictReg(nil, wiregen.TypeRef[strict.Quoted]())
	r.Mappings = map[string]wiregen.Mapping{strictPkg + ".Millis": {TS: "number", Decoder: "reqNum"}}
	err := generateErr(t, r)
	wantRefusals(t, err, "Quoted.Values", "Quoted.PP", "Quoted.Lvl", "Quoted.M", "Quoted.D")
	if err != nil {
		for _, ok := range []string{"Quoted.N:", "Quoted.P:", "Quoted.F:"} {
			if strings.Contains(err.Error(), ok) {
				t.Errorf("%s is refused, but encoding/json quotes it:\n%v", ok, err)
			}
		}
	}
}

// A tag name every encoding/json reads alike names the field, punctuation
// included; one the versions read differently is refused.
func TestTagNames(t *testing.T) {
	out := mustGen(t, strictReg(nil, wiregen.TypeRef[strict.Named]()).typesFile)
	for _, want := range []string{`"a-b.c:d": string;`, `"a b": string;`, "plain: string;"} {
		if !strings.Contains(out, want) {
			t.Errorf("types lack %s:\n%s", want, out)
		}
	}
	var got map[string]any
	if err := json.Unmarshal(must(json.Marshal(strict.Named{})), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a-b.c:d", "a b", "plain"} {
		if _, ok := got[key]; !ok {
			t.Errorf("encoding/json writes no key %q: %v", key, got)
		}
	}
	err := generateErr(t, strictReg(nil, wiregen.TypeRef[strict.BadNames]()))
	wantRefusals(t, err, "BadNames.Escaped", "BadNames.Quote", "BadNames.Euro")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// An enum's members are what encoding/json writes only when no other
// marshaler takes over: a string enum has none, an integer enum only its
// MarshalText.
func TestStrictEnumCodecs(t *testing.T) {
	for name, ref := range map[string]wiregen.EnumRef{
		"a string enum with MarshalText":   wiregen.StringEnum[strict.Loud](),
		"an integer enum with MarshalJSON": wiregen.TextEnum[strict.Coded](),
	} {
		t.Run(name, func(t *testing.T) {
			r := strictReg(nil, wiregen.TypeRef[strict.Tagged]())
			r.Enums = []wiregen.EnumRef{ref}
			err := generateErr(t, r)
			if err == nil || !strings.Contains(err.Error(), "but it implements") {
				t.Errorf("Generate = %v, want the enum refused for its marshaler", err)
			}
		})
	}
}
