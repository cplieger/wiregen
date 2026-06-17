package wiregen

// Unit wiregen-u3: tests that kill surviving gremlins mutants in wiregen.go.
// Internal test package so the unexported helpers under test (isIdentChar,
// isIdentReferenced, sanitizeVarName) are reachable. All identifiers defined
// here are prefixed gk_wiregen_u3_ to avoid colliding with sibling units that
// may share this package directory in the same wave.

import "testing"

// gk_wiregen_u3_byteCase is a table row for the isIdentChar boundary tests.
type gk_wiregen_u3_byteCase struct {
	name string
	in   byte
	want bool
}

// Test_gk_wiregen_u3_isIdentChar_rangeBoundaries pins every comparator in
// isIdentChar (wiregen.go:504):
//
//	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
//	       (c >= '0' && c <= '9') || c == '_' || c == '$'
//
// Each killing input is the boundary char of one accepted range while every
// OTHER range term is false, so flipping a single comparator flips the whole
// OR result:
//   - 'a'/'A'/'0' kill the >= lower-bound boundary mutants (504:12/38/64).
//   - 'z'/'Z'/'9' kill the <= upper-bound boundary mutants (504:24/50/76).
//   - 'm'/'M'/'5' (interior) kill the <= negation mutants (504:24/50/76 neg)
//     by going false when <= flips to >.
//   - the just-above-range chars '{'/'['/':' additionally kill those same
//     negation mutants from the false side (a flipped > wrongly accepts them).
func Test_gk_wiregen_u3_isIdentChar_rangeBoundaries(t *testing.T) {
	cases := []gk_wiregen_u3_byteCase{
		// lowercase a-z
		{name: "lower_low_a", in: 'a', want: true},  // 504:12 >= boundary
		{name: "lower_high_z", in: 'z', want: true}, // 504:24 <= boundary
		{name: "lower_mid_m", in: 'm', want: true},  // 504:24 negation (true side)
		// uppercase A-Z
		{name: "upper_low_A", in: 'A', want: true},  // 504:38 >= boundary
		{name: "upper_high_Z", in: 'Z', want: true}, // 504:50 <= boundary
		{name: "upper_mid_M", in: 'M', want: true},  // 504:50 negation (true side)
		// digits 0-9
		{name: "digit_low_0", in: '0', want: true},  // 504:64 >= boundary
		{name: "digit_high_9", in: '9', want: true}, // 504:76 <= boundary
		{name: "digit_mid_5", in: '5', want: true},  // 504:76 negation (true side)
		// other accepted identifier chars (characterization)
		{name: "underscore", in: '_', want: true},
		{name: "dollar", in: '$', want: true},
		// chars one past each range top: rejected by the original; a flipped
		// upper-bound <= (now >) would wrongly accept them.
		{name: "above_z_brace", in: '{', want: false},  // 504:24 negation (false side)
		{name: "above_Z_bracket", in: '[', want: false}, // 504:50 negation (false side)
		{name: "above_9_colon", in: ':', want: false},  // 504:76 negation (false side)
		// chars one below each range bottom and a plain separator: rejected.
		{name: "below_a_backtick", in: '`', want: false},
		{name: "below_A_at", in: '@', want: false},
		{name: "below_0_slash", in: '/', want: false},
		{name: "space", in: ' ', want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isIdentChar(tc.in)
			if got != tc.want {
				t.Errorf("isIdentChar(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// gk_wiregen_u3_refCase is a table row for the isIdentReferenced tests.
type gk_wiregen_u3_refCase struct {
	name  string
	body  string
	ident string
	want  bool
}

// Test_gk_wiregen_u3_isIdentReferenced_boundaries pins the index-guard
// comparators in isIdentReferenced (wiregen.go:479/483/491). These guards
// protect array indexing, so a flipped boundary either returns the wrong
// value or reads out of bounds (panic) — both fail the test, while the
// original returns cleanly without panicking.
//
//   - whole_body_match: the ident spans the whole body, so j==0 and
//     end==len(body). The original returns true. It kills:
//     479:8 boundary (j<0 -> j<=0 returns false at j==0),
//     483:8 boundary (j>0 -> j>=0 reads body[-1] -> panic at j==0),
//     483:8 negation (j>0 -> j<=0 reads body[-1] -> panic at j==0),
//     491:10 boundary (end<len -> end<=len reads body[len] -> panic).
//   - left_attached: an ident char precedes the match (j>0). The original
//     rejects it (false). It kills 483:8 negation (j>0 -> j<=0 skips the
//     left-boundary check and wrongly returns true).
//   - right_attached: an ident char follows the match (end<len). The original
//     rejects it (false). It kills 491:10 negation (end<len -> end>=len skips
//     the right-boundary check and wrongly returns true).
//
// 477:16 (`i < len(body)` -> `i <= len(body)`) is an EQUIVALENT mutant: i can
// only reach exactly len(body) via the advance, and the extra iteration the
// mutant adds does strings.Index("", ident) == -1 (ident is non-empty) and
// returns false — identical to the original's fall-through return false. No
// input can distinguish them, so it is intentionally not targeted here.
func Test_gk_wiregen_u3_isIdentReferenced_boundaries(t *testing.T) {
	cases := []gk_wiregen_u3_refCase{
		{name: "whole_body_match", body: "foo", ident: "foo", want: true},
		{name: "left_attached", body: "xfoo", ident: "foo", want: false},
		{name: "right_attached", body: "foox", ident: "foo", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isIdentReferenced(tc.body, tc.ident)
			if got != tc.want {
				t.Errorf("isIdentReferenced(%q, %q) = %v, want %v", tc.body, tc.ident, got, tc.want)
			}
		})
	}
}

// gk_wiregen_u3_varCase is a table row for the sanitizeVarName test.
type gk_wiregen_u3_varCase struct {
	name string
	in   string
	want string
}

// Test_gk_wiregen_u3_sanitizeVarName_digitBoundary pins the trailing-digit
// upper boundary in sanitizeVarName's char-strip pass (wiregen.go:432, the
// `r <= '9'` comparator):
//
//	} else if clean.Len() > 0 && r >= '0' && r <= '9' {
//
// A '9' right after a leading letter is the upper boundary of the digit range.
// The original keeps it ("a9"); flipping <= to < (CONDITIONALS_BOUNDARY) would
// drop the '9' and return "a" instead. "a9b" gives the same kill with an
// interior '9'.
func Test_gk_wiregen_u3_sanitizeVarName_digitBoundary(t *testing.T) {
	cases := []gk_wiregen_u3_varCase{
		{name: "trailing_nine_kept", in: "a9", want: "a9"},   // kills 432:46 boundary
		{name: "interior_nine_kept", in: "a9b", want: "a9b"}, // kills 432:46 boundary
		{name: "low_digit_zero_kept", in: "a0", want: "a0"},  // characterization
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeVarName(tc.in)
			if got != tc.want {
				t.Errorf("sanitizeVarName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
