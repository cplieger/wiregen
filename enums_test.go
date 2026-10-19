package wiregen_test

import (
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/enums"
	"github.com/cplieger/wiregen/v4/testdata/enums/a"
	"github.com/cplieger/wiregen/v4/testdata/enums/b"
	"github.com/cplieger/wiregen/v4/testdata/enums/owner"
)

const enumsPkg = "github.com/cplieger/wiregen/v4/testdata/enums"

func enumsReg(refs []wiregen.EnumRef, types ...wiregen.WireType) *testRegistry {
	r := newTestRegistry()
	r.Types = types
	r.Enums = refs
	return r
}

func TestRequiredEnumWithUnnamedZero(t *testing.T) {
	r := enumsReg([]wiregen.EnumRef{wiregen.TextEnum[enums.Level]()}, wiregen.TypeRef[enums.Job]())
	types := mustGen(t, r.typesFile)
	for _, want := range []string{`export type Level = "low" | "high";`, "level: Level;"} {
		if !strings.Contains(types, want) {
			t.Errorf("types missing %q:\n%s", want, types)
		}
	}
	dec := mustGen(t, r.decodersFile)
	for _, want := range []string{`const LEVELS = ["low", "high"] as const;`, `level: reqOneOf(o, "level", LEVELS, "$.job"),`} {
		if !strings.Contains(dec, want) {
			t.Errorf("decoders missing %q:\n%s", want, dec)
		}
	}
	r.OpenAPI = wiregen.OpenAPIInfo{Title: "t", Version: "0"}
	doc := mustGen(t, r.openapiFile)
	if got := mustJSON(t, component(t, doc, "Level")); got != `{"enum":["low","high"],"type":"string"}` {
		t.Errorf("Level component = %s, want exactly the named members", got)
	}
	if got := prop(t, component(t, doc, "Job"), "level"); got != `{"$ref":"#/components/schemas/Level"}` {
		t.Errorf("Job.level = %s", got)
	}
}

func TestEnumsByType(t *testing.T) {
	t.Run("two packages' same-named enums are refused", func(t *testing.T) {
		r := enumsReg([]wiregen.EnumRef{wiregen.StringEnum[a.Outcome](), wiregen.StringEnum[b.Outcome]()})
		err := generateErr(t, r)
		if err == nil {
			t.Fatal("Generate accepted two enums named Outcome")
		}
		for _, pkg := range []string{enumsPkg + "/a", enumsPkg + "/b"} {
			if !strings.Contains(err.Error(), pkg) {
				t.Errorf("error does not name %s: %v", pkg, err)
			}
		}
	})
	t.Run("an enum in a package with no registered type is discovered", func(t *testing.T) {
		r := enumsReg([]wiregen.EnumRef{wiregen.StringEnum[owner.Role]()}, wiregen.TypeRef[enums.Member]())
		out := mustGen(t, r.typesFile)
		for _, want := range []string{`export type Role = "admin" | "user";`, "role: Role;"} {
			if !strings.Contains(out, want) {
				t.Errorf("types missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("an unregistered enum type is not an enum", func(t *testing.T) {
		out := mustGen(t, enumsReg(nil, wiregen.TypeRef[enums.Job]()).typesFile)
		if !strings.Contains(out, "level: string;") || strings.Contains(out, "export type Level") {
			t.Errorf("an unregistered TextMarshaler should be a plain string:\n%s", out)
		}
	})
	t.Run("a same-named type from another package is not the registered one", func(t *testing.T) {
		r := enumsReg([]wiregen.EnumRef{wiregen.StringEnum[a.Outcome]()}, wiregen.TypeRef[a.Thing](), wiregen.TypeRef[b.Report]())
		err := generateErr(t, r)
		want := enumsPkg + "/b.Report.Thing: struct " + enumsPkg + "/b.Thing is neither registered nor mapped"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
		if err != nil && strings.Contains(err.Error(), "Report.Outcome") {
			t.Errorf("b.Outcome refused instead of resolving to a string: %v", err)
		}
		ok := enumsReg([]wiregen.EnumRef{wiregen.StringEnum[a.Outcome]()}, wiregen.TypeRef[b.Thing](), wiregen.TypeRef[b.Report]())
		if out := mustGen(t, ok.typesFile); !strings.Contains(out, "outcome: string;") {
			t.Errorf("b.Report.Outcome took a.Outcome's enum:\n%s", out)
		}
	})
	t.Run("an enum and a type sharing a name are refused", func(t *testing.T) {
		r := enumsReg([]wiregen.EnumRef{wiregen.StringEnum[enums.Empty]()}, wiregen.WireType{PkgPath: enumsPkg + "/a", Name: "Empty"})
		if err := generateErr(t, r); err == nil || !strings.Contains(err.Error(), `"Empty" is registered from two packages`) {
			t.Errorf("err = %v, want the shared name refused", err)
		}
	})
}

func TestEnumRefusals(t *testing.T) {
	cases := []struct {
		name string
		ref  wiregen.EnumRef
		want string
	}{
		{"no constants", wiregen.StringEnum[enums.Empty](), "enums.Empty: declares no constant with a wire name"},
		{"two constants encoding alike", wiregen.TextEnum[enums.Clash](), `constants ClashOne and ClashTwo both encode as "same"`},
		{"an integer enum without Wire", wiregen.EnumRef{Type: wiregen.TypeRef[enums.Level]()}, "needs Wire"},
		{"a string enum with Wire", wiregen.EnumRef{Type: wiregen.TypeRef[owner.Role](), Wire: func(uint64) (string, error) { return "x", nil }}, "Wire is for an integer enum"},
		{"a struct as an enum", wiregen.EnumRef{Type: wiregen.TypeRef[enums.Job]()}, "must be a string or an integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := generateErr(t, enumsReg([]wiregen.EnumRef{tc.ref}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	t.Run("every failing enum is reported", func(t *testing.T) {
		err := generateErr(t, enumsReg([]wiregen.EnumRef{wiregen.StringEnum[enums.Empty](), wiregen.TextEnum[enums.Clash]()}))
		if err == nil || !strings.Contains(err.Error(), "enums.Empty") || !strings.Contains(err.Error(), "enums.Clash") {
			t.Errorf("err = %v, want both enums named", err)
		}
	})
}

func TestTextEnumWire(t *testing.T) {
	wire := wiregen.TextEnum[enums.Level]().Wire
	for _, tc := range []struct {
		value  uint64
		want   string
		wantOK bool
	}{{1, "low", true}, {2, "high", true}, {0, "", false}, {257, "", false}} {
		got, err := wire(tc.value)
		if (err == nil) != tc.wantOK || got != tc.want {
			t.Errorf("Wire(%d) = %q, %v; want %q, ok %v", tc.value, got, err, tc.want, tc.wantOK)
		}
	}
}
