package wiregen

// The marshal oracle checks the engine's resolved field model against
// encoding/json's actual output — the ground truth the generated TypeScript
// must faithfully match. For each fixture type it marshals a zero-value and a
// reflection-populated instance and asserts, per field:
//
//	I1 (no phantom keys)   every marshaled key is a resolved field, so the
//	                       generated interface covers everything on the wire;
//	I2 (required present)  every field the engine resolves as required is
//	                       present in both marshals, so a required interface
//	                       member / decoder lookup can never miss;
//	I3 (null implies opt)  a field encoding/json CAN emit as null (nil
//	                       pointer/slice/map) is resolved Optional or is a
//	                       raw/unknown pass-through, matching the decoders'
//	                       null-as-absent contract (required slices/maps,
//	                       which marshal null when nil, are the exception the
//	                       emitters null-coalesce — asserted separately);
//	I4 (shape agreement)   each present non-null value's JSON kind matches
//	                       the resolved TS shape (string/number/boolean/
//	                       array/object).
//
// This converts the hand-mirrored encoding/json field-selection rules
// (promotion, tags, omitempty, special types) from carefully-mirrored to
// mechanically checked, without any mechanism change.

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/cplieger/wiregen/v2/testdata/basic"
	"github.com/cplieger/wiregen/v2/testdata/edges"
)

// oracleSpecimens are the fixture types the oracle registers and checks. Each
// entry is a pointer to a zero value; the reflect side derives both the
// WireType registration and the populated instance from it.
var oracleSpecimens = []any{
	// basic
	&basic.Address{},
	&basic.User{},
	&basic.Notification{},
	&basic.HasUnexported{},
	&basic.HasBytes{},
	&basic.HasOmitzero{},
	&basic.HasJSONString{},
	&basic.HasCustomMapped{},
	&basic.HasTime{},
	&basic.HasRaw{},
	&basic.HasMap{},
	&basic.HasInterface{},
	&basic.Base{},
	&basic.WithEmbedding{},
	// edges
	&edges.TreeNode{},
	&edges.CycleA{},
	&edges.CycleB{},
	&edges.SelfSlice{},
	&edges.SelfMap{},
	&edges.DashComma{},
	&edges.FieldNamedType{},
	&edges.ReservedFields{},
	&edges.DeepA{},
	&edges.DeepB{},
	&edges.DeepC{},
	&edges.Inner{},
	&edges.PtrSlicePtr{},
	&edges.AllKinds{},
	&edges.SliceOfSlice{},
	&edges.MapOfSlice{},
	&edges.SliceOfMap{},
	&edges.EmptyStruct{},
	&edges.AllOptional{},
	&edges.Ambiguous{},
	&edges.Diamond{},
	&edges.DirectWins{},
	&edges.MapVal{},
	&edges.MapOfStructs{},
	&edges.NestedOptPtr{},
	&edges.HasOptEnum{},
}

func TestMarshalOracle(t *testing.T) {
	r := NewRegistry(WithValidatorsImport("./v.js"))
	for _, s := range oracleSpecimens {
		st := reflect.TypeOf(s).Elem()
		r.Types = append(r.Types, WireType{PkgPath: st.PkgPath(), Name: st.Name()})
	}
	r.init()
	engine, err := newASTEngine(r)
	if err != nil {
		t.Fatalf("newASTEngine: %v", err)
	}

	for _, s := range oracleSpecimens {
		st := reflect.TypeOf(s).Elem()
		t.Run(st.Name(), func(t *testing.T) {
			ti := engine.byName[st.Name()]
			if ti == nil {
				t.Fatalf("engine resolved no typeInfo for %s", st.Name())
			}
			fields := make(map[string]*fieldInfo, len(ti.Fields))
			for i := range ti.Fields {
				fields[ti.Fields[i].WireName] = &ti.Fields[i]
			}

			zeroJSON := marshalToMap(t, s)
			filled := reflect.New(st)
			fillValue(filled.Elem(), 4)
			fullJSON := marshalToMap(t, filled.Interface())

			for _, m := range []struct {
				name string
				got  map[string]any
			}{{"zero", zeroJSON}, {"populated", fullJSON}} {
				for key := range m.got {
					if _, ok := fields[key]; !ok {
						t.Errorf("I1: %s marshal emits key %q that the engine did not resolve", m.name, key)
					}
				}
			}
			for wn, fi := range fields {
				if !fi.Optional {
					if _, ok := zeroJSON[wn]; !ok {
						t.Errorf("I2: required field %q absent from zero-value marshal (engine should resolve it Optional)", wn)
					}
					if _, ok := fullJSON[wn]; !ok {
						t.Errorf("I2: required field %q absent from populated marshal", wn)
					}
				}
			}
			for wn, fi := range fields {
				for _, m := range []struct {
					name string
					got  map[string]any
				}{{"zero", zeroJSON}, {"populated", fullJSON}} {
					v, ok := m.got[wn]
					if !ok {
						continue
					}
					if v == nil {
						// Null must be decodable: optional fields skip it,
						// required slices/maps coalesce it to empty, and
						// raw/iface/unknown pass it through as data.
						nullOK := fi.Optional || fi.IsRaw || fi.IsIface ||
							fi.TSType == tsUnknown || fi.IsSlice || fi.IsMap ||
							fi.IsBytes
						if !nullOK {
							t.Errorf("I3: field %q marshals null in %s instance but is required non-collection — the generated decoder would reject encoding/json's own output", wn, m.name)
						}
						continue
					}
					checkJSONShape(t, m.name, wn, fi, v)
				}
			}
		})
	}
}

// checkJSONShape asserts one present non-null marshaled value has the JSON
// kind the resolved field shape promises the TS consumer.
func checkJSONShape(t *testing.T, instance, wn string, fi *fieldInfo, v any) {
	t.Helper()
	kind := func(want string, ok bool) {
		if !ok {
			t.Errorf("I4: field %q (%s instance) resolved as %s but marshals as %T (%v)", wn, instance, want, v, v)
		}
	}
	switch {
	case fi.IsRaw || fi.IsIface || fi.TSType == tsUnknown:
		// pass-through: any JSON value is fine
	case fi.IsSlice:
		_, ok := v.([]any)
		kind("array ("+fi.TSType+")", ok)
	case fi.IsMap, fi.IsStruct:
		_, ok := v.(map[string]any)
		kind("object ("+fi.TSType+")", ok)
	case fi.JSONString || fi.TSType == tsString:
		_, ok := v.(string)
		kind("string", ok)
	case fi.TSType == tsNumber:
		_, ok := v.(float64)
		kind("number", ok)
	case fi.TSType == tsBoolean:
		_, ok := v.(bool)
		kind("boolean", ok)
	default:
		// Named enum TS types (typed strings) marshal as strings.
		_, ok := v.(string)
		kind("string enum ("+fi.TSType+")", ok)
	}
}

// marshalToMap round-trips v through encoding/json into a generic map — the
// exact representation a TS client's JSON.parse hands the generated decoders.
func marshalToMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%T): %v", v, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal(%T): %v", v, err)
	}
	return m
}

// fillValue populates v with deterministic non-zero data so a marshal of the
// result emits every emittable field. depth bounds recursive expansion
// (cyclic fixtures like TreeNode/CycleA stop at nil pointers, which marshal
// per their omitempty/optionality like any other nil).
func fillValue(v reflect.Value, depth int) {
	if depth <= 0 || !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		fillValue(v.Elem(), depth-1)
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Unix(1700000000, 0).UTC()))
			return
		}
		for _, fv := range v.Fields() {
			fillValue(fv, depth-1)
		}
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Slice:
		if v.Type() == reflect.TypeFor[json.RawMessage]() {
			v.Set(reflect.ValueOf(json.RawMessage(`{"k":1}`)))
			return
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			v.SetBytes([]byte("xy"))
			return
		}
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillValue(s.Index(0), depth-1)
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		key := reflect.New(v.Type().Key()).Elem()
		fillValue(key, depth-1)
		val := reflect.New(v.Type().Elem()).Elem()
		fillValue(val, depth-1)
		m.SetMapIndex(key, val)
		v.Set(m)
	case reflect.Interface:
		v.Set(reflect.ValueOf("x"))
	default:
		// remaining kinds (chan/func/complex/...) never appear in wire fixtures
	}
}
