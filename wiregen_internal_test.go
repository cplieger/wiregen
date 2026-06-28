package wiregen

import (
	"strings"
	"testing"
)

// White-box (package wiregen) unit tests for the unexported name/identifier
// helpers in wiregen.go. Shared helpers and fixtures for the internal test
// files (ast_engine_test.go, emitters_test.go) also live here.

// --- shared internal test helpers ---

const (
	basicPkg    = "github.com/cplieger/wiregen/testdata/basic"
	edgesPkg    = "github.com/cplieger/wiregen/testdata/edges"
	unionsPkg   = "github.com/cplieger/wiregen/testdata/unions"
	crossrefPkg = "github.com/cplieger/wiregen/testdata/crossref"
)

func eqStr(t *testing.T, fn, in, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s(%q) = %q, want %q", fn, in, got, want)
	}
}

func mustContain(t *testing.T, label, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("%s: output is missing %q\n--- output ---\n%s", label, want, out)
	}
}

func mustNotContain(t *testing.T, label, out, bad string) {
	t.Helper()
	if strings.Contains(out, bad) {
		t.Errorf("%s: output unexpectedly contains %q\n--- output ---\n%s", label, bad, out)
	}
}

// markerIface is a named interface used to exercise TypeRef's
// nil-reflect.TypeOf fallback path.
type markerIface interface{ marker() }

// TestPathName_snakeCase pins (*Registry).pathName's CamelCase/acronym →
// snake_case conversion used for decoder path segments. Each row isolates one
// case-boundary or acronym-boundary decision so the exact snake-casing is
// observable.
func TestPathName_snakeCase(t *testing.T) {
	r := &Registry{}
	cases := []struct{ in, want string }{
		{"Z", "z"},                    // lone uppercase is lowercased
		{"aB", "a_b"},                 // lower→upper inserts underscore
		{"zB", "z_b"},                 // lower(z)→upper inserts underscore
		{"ABc", "a_bc"},               // acronym then word: A | Bc
		{"ZBc", "z_bc"},               // acronym (Z upper bound) then word
		{"0Bc", "0bc"},                // non-letter before upper: no underscore
		{"[Bc", "[bc"},                // char above 'Z' before upper: no underscore
		{"HTTPServer", "http_server"}, // acronym run then word
		{"AB", "ab"},                  // trailing acronym, no following lower
		{"ABac", "a_bac"},             // acronym then lower starting at 'a'
		{"AB{", "ab{"},                // following char above 'z': no underscore
		{"ABz", "a_bz"},               // following char at 'z' upper bound
		{"fooBar", "foo_bar"},         // plain camelCase boundary
	}
	for _, c := range cases {
		eqStr(t, "pathName", c.in, r.pathName(c.in), c.want)
	}
}

// TestEnumConstName_screamingSnake pins (*Registry).enumConstName, which
// converts a Go enum type name into the SCREAMING_SNAKE const identifier (with
// a trailing "S") used for the runtime values array.
func TestEnumConstName_screamingSnake(t *testing.T) {
	r := &Registry{}
	cases := []struct{ in, want string }{
		{"Ab", "ABS"},
		{"Zb", "ZBS"},
		{"aB", "A_BS"},
		{"zB", "Z_BS"},
		{"aBCd", "A_B_CDS"},
	}
	for _, c := range cases {
		eqStr(t, "enumConstName", c.in, r.enumConstName(c.in), c.want)
	}
}

// TestSanitizeTSIdent_charClass pins sanitizeTSIdent's identifier char-class
// filter: letters / '_' / '$' are always kept; digits are kept only when
// something is already buffered (a leading digit is dropped).
func TestSanitizeTSIdent_charClass(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a", "a"},
		{"z", "z"},
		{"Z", "Z"},
		{"a0", "a0"},
		{"a9", "a9"},
		{"0a", "a"}, // leading digit dropped
	}
	for _, c := range cases {
		eqStr(t, "sanitizeTSIdent", c.in, sanitizeTSIdent(c.in), c.want)
	}
}

// TestSanitizeVarName pins sanitizeVarName: it camelCases underscore-separated
// names, strips non-identifier characters (keeping a digit only after a kept
// char), and suffixes "Val" onto names that collide with a TS reserved word or
// a generated local so the generated decoder never shadows them.
func TestSanitizeVarName(t *testing.T) {
	cases := []struct{ in, want string }{
		// char-class boundaries
		{"A", "A"},
		{"Z", "Z"},
		{"a0", "a0"},
		{"a9", "a9"},
		{"a9b", "a9b"},
		{"123bad", "bad"}, // leading digits dropped
		{"", ""},
		// camelCase of underscore-separated names
		{"hello_world", "helloWorld"},
		{"a_b_c", "aBC"},
		// reserved word / generated-local collisions get a "Val" suffix
		{"o", "oVal"},
		{"out", "outVal"},
		{"v", "vVal"},
		{"private", "privateVal"},
		{"public", "publicVal"},
		{"protected", "protectedVal"},
		{"class", "classVal"},
		{"return", "returnVal"},
		{"delete", "deleteVal"},
		{"default", "defaultVal"},
		{"export", "exportVal"},
		{"import", "importVal"},
		{"new", "newVal"},
		{"this", "thisVal"},
	}
	for _, c := range cases {
		eqStr(t, "sanitizeVarName", c.in, sanitizeVarName(c.in), c.want)
	}
}

// byteCase is a table row for the isIdentChar test.
type byteCase struct {
	name string
	in   byte
	want bool
}

// TestIsIdentChar pins isIdentChar across every accepted range. Each boundary
// char is the edge of one accepted range while every other range term is
// false, so the OR result hinges on that single comparator; the chars just
// past each range top confirm they are rejected.
func TestIsIdentChar(t *testing.T) {
	cases := []byteCase{
		{name: "lower_low_a", in: 'a', want: true},
		{name: "lower_high_z", in: 'z', want: true},
		{name: "lower_mid_m", in: 'm', want: true},
		{name: "upper_low_A", in: 'A', want: true},
		{name: "upper_high_Z", in: 'Z', want: true},
		{name: "upper_mid_M", in: 'M', want: true},
		{name: "digit_low_0", in: '0', want: true},
		{name: "digit_high_9", in: '9', want: true},
		{name: "digit_mid_5", in: '5', want: true},
		{name: "underscore", in: '_', want: true},
		{name: "dollar", in: '$', want: true},
		{name: "above_z_brace", in: '{', want: false},
		{name: "above_Z_bracket", in: '[', want: false},
		{name: "above_9_colon", in: ':', want: false},
		{name: "below_a_backtick", in: '`', want: false},
		{name: "below_A_at", in: '@', want: false},
		{name: "below_0_slash", in: '/', want: false},
		{name: "space", in: ' ', want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isIdentChar(tc.in); got != tc.want {
				t.Errorf("isIdentChar(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// refCase is a table row for the isIdentReferenced test.
type refCase struct {
	name  string
	body  string
	ident string
	want  bool
}

// TestIsIdentReferenced pins isIdentReferenced's word-boundary matching: an
// identifier counts as referenced only when it is not flanked by other
// identifier characters. The boundary cases also guard against off-by-one
// index reads at the start and end of the body.
func TestIsIdentReferenced(t *testing.T) {
	cases := []refCase{
		{name: "whole_body_match", body: "foo", ident: "foo", want: true},
		{name: "left_attached", body: "xfoo", ident: "foo", want: false},
		{name: "right_attached", body: "foox", ident: "foo", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isIdentReferenced(tc.body, tc.ident); got != tc.want {
				t.Errorf("isIdentReferenced(%q, %q) = %v, want %v", tc.body, tc.ident, got, tc.want)
			}
		})
	}
}

// TestTypeRef_interfaceFallback pins TypeRef's nil-reflect.TypeOf guard: an
// interface type parameter has a nil zero value, so reflect.TypeOf returns nil
// and the code must fall back to reflect.TypeFor[T]() to resolve the name.
func TestTypeRef_interfaceFallback(t *testing.T) {
	// error is a builtin interface: PkgPath "" and Name "error".
	if got := TypeRef[error](); got != (WireType{PkgPath: "", Name: "error"}) {
		t.Errorf("TypeRef[error]() = %+v, want {PkgPath:\"\" Name:\"error\"}", got)
	}
	// A named interface in this package resolves its name via the fallback.
	if got := TypeRef[markerIface](); got.Name != "markerIface" {
		t.Errorf("TypeRef[markerIface]().Name = %q, want %q", got.Name, "markerIface")
	}
}

// TestIsValidTSIdent pins isValidTSIdent's predicate: the empty string and a
// leading digit are rejected via the early return, and the char-class accepts
// letters, '_', '$', and a non-leading digit. The empty / leading-digit
// rejections are unreachable through tsPropName / tsMemberRef (wire names
// never start that way), so they need a direct test.
func TestIsValidTSIdent(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"0abc", false},
		{"9", false},
		{"content-type", false},
		{"a b", false},
		{"abc", true},
		{"_x", true},
		{"$x", true},
		{"a1", true},
		{"A", true},
		{"_", true},
		{"$", true},
	}
	for _, c := range cases {
		if got := isValidTSIdent(c.in); got != c.want {
			t.Errorf("isValidTSIdent(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestEmitUnionDecoder_sanitizesDiscriminator pins that a non-identifier or
// reserved-word //wiregen:union discriminator is sanitized to a valid TS
// identifier for the decoder's parameter name. The case labels use the
// already-escaped DiscriminatorMap keys, so only the local param binding is
// affected; a value that sanitizes to empty falls back to "disc". A valid
// identifier discriminator (see TestUnion_DecoderAllVariants) is unchanged.
func TestEmitUnionDecoder_sanitizesDiscriminator(t *testing.T) {
	r := &Registry{DiscriminatorMap: map[string]map[string]string{"E": {"a": "A"}}}
	for _, c := range []struct{ disc, want string }{
		{"event_type", "eventType"}, // underscore -> camelCase
		{"default", "defaultVal"},   // reserved word -> suffixed
		{"@@@", "disc"},             // sanitizes to empty -> fallback
	} {
		var w strings.Builder
		r.emitUnionDecoder(&w, &typeInfo{Name: "E", Union: &UnionDef{Discriminator: c.disc}})
		if out := w.String(); !strings.Contains(out, "("+c.want+": string, v: unknown)") {
			t.Errorf("discriminator %q: expected sanitized param %q in signature, got:\n%s", c.disc, c.want, out)
		}
	}
}
