package wiregen_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/openapi"
)

const openapiPkg = "github.com/cplieger/wiregen/v4/testdata/openapi"

// boundsReg registers Create, whose fields carry every bound method.
func boundsReg() *testRegistry {
	r := newTestRegistry()
	r.Types = []wiregen.WireType{wiregen.TypeRef[openapi.Create](), wiregen.TypeRef[openapi.Item](), wiregen.TypeRef[openapi.Shared]()}
	r.Enums = []wiregen.EnumRef{wiregen.TextEnum[openapi.Level]()}
	r.OpenAPI = wiregen.OpenAPIInfo{Title: "t", Version: "0"}
	return r
}

// component returns the named component schema of doc.
func component(t *testing.T, doc, name string) map[string]any {
	t.Helper()
	var d struct {
		Components struct {
			Schemas map[string]map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatal(err)
	}
	s, ok := d.Components.Schemas[name]
	if !ok {
		t.Fatalf("no component %s in:\n%s", name, doc)
	}
	return s
}

// prop returns property name of an object schema, as JSON.
func prop(t *testing.T, schema map[string]any, name string) string {
	t.Helper()
	p, ok := schema["properties"].(map[string]any)[name]
	if !ok {
		t.Fatalf("no property %s in %v", name, schema)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBoundsByMethod(t *testing.T) {
	r := boundsReg()
	doc := mustGen(t, r.openapiFile)
	create := component(t, doc, "Create")
	t.Run("schema keywords", func(t *testing.T) {
		for name, want := range map[string]string{
			"limit": `{"maximum":500,"minimum":1,"type":"integer"}`,
			"files": `{"items":{"maxLength":510,"pattern":"^[^/]*$","type":"string","x-max-bytes":510},"maxItems":3,"minItems":1,"type":"array"}`,
		} {
			if got := prop(t, create, name); got != want {
				t.Errorf("%s schema = %s, want %s", name, got, want)
			}
		}
	})
	t.Run("arbitraries draw within the bounds", func(t *testing.T) {
		out := mustGen(t, r.arbitrariesFile)
		for _, want := range []string{
			`limit: fc.integer({ min: 1, max: 500 }),`,
			`files: fc.array(fc.stringMatching(new globalThis.RegExp("^[^/]*$", "u")).filter((s) => new globalThis.TextEncoder().encode(s).length <= 510), { minLength: 1, maxLength: 2, size: "small", depthIdentifier: "wiregen" }),`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("arbitraries missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("validator", func(t *testing.T) {
		body := func(files ...string) string {
			if files == nil {
				files = []string{}
			}
			b, _ := json.Marshal(map[string]any{"item": nil, "labels": nil, "files": files, "shared": map[string]any{}, "limit": 1, "level": "low"})
			return string(b)
		}
		cases := []struct {
			name, body, want string
		}{
			{"255 two-byte runes", body(strings.Repeat("é", 255)), "ok"},
			{"256 two-byte runes", body(strings.Repeat("é", 256)), "expected at most 510 UTF-8 bytes, got 512"},
			{"510 ASCII bytes", body(strings.Repeat("a", 510)), "ok"},
			{"511 ASCII bytes", body(strings.Repeat("a", 511)), "got 511"},
			{"pattern", body("a/b"), "expected a match of ^[^/]*$"},
			{"no files", body(), "expected 1 to 3 items, got 0"},
			{"null files", strings.Replace(body("a"), `"files":["a"]`, `"files":null`, 1), "$.create.files: expected array, got null"},
			{"four files", body("a", "b", "c", "d"), "expected 1 to 3 items, got 4"},
			{"limit over", strings.Replace(body("a"), `"limit":1`, `"limit":501`, 1), "expected an integer from 1 to 500, got 501"},
			{"limit fraction", strings.Replace(body("a"), `"limit":1`, `"limit":1.5`, 1), "expected an integer from 1 to 500"},
		}
		bodies := make([]string, 0, len(cases))
		for _, c := range cases {
			bodies = append(bodies, c.body)
		}
		got := decodeVerdicts(t, r, "decodeCreate", bodies)
		for i, c := range cases {
			if ok := c.want == "ok"; ok && got[i] != "ok" || !ok && !strings.Contains(got[i], c.want) {
				t.Errorf("%s: verdict %q, want %q", c.name, got[i], c.want)
			}
		}
	})
}

func TestBoundRefusals(t *testing.T) {
	r := newTestRegistry()
	r.Types = []wiregen.WireType{wiregen.TypeRef[openapi.Misbound]()}
	_, err := r.typesFile(t.Context())
	if err == nil {
		t.Fatal("Generate accepted unreadable or misplaced bounds")
	}
	for _, want := range []string{
		openapiPkg + ".Misbound.A: " + openapiPkg + ".BadRange declares Range, which bounds an integer type",
		openapiPkg + ".Misbound.B: " + openapiPkg + ".PtrBound has a pointer-receiver MaxBytes",
		openapiPkg + ".Misbound.C: " + openapiPkg + ".Computed MaxBytes must be one return of constants",
		openapiPkg + ".Misbound.D: " + openapiPkg + ".EmptyRange Range 5 to 1 is empty",
		openapiPkg + ".Misbound.E: " + openapiPkg + ".RE2Only Pattern is not a Go regexp",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not contain %q:\n%v", want, err)
		}
	}
}
