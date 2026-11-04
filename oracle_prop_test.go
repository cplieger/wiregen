package wiregen_test

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/cplieger/wiregen/v4"
	"github.com/cplieger/wiregen/v4/testdata/oracle"
)

// The oracle is encoding/json itself: for values drawn over the fixture
// types, what it writes must decode through the generated decoder to the
// same value and validate against the emitted schema of its direction, and a
// request body it writes must decode back into the Go type unchanged.

const oraclePkg = "github.com/cplieger/wiregen/v4/testdata/oracle"

var oracleMappings = map[string]wiregen.Mapping{
	oraclePkg + ".Stamp":  {TS: "number", Decoder: "reqNum", Schema: map[string]any{"type": "integer"}},
	oraclePkg + ".Millis": {TS: "number", Decoder: "reqNum", Schema: map[string]any{"type": "integer"}},
}

// oracleCase is one registry the oracle runs: its response and request body
// types, each drawn from by the value generator.
type oracleCase struct {
	reg      *testRegistry
	response []reflect.Type
	request  []reflect.Type
}

func newOracleCase(response, request []reflect.Type, more ...wiregen.WireType) oracleCase {
	r := newTestRegistry(wiregen.WithTransportImport("./api-client.js"))
	r.Types = more
	r.Enums = []wiregen.EnumRef{wiregen.StringEnum[oracle.Color](), wiregen.TextEnum[oracle.Level]()}
	r.Mappings = oracleMappings
	r.OpenAPI = wiregen.OpenAPIInfo{Title: "oracle", Version: "0"}
	for i, t := range response {
		r.Endpoints = append(r.Endpoints, wiregen.Endpoint{
			Name: fmt.Sprintf("get%d", i), Method: "GET", Path: fmt.Sprintf("/r/%d", i),
			Kind: wiregen.KindJSON, Response: wiregen.TypeOf(t),
		})
	}
	for i, t := range request {
		r.Endpoints = append(r.Endpoints, wiregen.Endpoint{
			Name: fmt.Sprintf("put%d", i), Method: "PUT", Path: fmt.Sprintf("/q/%d", i),
			Kind: wiregen.KindJSON, Request: wiregen.TypeOf(t),
		})
	}
	return oracleCase{reg: r, response: response, request: request}
}

func fixtureCase() oracleCase {
	return newOracleCase(
		[]reflect.Type{reflect.TypeFor[oracle.Resp](), reflect.TypeFor[oracle.Inner]()},
		[]reflect.Type{reflect.TypeFor[oracle.Req]()},
	)
}

type oracleRun struct {
	c    oracleCase
	repl *nodeREPL
	doc  map[string]any
	raw  string
}

func startOracle(tb testing.TB, c oracleCase) (*oracleRun, error) {
	tb.Helper()
	raw, err := c.reg.openapiFile(tb.Context())
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		tb.Fatal(err)
	}
	repl := startREPL(tb, c.reg, `import * as D from "./decoders.gen.ts";`, `
  const dec = (D as unknown as Record<string, (v: unknown) => unknown>)[req.decoder];
  const parsed: unknown = JSON.parse(req.body);
  try {
    return JSON.stringify({ parsed: JSON.stringify(parsed), decoded: JSON.stringify(dec(parsed)) });
  } catch (e) {
    return JSON.stringify({ error: e instanceof Error ? e.message : String(e) });
  }`)
	return &oracleRun{c: c, repl: repl, doc: doc, raw: raw}, nil
}

// component returns the component schema of t in a direction: the request
// form is the Input component when the document has one.
func (o *oracleRun) component(t reflect.Type, request bool) string {
	schemas := o.doc["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := schemas[t.Name()+"Input"]; ok && request {
		return t.Name() + "Input"
	}
	return t.Name()
}

// check draws n values of each body type from rng and returns the ways the
// generated code disagrees with encoding/json about them, the first few of
// each kind.
func (o *oracleRun) check(tb testing.TB, rng *rand.Rand, n int) []string {
	tb.Helper()
	fails := o.checkAll(tb, rng, n)
	seen := map[string]int{}
	return slices.DeleteFunc(fails, func(f string) bool {
		kind, _, _ := strings.Cut(f, " encoding/json")
		seen[kind]++
		return seen[kind] > 3
	})
}

func (o *oracleRun) checkAll(tb testing.TB, rng *rand.Rand, n int) []string {
	tb.Helper()
	gen := newValueGen(rng)
	var fails []string
	for _, t := range o.c.response {
		schema := compiled(tb, o.raw, "#/components/schemas/"+o.component(t, false))
		at := o.schemaOf(tb, o.component(t, false))
		for range n {
			body := marshalDrawn(tb, gen, t)
			fails = append(fails, o.checkDecode(tb, t, at, body)...)
			if err := validates(tb, schema, body); err != nil {
				fails = append(fails, fmt.Sprintf("%s: the response schema refuses encoding/json's %s: %v", t.Name(), body, err))
			}
		}
	}
	for _, t := range o.c.request {
		schema := compiled(tb, o.raw, "#/components/schemas/"+o.component(t, true))
		at := o.schemaOf(tb, o.component(t, true))
		for range n {
			body := marshalDrawn(tb, gen, t)
			if err := validates(tb, schema, body); err != nil {
				fails = append(fails, fmt.Sprintf("%s: the request schema refuses encoding/json's %s: %v", t.Name(), body, err))
			}
			back := reflect.New(t)
			if err := json.Unmarshal([]byte(body), back.Interface()); err != nil {
				fails = append(fails, fmt.Sprintf("%s: encoding/json cannot decode the request body %s: %v", t.Name(), body, err))
				continue
			}
			again := mustMarshal(tb, back.Interface())
			if diff := decodedDiff("$", at, parseJSON(tb, body), parseJSON(tb, again)); diff != "" {
				fails = append(fails, fmt.Sprintf("%s: the request body %s decodes to a value that encodes as %s: %s", t.Name(), body, again, diff))
			}
		}
	}
	return fails
}

// checkDecode runs t's decoder over body and compares what it returns with
// what JSON.parse read, at schema at.
func (o *oracleRun) checkDecode(tb testing.TB, t reflect.Type, at schemaAt, body string) []string {
	tb.Helper()
	var ans struct {
		Parsed  string `json:"parsed"`
		Decoded string `json:"decoded"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(o.repl.ask(tb, map[string]string{"decoder": "decode" + t.Name(), "body": body})), &ans); err != nil {
		tb.Fatal(err)
	}
	if ans.Error != "" {
		return []string{fmt.Sprintf("%s: the decoder refuses encoding/json's %s: %s", t.Name(), body, ans.Error)}
	}
	if diff := decodedDiff("$", at, parseJSON(tb, ans.Parsed), parseJSON(tb, ans.Decoded)); diff != "" {
		return []string{fmt.Sprintf("%s: the decoder turns encoding/json's %s into %s: %s", t.Name(), body, ans.Decoded, diff)}
	}
	return nil
}

// decodedDiff compares a decoded value with the JSON it came from, at
// schema at, allowing only the readings of null docs/type-mapping.md "Null
// and presence" documents, each where the schema says it happens: [] for an
// array, {} for a map, "" for base64 text, and an absent key where the
// property is not required or only a pointer can write its null. A pointer
// to a collection with no omit option has its collection's schema, so the
// fixtures hold none: the oracle would require its null to read as empty.
func decodedDiff(path string, at schemaAt, want, got any) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return path + ": not an object"
		}
		for k, wv := range w {
			sub, optional := at.property(k)
			gv, present := g[k]
			if !present {
				if wv == nil && (optional || sub.pointerNull()) {
					continue
				}
				return path + "." + k + ": lost"
			}
			if d := decodedDiff(path+"."+k, sub, wv, gv); d != "" {
				return d
			}
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				return path + "." + k + ": not in the JSON"
			}
		}
		return ""
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return path + ": not the same array"
		}
		items := at.items()
		for i := range w {
			if d := decodedDiff(fmt.Sprintf("%s[%d]", path, i), items, w[i], g[i]); d != "" {
				return d
			}
		}
		return ""
	case nil:
		if at.readsNull(got) {
			return ""
		}
	}
	if !reflect.DeepEqual(want, got) {
		return fmt.Sprintf("%s: %v became %v", path, want, got)
	}
	return ""
}

// schemaAt is every schema a value at one place in the document answers
// to, with each $ref, anyOf, oneOf and allOf member expanded.
type schemaAt struct {
	o       *oracleRun
	schemas []map[string]any
}

func (o *oracleRun) schemaOf(tb testing.TB, component string) schemaAt {
	tb.Helper()
	s, ok := o.doc["components"].(map[string]any)["schemas"].(map[string]any)[component]
	if !ok {
		tb.Fatalf("the document has no component %s", component)
	}
	return o.expand(s)
}

func (o *oracleRun) expand(s any) schemaAt {
	at := schemaAt{o: o}
	var walk func(any)
	walk = func(s any) {
		m, ok := s.(map[string]any)
		if !ok {
			return
		}
		if ref, ok := m["$ref"].(string); ok {
			walk(o.resolve(ref))
			return
		}
		at.schemas = append(at.schemas, m)
		for _, kw := range []string{"anyOf", "oneOf", "allOf"} {
			list, _ := m[kw].([]any)
			for _, e := range list {
				walk(e)
			}
		}
	}
	walk(s)
	return at
}

// resolve follows a local $ref, which the generator has already checked.
func (o *oracleRun) resolve(ref string) any {
	var cur any = o.doc
	for tok := range strings.SplitSeq(strings.TrimPrefix(ref, "#/"), "/") {
		cur = cur.(map[string]any)[strings.NewReplacer("~1", "/", "~0", "~").Replace(tok)]
	}
	return cur
}

// property returns the schema of key k and whether a schema declaring it
// leaves it out of required. A key no schema declares is a map entry.
func (a schemaAt) property(k string) (sub schemaAt, optional bool) {
	sub = schemaAt{o: a.o}
	declared := false
	for _, m := range a.schemas {
		props, _ := m["properties"].(map[string]any)
		p, ok := props[k]
		if !ok {
			continue
		}
		declared = true
		sub.schemas = append(sub.schemas, a.o.expand(p).schemas...)
		required, _ := m["required"].([]any)
		optional = optional || !slices.Contains(required, any(k))
	}
	if !declared {
		for _, m := range a.schemas {
			sub.schemas = append(sub.schemas, a.o.expand(m["additionalProperties"]).schemas...)
		}
	}
	return sub, optional
}

func (a schemaAt) items() schemaAt {
	sub := schemaAt{o: a.o}
	for _, m := range a.schemas {
		sub.schemas = append(sub.schemas, a.o.expand(m["items"]).schemas...)
	}
	return sub
}

// readsNull reports whether got is a reading of null the schema allows.
func (a schemaAt) readsNull(got any) bool {
	switch g := got.(type) {
	case nil:
		return true
	case []any:
		return len(g) == 0 && a.hasType("array")
	case map[string]any:
		return len(g) == 0 && a.has(isMap)
	case string:
		return g == "" && a.has(isBase64)
	}
	return false
}

// pointerNull reports whether only a nil pointer writes null here: the
// schema admits null and has no empty reading of it.
func (a schemaAt) pointerNull() bool {
	return a.hasType("null") && !a.hasType("array") && !a.has(isMap) && !a.has(isBase64)
}

func (a schemaAt) has(is func(map[string]any) bool) bool { return slices.ContainsFunc(a.schemas, is) }

func isMap(m map[string]any) bool {
	_, ok := m["additionalProperties"]
	return ok
}

func isBase64(m map[string]any) bool { return m["contentEncoding"] == "base64" }

func (a schemaAt) hasType(name string) bool {
	return slices.ContainsFunc(a.schemas, func(m map[string]any) bool {
		switch t := m["type"].(type) {
		case string:
			return t == name
		case []any:
			return slices.Contains(t, any(name))
		}
		return false
	})
}

func parseJSON(tb testing.TB, s string) any {
	tb.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		tb.Fatal(err)
	}
	return v
}

func marshalDrawn(tb testing.TB, gen *valueGen, t reflect.Type) string {
	tb.Helper()
	v := reflect.New(t)
	gen.fill(v.Elem())
	return mustMarshal(tb, v.Interface())
}

func mustMarshal(tb testing.TB, v any) string {
	tb.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		tb.Fatalf("encoding/json refuses a drawn %T: %v", v, err)
	}
	return string(b)
}

// FuzzOracle_encodingJSONRoundTrip draws values over the fixture types from
// the seed and holds the generated decoders, schemas and request types to
// encoding/json's reading of each.
func FuzzOracle_encodingJSONRoundTrip(f *testing.F) {
	for seed := range uint64(8) {
		f.Add(seed)
	}
	o, err := startOracle(f, fixtureCase())
	if err != nil {
		f.Fatalf("generate: %v", err)
	}
	f.Fuzz(func(t *testing.T, seed uint64) {
		for _, fail := range o.check(t, rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), 8) {
			t.Error(fail)
		}
	})
}

// Whatever Generate accepts holds to encoding/json; what it cannot hold to,
// it refuses naming the field.
func TestOracle_acceptedOrRefusedByField(t *testing.T) {
	cases := map[string]struct {
		c     oracleCase
		field string
	}{
		"embedded pointer to an unexported struct in a request": {
			newOracleCase(nil, []reflect.Type{reflect.TypeFor[oracle.SeedReqEmbedPtr]()}), "SeedReqEmbedPtr.hidden.H",
		},
		"a mapped type under ,string": {
			newOracleCase([]reflect.Type{reflect.TypeFor[oracle.SeedQuotedMapped]()}, nil), "SeedQuotedMapped.M",
		},
		"a text marshaler with no UnmarshalText in a request": {
			newOracleCase(nil, []reflect.Type{reflect.TypeFor[oracle.SeedTextNoUnmarshal]()}), "SeedTextNoUnmarshal.L",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o, err := startOracle(t, tc.c)
			if err != nil {
				if !strings.Contains(err.Error(), oraclePkg+"."+tc.field+":") {
					t.Fatalf("Generate refused without naming %s: %v", tc.field, err)
				}
				return
			}
			for _, fail := range o.check(t, rand.New(rand.NewPCG(1, 2)), 32) {
				t.Error(fail)
			}
		})
	}
}

// The response schema's required and nullable say exactly what encoding/json
// does: a key it never leaves out is required, and a key it never writes as
// null is not nullable.
func TestOracle_presenceIsTight(t *testing.T) {
	o, err := startOracle(t, fixtureCase())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	gen := newValueGen(rand.New(rand.NewPCG(7, 11)))
	for _, typ := range o.c.response {
		absent, null := map[string]bool{}, map[string]bool{}
		comp := o.doc["components"].(map[string]any)["schemas"].(map[string]any)[o.component(typ, false)].(map[string]any)
		props := comp["properties"].(map[string]any)
		for range 400 {
			var m map[string]any
			if err := json.Unmarshal([]byte(marshalDrawn(t, gen, typ)), &m); err != nil {
				t.Fatal(err)
			}
			for k := range props {
				v, ok := m[k]
				absent[k] = absent[k] || !ok
				null[k] = null[k] || (ok && v == nil)
			}
		}
		required := map[string]bool{}
		if list, ok := comp["required"].([]any); ok {
			for _, k := range list {
				required[k.(string)] = true
			}
		}
		for k, p := range props {
			if absent[k] == required[k] {
				t.Errorf("%s.%s: encoding/json left it out: %v, but the schema's required is %v", typ.Name(), k, absent[k], required[k])
			}
			if null[k] != admitsNull(p.(map[string]any)) {
				t.Errorf("%s.%s: encoding/json wrote null: %v, but the schema admits null: %v", typ.Name(), k, null[k], !null[k])
			}
		}
	}
}

// admitsNull reports whether property schema s admits null.
func admitsNull(s map[string]any) bool {
	if types, ok := s["type"].([]any); ok {
		return slices.Contains(types, any("null"))
	}
	if anyOf, ok := s["anyOf"].([]any); ok {
		for _, a := range anyOf {
			if a.(map[string]any)["type"] == "null" {
				return true
			}
		}
		return false
	}
	_, typed := s["type"]
	_, ref := s["$ref"]
	return !typed && !ref
}

// valueGen draws Go values, reaching nil and non-nil at every pointer level,
// nil, empty and filled collections, and the zero of every type whose zero
// encodes.
type valueGen struct {
	r       *rand.Rand
	special map[reflect.Type]func() reflect.Value
}

var timeType = reflect.TypeFor[time.Time]()

func newValueGen(r *rand.Rand) *valueGen {
	g := &valueGen{r: r}
	pick := func(vs ...any) func() reflect.Value {
		return func() reflect.Value { return reflect.ValueOf(vs[r.IntN(len(vs))]) }
	}
	g.special = map[reflect.Type]func() reflect.Value{
		reflect.TypeFor[oracle.Color]():    pick(oracle.Red, oracle.Blue),
		reflect.TypeFor[oracle.Level]():    pick(oracle.Low, oracle.High),
		reflect.TypeFor[oracle.Text]():     func() reflect.Value { return reflect.ValueOf(oracle.NewText(g.str())) },
		reflect.TypeFor[oracle.Stamp]():    func() reflect.Value { return reflect.ValueOf(oracle.NewStamp(g.r.Int64N(1 << 53))) },
		reflect.TypeFor[json.RawMessage](): pick(json.RawMessage(nil), json.RawMessage(`null`), json.RawMessage(`1`), json.RawMessage(`"x"`), json.RawMessage(`[1,{"a":null}]`), json.RawMessage(`{}`)),
		timeType:                           g.time,
	}
	return g
}

func (g *valueGen) str() string {
	pool := []string{"", "a", "é", "__proto__", "constructor", "a\"b\u2028<&>", "t:x", "0"}
	if g.r.IntN(3) == 0 {
		var b strings.Builder
		for range g.r.IntN(6) {
			b.WriteRune(rune(g.r.IntN(0x2FFF)))
		}
		return b.String()
	}
	return pool[g.r.IntN(len(pool))]
}

func (g *valueGen) time() reflect.Value {
	if g.r.IntN(5) == 0 {
		return reflect.ValueOf(time.Time{})
	}
	zones := []*time.Location{time.UTC, time.FixedZone("", 2*3600), time.FixedZone("", -(5*3600 + 1800))}
	t := time.Unix(g.r.Int64N(253402214400)-62135596800+86400, g.r.Int64N(1e9)).In(zones[g.r.IntN(len(zones))])
	return reflect.ValueOf(t)
}

func (g *valueGen) int(bits int, unsigned bool) uint64 {
	switch g.r.IntN(6) {
	case 0:
		return 0
	case 1:
		return 1
	case 2:
		if unsigned {
			return math.MaxUint64 >> (64 - bits)
		}
		return uint64(math.MaxInt64 >> (64 - bits))
	case 3:
		if unsigned {
			return 2
		}
		minInt := int64(math.MinInt64) >> (64 - bits)
		return uint64(minInt)
	}
	return g.r.Uint64() >> (64 - bits)
}

// canZero reports whether t's zero value encodes: no special type, whose
// zero may not, is reachable from it.
func (g *valueGen) canZero(t reflect.Type) bool {
	if _, ok := g.special[t]; ok {
		return false
	}
	if t.Kind() == reflect.Struct {
		for sf := range t.Fields() {
			if !g.canZero(sf.Type) {
				return false
			}
		}
	}
	return true
}

// fill sets v, addressable, to a drawn value of its type.
func (g *valueGen) fill(v reflect.Value) {
	t := v.Type()
	if draw, ok := g.special[t]; ok {
		v.Set(draw())
		return
	}
	switch t.Kind() {
	case reflect.Pointer:
		if g.r.IntN(4) != 0 {
			p := reflect.New(t.Elem())
			g.fill(p.Elem())
			v.Set(p)
		}
	case reflect.Struct:
		if g.canZero(t) && g.r.IntN(5) == 0 {
			return
		}
		for i := range t.NumField() {
			sf := t.Field(i)
			if !sf.IsExported() && !sf.Anonymous {
				continue
			}
			f := v.Field(i)
			if !f.CanSet() {
				f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
			}
			g.fill(f)
		}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 && g.r.IntN(4) == 0 {
			return
		}
		switch g.r.IntN(4) {
		case 0:
		case 1:
			v.Set(reflect.MakeSlice(t, 0, 0))
		default:
			s := reflect.MakeSlice(t, 1+g.r.IntN(3), 3)
			for i := range s.Len() {
				g.fill(s.Index(i))
			}
			v.Set(s)
		}
	case reflect.Map:
		switch g.r.IntN(4) {
		case 0:
		case 1:
			v.Set(reflect.MakeMap(t))
		default:
			m := reflect.MakeMap(t)
			for range 1 + g.r.IntN(3) {
				k, e := reflect.New(t.Key()).Elem(), reflect.New(t.Elem()).Elem()
				g.fill(k)
				g.fill(e)
				m.SetMapIndex(k, e)
			}
			v.Set(m)
		}
	case reflect.String:
		v.SetString(g.str())
	case reflect.Bool:
		v.SetBool(g.r.IntN(2) == 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(g.int(t.Bits(), false)))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(g.int(t.Bits(), true))
	case reflect.Float32, reflect.Float64:
		pool := []float64{0, math.Copysign(0, -1), 1.5, 1e21, 5e-324, -math.MaxFloat64, g.r.NormFloat64() * 1e6}
		v.SetFloat(pool[g.r.IntN(len(pool))])
	default:
		panic("valueGen: no draw for " + t.String())
	}
}
