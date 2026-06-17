package wiregen

import "testing"

// gk_wiregen_u2_eq asserts a string helper produced the exact expected output.
// The unit-tag prefix keeps it from colliding with a sibling unit that shares
// this package directory.
func gk_wiregen_u2_eq(t *testing.T, fn, in, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s(%q) = %q, want %q", fn, in, got, want)
	}
}

// Test_gk_wiregen_u2_pathName pins (*Registry).pathName's snake-case
// conversion, focusing on the acronym-boundary else-if at wiregen.go:366
// ("uppercase preceded by uppercase, followed by lowercase -> insert _").
// Each input is chosen so mutating one operator on line 366 changes the
// observable output or indexes runes[i+1] out of range.
func Test_gk_wiregen_u2_pathName(t *testing.T) {
	r := &Registry{}
	cases := []struct{ in, want string }{
		// 'S' (prev 'P' upper, next 'e' lower) gets an underscore. Kills
		// 366:49 negation (i+1<len -> i+1>=len drops the _), 366:72
		// (runes[i+1] -> runes[i-1]='P', not lower, drops the _), 366:76
		// negation, and 366:97 negation.
		{"HTTPServer", "http_server"},
		// Trailing uppercase preceded by uppercase: the original short-circuits
		// at i+1<len (2<2 is false). 366:46 (i+1 -> i-1) and 366:49 boundary
		// (< -> <=) both then evaluate runes[i+1]=runes[2] -> index out of range.
		{"AB", "ab"},
		// Next char is exactly 'a': kills 366:76 boundary (>='a' -> >'a' drops _).
		{"ABac", "a_bac"},
		// Next char '{' is just above 'z': original drops the _, while 366:93
		// (runes[i+1] -> runes[i-1]='A', which is <='z') would wrongly add one.
		{"AB{", "ab{"},
		// Next char is exactly 'z': kills 366:97 boundary (<='z' -> <'z' drops _).
		{"ABz", "a_bz"},
		// camelCase boundary through the prev-lowercase branch.
		{"fooBar", "foo_bar"},
	}
	for _, c := range cases {
		gk_wiregen_u2_eq(t, "pathName", c.in, r.pathName(c.in), c.want)
	}
}

// Test_gk_wiregen_u2_enumConstName pins (*Registry).enumConstName, covering the
// uppercase test at wiregen.go:383, the prev-index read at 385, and the
// prev-lowercase test at 386.
func Test_gk_wiregen_u2_enumConstName(t *testing.T) {
	r := &Registry{}
	cases := []struct{ in, want string }{
		// 'A' is the lower bound of the uppercase test: 383:9 (>='A' -> >'A')
		// reclassifies 'A' as non-uppercase and lowercases it via ru-32 ('!').
		{"Ab", "ABS"},
		// 'Z' is the upper bound: 383:22 (<='Z' -> <'Z') reclassifies 'Z' (':').
		{"Zb", "ZBS"},
		// prev 'a' is lowercase -> underscore. 386:13 (boundary and negation)
		// and 386:28 negation drop the underscore ("ABS"); and the prev read at
		// 385:20 (runes[i-1] -> runes[i+1]=runes[2]) indexes out of range.
		{"aB", "A_BS"},
		// prev 'z' is exactly the upper bound: 386:28 boundary (<='z' -> <'z')
		// drops the underscore ("ZBS").
		{"zB", "Z_BS"},
		// In-bounds, non-panic kill of 385:20: reading runes[i+1] instead of
		// runes[i-1] flips both underscore decisions ("AB_CDS" vs "A_B_CDS").
		{"aBCd", "A_B_CDS"},
	}
	for _, c := range cases {
		gk_wiregen_u2_eq(t, "enumConstName", c.in, r.enumConstName(c.in), c.want)
	}
}

// Test_gk_wiregen_u2_sanitizeTSIdent pins the char-class filter in
// sanitizeTSIdent at wiregen.go:406 (keep letters/_/$) and 408 (keep digits
// only when something is already buffered). A dropped boundary char shrinks
// the output, which the assertion catches.
func Test_gk_wiregen_u2_sanitizeTSIdent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a", "a"},   // 406:9 boundary (>='a' -> >'a') and 406:21 negation drop 'a'.
		{"z", "z"},   // 406:21 boundary (<='z' -> <'z') drops 'z'.
		{"Z", "Z"},   // 406:47 boundary (<='Z' -> <'Z') drops 'Z'.
		{"a0", "a0"}, // 408:30 boundary (>='0' -> >'0') drops the kept digit.
		{"a9", "a9"}, // 408:42 boundary (<='9' -> <'9') drops '9'.
		{"0a", "a"},  // leading digit is dropped (the b.Len()==0 branch).
	}
	for _, c := range cases {
		gk_wiregen_u2_eq(t, "sanitizeTSIdent", c.in, sanitizeTSIdent(c.in), c.want)
	}
}

// Test_gk_wiregen_u2_sanitizeVarName pins the char-class filter in the clean
// loop of sanitizeVarName at wiregen.go:430 and 432. Each input survives the
// camelCase step unchanged, so the clean-loop operator is exactly what the
// output depends on (a dropped boundary char yields "" or a shorter string).
func Test_gk_wiregen_u2_sanitizeVarName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"A", "A"},   // 430:35 boundary (>='A' -> >'A') drops 'A' -> "".
		{"Z", "Z"},   // 430:47 boundary (<='Z' -> <'Z') drops 'Z' -> "".
		{"a0", "a0"}, // 432:34 boundary (>='0' -> >'0') drops '0' -> "a".
	}
	for _, c := range cases {
		gk_wiregen_u2_eq(t, "sanitizeVarName", c.in, sanitizeVarName(c.in), c.want)
	}
}
